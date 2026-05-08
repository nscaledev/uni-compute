# `pkg/monitor`

## Purpose

This package defines the shell of a polling-based monitor process for compute.

Today its main architectural meaning is negative: compute ships a monitor
binary, but this package does not yet register any active checkers. So it is a
framework stub rather than a real status-projection subsystem like the one in
`region`.

## Invariants And Guard Rails

- The monitor loop runs under a synthetic `compute-monitor` principal.
- The process is structured around pluggable `Checker` implementations and a
  configurable poll period.
- The loop is cancellation-aware and logs checker failures without crashing the
  whole process.

## Caveats

- `checkers := []Checker{}` means the current implementation does no work.
- The options carry identity, region, and client configuration, which suggests
  intended future cross-service polling behaviour, but that architecture is not
  live yet.
- Higher-level documentation should not imply compute currently has a meaningful
  monitor-driven truth-projection path comparable to region.

## TODO

- Either add real checkers and document their ownership/truth model, or delete
  the dormant monitor subsystem if it is not going to be used.
- If polling is introduced later, document clearly whether it is authoritative
  observation, drift detection, metrics projection, or compensating control.

## Cross-Package Context

- [../../cmd/unikorn-compute-monitor](../../cmd/unikorn-compute-monitor/README.md)
  is the binary entrypoint for this package
