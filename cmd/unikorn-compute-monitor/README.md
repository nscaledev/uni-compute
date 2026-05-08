# `cmd/unikorn-compute-monitor`

## Purpose

This command is the binary entrypoint for compute's monitor process.

Right now it mostly exists to host the polling framework in
[../../pkg/monitor](../../pkg/monitor/README.md). The important architectural
fact is that the framework currently has no registered checkers, so this binary
starts successfully but does not yet perform meaningful monitoring work.

## Caveats

- This command should not be described as active status projection until real
  checkers exist.
- Its existence is still useful signal: the service expected to grow a monitor
  loop similar in broad shape to region's observational processes, but that work
  is not complete here.
