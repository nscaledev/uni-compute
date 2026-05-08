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
- SSH CA references must stay inside the same organization and project as the
  instance.
- User data is validated more strictly when an SSH CA is attached because
  managed user-data injection depends on recognized cloud-init formats.
- Instance names are best-effort unique per network so the higher-level
  abstraction does not create cloud-hostname aliasing underneath.
- Operational verbs act on exactly one backing server. If scoped lookup finds
  zero or multiple matches, the request fails as a consistency error.
- Snapshot requests strip compute-reserved system tags from the caller payload
  and then add the reserved instance provenance tag themselves.

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

## Caveats

- The instance-to-server link is implicit and tag-based. That is flexible, but
  it also means correctness depends on the reserved tag remaining exclusive and
  on scoped server queries returning exactly one match.
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
