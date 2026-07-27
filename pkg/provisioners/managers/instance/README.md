# `pkg/provisioners/managers/instance`

## Purpose

This package is the controller-side realization of the compute instance
abstraction.

It reads stored `ComputeInstance` desired state and makes sure there is a
matching backing `region.Server` with the expected shape. It also mirrors
provider-facing status back from the server onto the instance and tears down the
associated quota allocation after the backing server is gone.

If `pkg/server/handler/instance` defines the public contract, this package is
where that contract is translated into the hidden execution primitive.

## Invariants And Guard Rails

- A backing server is located by scoped `region` query plus the reserved
  instance tag, not by an explicit foreign key stored on the instance.
- Create and update requests sent to region always carry the reserved instance
  tag so later lookup and reverse mapping can find the server again.
- Instance status is projection-only here: IP addresses, MAC address, lifecycle,
  and health are mirrored from the backing server. Lifecycle is the generic
  `Active` condition (reusing region's `ActiveConditionReason` vocabulary —
  `Queued`/`Building` for baremetal, plus the `Rebuilding` reimage state),
  replacing region's retired `Status.Phase`. The backing server's error detail
  is reconstructed onto the instance's `provisioningStatusDetail`, and lifecycle
  transitions are logged to the structured stream at parity with provisioning.
- Deprovision is two-stage:
  delete the backing server first, then release the identity allocation record
  only after the server is gone.
- The controller yields while region still reports provisioning or while delete
  is still in flight. Final instance completion is intentionally downstream of
  region's lifecycle.
- The region/network/flavor/image/server IDs needed to talk to region are read
  back from the instance's labels and spec as strings and parsed to typed IDs
  (see [`../../../ids`](../../../ids/README.md)) at the region-client call. This
  is fail-closed: a missing or malformed stored ID surfaces as a reconcile error
  rather than a request built from an unchecked string. The backing server ID is
  likewise parsed from the region read model only on the paths that actually call
  region (rebuild/update), not on the no-op path where desired and current specs
  already match.

## Destructive Update Semantics

This package hides a major nuance the higher-level architecture needs to state
clearly: some instance updates are actually rebuilds.

When flavor or image changes are detected, the controller treats that as
replacement-worthy and deletes the existing backing server before continuing.
That is not a cosmetic implementation detail. It means an apparently simple
instance update can imply loss of server-local state and changes to IP/disk
continuity.

Instance names are immutable end-to-end: the API handler rejects renames with
HTTP 422 and region enforces the same at its layer. The controller therefore
never encounters a name change at reconcile time; name-change detection has been
removed from the rebuild heuristic.

## Caveats

- The lookup contract is weaker than the handler-side one. `getServer()` returns
  the first match and does not enforce uniqueness, so multiple matching servers
  would create ambiguous controller behaviour.
- The current rebuild heuristic is intentionally incomplete. The code comments
  already admit that flavor-change detection is constrained by lower-level
  region/provider visibility and that live-migration preservation is not modeled.
- The region client is not cached and the code already notes token/cache churn
  risk during busy periods.
- This package depends on region to be the truth source for provisioning
  progress and health. Compute does not independently observe provider state;
  when region distinguishes queued baremetal servers from active provisioning,
  this package only persists and re-exposes that region-derived metadata.

## TODO

- Enforce uniqueness when resolving the backing server, not just existence.
- Make rebuild semantics explicit in status or events so users are not left
  inferring destructive replacement from delayed side effects.
- Cache or otherwise amortize controller-side region client construction if
  identity token churn becomes operationally significant.
- Revisit whether backing-server identity should stay tag-based or become a more
  explicit coordination contract.

## Cross-Package Context

- [../../../server/handler/instance](../../../server/handler/instance/README.md)
  documents the API-side invariants this package realizes asynchronously
- [../../../managers/instance](../../../managers/instance/README.md) documents
  the controller wiring and reverse watch path from `region.Server` back to
  `ComputeInstance`
