# `pkg/apis/unikorn/v1alpha1`

## Purpose

This package defines the persisted Kubernetes resource model for compute's
user-facing abstraction: `ComputeInstance`.

The important architectural point is that `ComputeInstance` is not the cloud
primitive that ultimately performs the work. It is the control-plane resource
that compute exposes to users while the backing lifecycle is later realized via
`region` `Server` resources by the compute controller.

That means this package defines the storage contract for the visible lifecycle
root, not for the hidden execution primitive.

## What The Resource Carries

- desired machine shape through `FlavorID` and `ImageID`
- network intent through a single selected network plus optional public IP,
  security groups, and allowed source addresses
- optional SSH certificate authority linkage
- optional cloud-init user data
- projected runtime status such as IP addresses, MAC address, power state, and
  health conditions

The object also carries org/project/region/network labels and delegated
principal metadata through shared helpers outside this package.

## Invariants And Guard Rails

- `ComputeInstance` is the API/storage root compute wants users to manipulate.
  Clients should not need to manage backing `region.Server` resources directly.
- The spec is intentionally narrower than `region.Server`: it only exposes the
  fields compute wants to stabilize as the higher-level instance contract.
- `Pause` is a real reconciliation guard rail. A paused instance should stop
  compute-side reconciliation without changing the stored desired state.
- Status is mostly projected truth from the backing `region.Server`, not an
  independently reconciled compute-owned runtime source of truth.
- `PublicIPEnabled()` is part of the accounting contract: quota allocation logic
  derives floating-IP usage from this persisted intent.

## Caveats

- `ResourceLabels()` currently returns `nil`, which suggests the resource is not
  yet participating in a stronger single-namespace identity scheme the shared
  interfaces were designed to support.
- The API object does not store an explicit foreign key to the backing
  `region.Server`. That linkage is recovered later through labels, tags, and
  scoped lookup in other packages.
- Several fields are current operational reality rather than polished final
  shape. For example, status still exposes `PrivateIP` and `PublicIP` as
  strings with inline TODO notes rather than stronger address types.

## TODO

- Implement or intentionally remove `ResourceLabels()` so the package either
  participates in the shared single-namespace lookup contract or clearly opts
  out of it.
- Replace stringly typed IP status fields with stronger address types once the
  surrounding API shape is ready.
- Revisit whether the backing-server linkage should remain entirely implicit or
  whether a more explicit coordination field would reduce consistency risk.

## Cross-Package Context

- [../../../server/handler/instance](../../../server/handler/instance/README.md)
  documents how handler code creates and mutates `ComputeInstance` objects
- [../../../provisioners/managers/instance](../../../provisioners/managers/instance/README.md)
  documents how the controller turns this stored desired state into backing
  `region.Server` lifecycle
