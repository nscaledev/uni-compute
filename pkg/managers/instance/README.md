# `pkg/managers/instance`

## Purpose

This package wires the `ComputeInstance` controller into the shared manager
framework.

The important behaviour is not the factory boilerplate. It is the reverse watch
edge from `region.Server` back to `ComputeInstance`, which closes the loop
between the hidden server primitive and the visible compute instance root.

## Invariants And Guard Rails

- Generation changes on `ComputeInstance` trigger normal reconciliation of
  desired-state changes.
- Resource-version changes on `region.Server` also trigger reconcile, using the
  reserved instance tag to map server updates back to the owning instance.
- This reverse watch path is what lets compute refresh instance status and react
  to backing-server deletion or state changes without exposing `Server` as a
  user-facing root.
- The controller registers both compute and region schemes because it spans both
  resource families directly.

## Caveats

- The reverse mapping currently lists all instances and then searches by name.
  The code comment still describes this as transitional until project-namespace
  assumptions disappear, so this is not the clean final scoping model.
- Reverse lookup again depends on the reserved tag contract. If the tag is
  absent or reused, status propagation breaks silently by returning no requests.
- This package is intentionally thin. Most lifecycle policy lives in
  [../../provisioners/managers/instance](../../provisioners/managers/instance/README.md).

## TODO

- Replace the full-list reverse lookup with a direct indexed/shared-namespace
  lookup once the remaining namespace/scoping debt is removed.
- Decide whether missing or duplicate reverse mappings should become explicit
  warnings or metrics rather than being ignored.

## Cross-Package Context

- [../../provisioners/managers/instance](../../provisioners/managers/instance/README.md)
  contains the actual reconcile logic this factory wires up
- [../../../cmd/unikorn-compute-network-consumer](../../../cmd/unikorn-compute-network-consumer/README.md)
  is the other important lifecycle edge outside `pkg/` that participates in
  keeping instance and network teardown aligned
