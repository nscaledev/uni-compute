# Packages

## Purpose

This tree contains the compute service implementation.

The useful way to read it is not as an isolated CRUD service, but as a
specialized façade over a lower-level region compute primitive:

- handlers expose a user-facing `Instance` abstraction
- handlers charge and validate that abstraction against identity and region
- controllers realize it later as a hidden `region.Server`
- reverse watches and command-level consumers keep the visible instance root in
  sync with lower-level server and network lifecycle

Compared with `identity` and `region`, the most important difference is that
compute's visible lifecycle root is not the resource that actually performs the
work.

## Recommended Reading Order

### Typed Resource Identifiers

- [ids](./ids/README.md)

This package defines the UUID-backed `InstanceID` compute owns, and documents
how compute consumes identity's and region's ID types as a black box. It is the
foundation of the "validate at the boundary, carry typed, stringify at the sink"
rule the handler and controller follow.

### Stored Resource Model

- [apis/unikorn/v1alpha1](./apis/unikorn/v1alpha1/README.md)

This package defines the persisted `ComputeInstance` object that compute
exposes as its user-facing root.

### API Behaviour

- [server/handler/instance](./server/handler/instance/README.md)

This package explains the public contract:

- authenticated service version discovery through `GET /api/version`
- instance creation and mutation
- validation against region-owned resources
- quota coupling to identity
- operational verbs delegated to the hidden backing server

### Controller Realization

- [provisioners/managers/instance](./provisioners/managers/instance/README.md)
- [managers/instance](./managers/instance/README.md)

These packages explain how stored instance intent becomes backing
`region.Server` lifecycle and how server events are mapped back into instance
reconciliation.

## Important Cross-Cutting Themes

### Hidden Primitive, Visible Abstraction

Compute intentionally exposes `Instance` while preserving `Server` as a hidden
primitive in region.

That buys architectural freedom:

- end users interact with the higher-level abstraction
- compute can stabilize a narrower contract than raw server lifecycle
- the platform keeps the option to use `region.Server` for other internal or
  future behind-the-scenes purposes

But it also creates documentation obligations:

- the visible root is `ComputeInstance`
- the execution primitive is `region.Server`
- the link between them is currently recovered through scoped tag-based lookup
  rather than a stored foreign key

### Region-Dependent Compute

Unlike `region`, this repo does not own a direct provider abstraction layer.

Compute depends on region for:

- network scoping and authorization
- flavor catalog and region-available image visibility
- security-group and SSH CA validation
- the actual server lifecycle and operational verbs
- projected runtime status

So compute should be read as an application/service layer over region compute,
not as a separate cloud-control implementation.

### Typed Identifiers At The Boundary

Resource identifiers are UUID-backed typed IDs (see [ids](./ids/README.md)), not
bare strings, from the router to the edge of the handler and controller layers.

- Compute owns `InstanceID`; it consumes identity's organization/project IDs and
  region's region/network/flavor/image/server/SSH-CA IDs as a black box.
- Validation happens at the trust boundary: path parameters at the router,
  create-body IDs at unmarshal. Stored IDs (CRD labels and spec, region read
  models) are re-parsed fail-closed when they re-enter the typed world.
- The IDs convert to strings only at genuine sinks — Kubernetes object names and
  labels, CRD spec fields, and provider/region API calls — all of which remain
  string-typed.

The net behavioural change is that a malformed identifier now fails early and
uniformly (a `400` at the router for path IDs, an explicit parse error for stored
or body IDs) rather than surfacing — if at all — deep in a handler or at the
region client.

### Accounting Lives At The Abstraction Boundary

Quota charging happens at the compute instance layer, not at the hidden server
layer.

The allocation model is deliberately phrased in user-facing terms such as:

- `servers`
- `gpus`
- `floatingips`

That makes compute the accounting façade over region-backed server capacity.

### Lifecycle DAG, Not Just Package Imports

The key runtime graph is:

1. API create/update writes `ComputeInstance` plus allocation state
2. controller translates the instance into backing `region.Server` lifecycle
3. reverse watch on `region.Server` feeds status and deletion changes back to
   instance reconcile
4. network deletion is bridged into instance deletion by
   [../cmd/unikorn-compute-network-consumer](../cmd/unikorn-compute-network-consumer/README.md)

This is the right abstraction for the service. Describing compute as a bag of
CRUD handlers would hide the important coordination edges.

## Caveats

- The instance-to-server link is currently implicit and tag-based.
- Some instance updates cross the backing-server boundary: flavor drift
  deletes and recreates the backing server, image-only drift updates the existing
  server so region performs an in-place Nova rebuild, and userData drift is an
  in-place spec update not acted on against the running server.
- The monitor binary exists, but `pkg/monitor` does not yet register active
  checkers, so compute does not currently have a meaningful region-style polling
  architecture.
- Parts of the controller wiring still carry scoping-transition debt around
  project namespaces versus the intended flatter shared-namespace model.

## TODO

- Document additional helper packages such as `client`, `constants`, `openapi`,
  and `server` once their usage context has been reviewed in more detail.
- Decide whether the hidden server linkage should remain purely tag-based or be
  made more explicit.
- Make destructive rebuild semantics clearer in user-visible status,
  documentation, or API shape.
- Tighten the remaining namespace/scoping transition points in controller-side
  reverse lookup.
