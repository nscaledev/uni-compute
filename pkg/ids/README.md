# `pkg/ids`

## Purpose

Nominal UUID types for the resource identifiers the compute API owns.

Compute mints exactly one user-facing resource identifier of its own, so this
package is deliberately small:

| Type | Identifies |
|---|---|
| `InstanceID` | compute instances |

`InstanceID` is a distinct named type over `uuid.UUID` — not an alias — so the
compiler prevents it from being interchanged with any other ID type, and it
implements `encoding.TextUnmarshaler` by delegating to `uuid.UUID`. That makes
the oapi-codegen parameter binder validate UUID format at path-parameter binding
time, so a non-UUID instance ID is rejected with a `400` at the routing layer
before any handler runs.

## Scope

These types live **at the API layer only**. The `ComputeInstance` CRD, its
labels, and the region/provider calls behind it all remain string-typed; the
handler and controller convert to plain strings at those sinks.

Identifiers owned by other services are **not** redefined here — compute uses
their types directly:

- `organizationId` / `projectId` come from the identity service's
  [`pkg/ids`](https://github.com/nscaledev/uni-identity/tree/main/pkg/ids)
  (`identityids.OrganizationID` / `identityids.ProjectID`).
- `regionId`, `networkId`, `flavorId`, `imageId`, `serverId` and
  `sshCertificateAuthorityId` come from the region service's
  [`pkg/ids`](https://github.com/nscaledev/uni-region/tree/main/pkg/ids).

This keeps ownership honest: a service only mints typed IDs for the resources it
is the system of record for, and consumes everyone else's types as a black box.

## Conversion

The same inward/outward rules the identity and region services use apply here.

**Inward (string → typed ID):** use `Parse*` for any value that arrives as an
untrusted or stored string — Kubernetes labels, CRD spec fields read back from
storage, and IDs echoed from a region read model. Parsing fails closed: a
malformed value surfaces as an error rather than silently proceeding. Use
`MustParse*` only where the value is guaranteed valid by a prior step — a path
parameter the oapi-codegen binder has already validated, or a known-good test
fixture literal.

**Outward (typed ID → string):** call `.String()` to produce the canonical
hyphenated UUID for a Kubernetes object name, a label value, a CRD spec field,
or a provider/region API call.

## Trust Boundary

The typed IDs exist to move ID validation to the system's edges:

- **Path parameters** are validated at the router via `x-go-type`.
- **Create-body IDs** (organization, project, network, flavor, image, SSH CA)
  are UUID-validated at unmarshal, so a handler never has to defend against a
  malformed body ID.
- **Stored IDs** (CRD labels and spec, region read models) are re-parsed
  fail-closed when they re-enter the typed world on read or reconcile paths.

Beyond those edges the IDs are carried typed, so the compiler — not a deferred
runtime check — guarantees the right kind of ID reaches the right call.

## Cross-Package Context

- [`../README.md`](../README.md) is the ordered entry point to the service
  internals and links the packages that consume these types.
- [`../server/handler/instance`](../server/handler/instance/README.md) validates
  and carries typed IDs across the API boundary.
- [`../provisioners/managers/instance`](../provisioners/managers/instance/README.md)
  re-parses stored IDs fail-closed on the controller path.
