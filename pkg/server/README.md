# `pkg/server`

## Purpose

This package assembles the live compute HTTP server.

It is where the service-specific application layer is combined with the shared
platform request pipeline:

- OpenTelemetry
- logging
- route resolution
- CORS
- remote authorization
- OpenAPI validation
- audit

The important point is that compute does not invent its own trust model here.
It composes platform-defined middleware and then injects a compute-specific
handler that depends on both identity and region clients.

## Invariants And Guard Rails

- Middleware ordering is treated as an application-level invariant.
- Identity-backed authorization and OpenAPI validation are applied centrally,
  not route-by-route.
- The server always constructs both identity and region API clients before the
  compute handler comes alive, which reflects compute's role as a façade over
  both services.
- pprof is exposed on a separate listener, which is operationally relevant even
  though it is not part of the user-facing API contract.
- Audit always writes one structured log line per authenticated write. When
  `--audit-host` is set, records also go to the collector over mutual TLS,
  signed, with an optional bearer token. The chart sets these flags from
  `audit` or `global.audit` values (unikorn-common's `unikorn.audit.flags`).
  A configured collector whose credentials cannot be read stops the server at
  startup, rather than letting it run without delivering records. The secrets
  are read through the cached client, which the server's existing `list` and
  `watch` on secrets already covers. See identity's
  [audit README](https://github.com/nscaledev/uni-identity/blob/main/pkg/middleware/audit/README.md).

## Caveats

- This package is mostly composition, so it is easy to undersell. In practice it
  defines the live request pipeline that makes the handler model real.
- The pprof server is started opportunistically in a goroutine and logs errors
  via `fmt.Println`, which is operationally weaker than the structured logging
  used elsewhere.
- `context.TODO()` is still used when building downstream identity and region
  API clients, which suggests some context-propagation cleanup is still pending.
- Audit runs inside the validator, as the middleware list is applied in
  reverse. A request the validator rejects, such as a bad body or a failed ACL
  lookup, never reaches audit and leaves no record.
- With audit values set, the chart and the image must roll back together: an
  older image does not know the `--audit-*` flags and will not start with them.

## TODO

- Move OpenTelemetry-specific configuration into a dedicated options struct as
  the in-code TODO already suggests.
- Revisit pprof listener lifecycle and logging so it follows the same
  structured-operational conventions as the main server.
- Audit whether downstream client construction should inherit a more explicit
  startup context than `context.TODO()`.
- Run audit outside the validator, so requests it rejects are recorded too.

## Cross-Package Context

- [./handler](./handler/README.md) documents the compute application layer this
  package exposes over HTTP
- [`identity/pkg/middleware/openapi`](https://github.com/nscaledev/uni-identity/blob/main/pkg/middleware/openapi/README.md)
  documents the authorization/validation middleware family this package composes
