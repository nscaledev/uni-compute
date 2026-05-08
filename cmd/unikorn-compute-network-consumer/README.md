# `cmd/unikorn-compute-network-consumer`

## Purpose

This command is a cross-service lifecycle-edge bridge.

It watches `region.Network` deletion events and propagates that deletion intent
into `ComputeInstance` roots that carry the matching network label.

That matters because the visible compute lifecycle root is not the same object
as the lower-level `region.Server`, and compute instances are not owner
referenced directly under the region network object. This command closes that
gap.

## Invariants And Guard Rails

- `region.Network` remains the source of truth for network lifecycle.
- This command only finds instances that are correctly labeled with the network
  label.
- It deletes compute roots, not backing region servers directly. The compute
  controller and region ownership model then handle downstream teardown.

## Caveats

- Correctness depends on label completeness. If an instance is missing the
  network label, network deletion will not reach it through this bridge.
- This command is part of the real architecture even though it sits outside
  `pkg/`. Without it, compute and region would each be locally reasonable but
  globally disconnected during network teardown.

## TODO

- Audit whether any future compute root resources beyond `ComputeInstance`
  should also participate in network-deletion propagation.
- Add explicit observability for deletion fan-out failures if this bridge
  becomes operationally important enough to warrant alerting.
