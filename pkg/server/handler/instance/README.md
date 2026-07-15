# `pkg/server/handler/instance`

## Purpose

This package is the real API behaviour behind compute instances.

It turns the public `Instance` API into:

- scoped Kubernetes `ComputeInstance` objects
- quota/allocation updates in identity
- validation against region-owned networks, flavors, images, security groups,
  and SSH certificate authorities
- operational pass-through to the hidden backing `region.Server`

The most important architectural fact here is that compute does not expose
`region.Server` directly. It exposes `Instance`, then recovers the backing
server later when operational verbs such as power control, console access, SSH
key retrieval, or snapshotting are requested.

## Invariants And Guard Rails

- `Instance` create is a saga-backed multi-object workflow:
  quota allocation is charged first, then the `ComputeInstance` CRD is created.
- The selected network is the main scoping root for creation. Compute injects
  org/project into the principal and asks region to resolve the network under
  impersonated access rather than trusting a caller-supplied ownership claim.
- Flavor and image must both be visible in the chosen region and must be
  mutually compatible for architecture, disk size, and virtualization mode.
- Referenced resources must stay inside the instance's scope, rejected at the API
  edge with HTTP 422 on both create and update. Fetching a reference from region
  only proves the caller MAY see it — a caller authorized across several tenancies
  could otherwise attach a resource from another tenancy — so an explicit scope
  check is applied on top:
  - a referenced security group must belong to the same **network** as the
    instance (`status.networkId`). A network belongs to exactly one identity (one
    underlying OpenStack project), which belongs to one organization and project,
    so same-network is the natural granularity that closes the cross-tenancy hole
    and matches what OpenStack permits. Region enforces the identical rule against
    the same field, so it is uniform across both services.
  - a referenced SSH CA (which is not network-scoped) must share the instance's
    organization and project.
- User data is validated on create against the region server provisioner's
  cloud-init parser, so malformed payloads are rejected with HTTP 422 at the
  boundary instead of failing region-side during managed cloud-init augmentation
  or silently inside the guest at boot. With an SSH CA attached the payload must
  additionally support managed augmentation (which excludes gzip); without one,
  gzip payloads are accepted and passed through unmodified. Updates only
  re-check the SSH CA coupling (against the CA in the update request; the
  region API goes further and re-validates nothing on update, treating the CA
  as immutable). User-data is not otherwise re-validated on update because it
  is only consumed at initial bootstrap, and re-validating would block updates
  of pre-existing instances whose user-data predates create-time validation.
- Instance names are unique per network: the Kubernetes resource name is derived
  deterministically from `(networkID, instanceName)` via UUID v5, so a duplicate
  create collides at the Kubernetes layer and is rejected with HTTP 409 without a
  read-before-write; this applies only to instances created after this mechanism
  was introduced — pre-existing randomly-named instances are not covered.
- Instance names are immutable after creation; update requests that supply a
  different name are rejected with HTTP 422. This mirrors the region-level
  constraint that VM hostnames cannot change after boot.
- Operational verbs act on exactly one backing server. If scoped lookup finds
  zero or multiple matches, the request fails as a consistency error.
- Snapshot requests strip compute-reserved system tags from the caller payload
  and then add the reserved instance provenance tag themselves.

## Typed Identifier Boundary

This package is a trust boundary for resource identifiers (see
[`../../../ids`](../../../ids/README.md)).

- Inbound IDs arrive already typed: the instance path parameter is UUID-validated
  at the router, and create-body IDs (organization, project, network, flavor,
  image, SSH CA) are UUID-validated at unmarshal. Handlers never defend against a
  malformed inbound ID.
- RBAC and tenancy are consumed from identity's scope-reader surface: the
  `AllowOrganizationScopeID` / `AllowProjectScopeID` / `AllowProjectScopeCreateID`
  variants take typed IDs directly.
- The IDs are carried typed through the per-resource client methods and converted
  to strings only at the sinks — Kubernetes object names and labels, the
  `ComputeInstance` CRD spec, and region API calls.
- Read models are produced by parsing the stored CRD strings back to typed IDs.
  Because the instance spec schema is shared between the read and write surfaces,
  `convert` fails closed if a stored flavor/image/SSH-CA ID is malformed; the
  read-only status IDs (`regionId` / `networkId`, mirrored from labels) and the
  security-group ID list stay string-typed, matching region's treatment of
  status and list IDs.

## Hidden-Primitive Model

The package deliberately hides the backing server primitive.

It stores enough label context on the `ComputeInstance` to constrain later
server lookup by:

- organization
- project
- region
- network
- reserved `compute.unikorn-cloud.org/instance-id` tag

That keeps the public API centered on `Instance`, but it also means the server
link is reconstructed rather than stored explicitly.

## Phase Projection

`convertPowerState` projects the region-owned `Server.Status.PowerState`
(a `regionv1.InstanceLifecyclePhase` on the persisted `ComputeInstance`) into
the public API as `status.powerState`. The round-trip is lossless for known
phases — `Pending`, `Queued`, `Building`, `Running`, `Stopping`, `Stopped` — and
intentionally drops unknown values to `nil` rather than collapsing them to
`Pending`. This keeps the API honest: if region adds a new phase before compute
learns about it, callers see no `powerState` rather than a misleading `Pending`,
which surfaces the gap immediately. The provisioner-side `convertPowerState`
follows the same rule so the controller and handler agree on what "unknown"
means.

## Caveats

- The instance-to-server link is implicit and tag-based. That is flexible, but
  it also means correctness depends on the reserved tag remaining exclusive and
  on scoped server queries returning exactly one match.
- The update path recovers the owning scope, region and network IDs from the
  existing object's labels and parses them to typed IDs (fail-closed). If a label
  is absent or corrupt on an existing object (e.g. manually edited via kubectl),
  the parse fails and the update returns a 500. The admission policy guards
  against this for new objects, but cannot repair already-corrupt state.
- Update is not always an in-place mutation in effect. Flavor or image changes
  can lead to destructive server replacement later in the controller layer.
- The package preserves allocation annotations manually during update, which is
  already marked in code as smell and signals a leaky boundary between handler
  mutation and allocation bookkeeping.
- This package depends heavily on region as both validation oracle and
  operational execution layer. Compute is not cloud-provider-facing on its own.

## TODO

- Replace the implicit server lookup contract with a more explicit coordination
  mechanism if hidden-server use cases start making tag collisions plausible.
- Remove the allocation-annotation preservation hack by tightening the shared
  mutation/allocation interface.
- Revisit whether the API should surface destructive-rebuild semantics more
  explicitly for flavor/image changes.

## Cross-Package Context

- [../../../apis/unikorn/v1alpha1](../../../apis/unikorn/v1alpha1/README.md)
  defines the persisted instance resource this package reads and writes
- [../../../provisioners/managers/instance](../../../provisioners/managers/instance/README.md)
  documents how backing servers are actually created and updated after this
  package writes desired state
- [`region/pkg/handler/server`](https://github.com/nscaledev/uni-region/blob/main/pkg/handler/server/README.md)
  documents the lower-level server API surface compute builds on but does not
  expose directly
