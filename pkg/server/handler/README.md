# `pkg/server/handler`

## Purpose

This package is the API application layer for compute.

At the top level it is intentionally thinner than the equivalent handler layer
in `region`. Most of the interesting behaviour lives in the resource-specific
clients beneath it:

- `region` read-side capability exposure for regions, flavors, and images
- `instance` create/read/update/delete plus operational verbs over the hidden
  backing server
- `version` release metadata for the deployed compute API binary

So this package is best understood as the transport and request-shaping layer
that ties those two pieces together under one HTTP API.

## Invariants And Guard Rails

- `v2` `Instance` is the main intended API surface for compute lifecycle.
- The older `v1`-shaped endpoints here are read-side capability discovery over
  region resources, not compute-owned server lifecycle.
- `GET /api/v2/version` is an authenticated service metadata endpoint. It
  returns the build name and version from the running binary, sets
  `Cache-Control: no-cache`, and does not expose or mutate compute resources.
- Top-level handlers do final request parsing, response writing, and HTTP error
  normalization, then delegate actual policy and mutation logic downward.
- Read-side region/flavor/image endpoints impersonate the caller into region so
  region remains the authority on visibility.
- Flavor discovery excludes region flavors marked `pinnedOnly`, because compute
  instances do not expose `infrastructureRef` host pinning and those flavors
  would be rejected by region during backing server creation.
- The `.well-known/openid-protected-resource` endpoint is cacheable and is part
  of the service trust contract, not just a convenience route.

## Architectural Split

This package makes compute's split surface visible:

- `GetApiV1Organizations...Regions/Flavors/Images` is effectively curated
  catalog access into region
- `Get /api/v2/version` is authenticated deployment metadata for clients and
  CI gates that need to identify the exact compute server build
- `Get/Post/Put/Delete /api/v2/instances...` is compute's own higher-level
  abstraction over hidden server lifecycle

That split is one of the real differences from `region`, where server lifecycle
itself is directly exposed as a first-class root.

## Caveats

- The interesting invariants are mostly not in the top-level handler methods.
  They live in [./instance](./instance/README.md) and in the controller layer.
- The package still carries mixed API eras: `v1` compatibility-shaped read
  endpoints alongside `v2` instance lifecycle.
- Error and path handling are deliberately centralized here, so small mistakes
  in transport wiring can affect every route.

## TODO

- Document whether the remaining `v1` read-side catalog routes should remain as
  long-term service surface or be replaced by a flatter `v2` capability model.
- Revisit whether the top-level split between curated region read APIs and
  compute-owned instance lifecycle needs to be made clearer in the public API
  documentation itself.

## Cross-Package Context

- [./instance](./instance/README.md) contains the actual instance behaviour and
  most of the service-specific policy
- [../README.md](../README.md) documents how this handler layer is composed into
  the live HTTP server and trust pipeline
