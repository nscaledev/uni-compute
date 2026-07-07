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
- Instance status is projection-only here: IP addresses, MAC address, power
  state (lifecycle Phase, including Queued/Building for baremetal), and health
  are copied from the server response.
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
clearly: some instance updates replace the backing server, and some rebuild it.

Flavor drift is replacement-worthy: the controller deletes the
existing backing server and recreates it, which loses server-local state and
changes IP/disk continuity.

SSH certificate authority drift is likewise replacement-worthy. Region's server
update body (`ServerV2Spec`) carries no CA field — the CA is a create-only input
— so an in-place update can never change it. The controller therefore treats a
CA change (set, unset, or swapped) as a recreate trigger, comparing the instance's
desired CA against the live CA region reports in the server's status. Without this,
a CA change would be silently dropped.

Image-only drift is not destructive at the compute layer: the controller updates
the existing region server, and the region controller performs an in-place Nova
rebuild. The instance record and its backing server survive, though the rebuild
reimages the server's root disk.

A rebuild that fails is not retried by region: region parks the server and
surfaces the failure (as an error requiring user action) rather than looping. The
compute controller does not treat this specially — recovery is expressed
per-instance, by changing the image again (which drives a fresh rebuild) or by
recreating the instance.

Instance names are immutable end-to-end: the API handler rejects renames with
HTTP 422 and region enforces the same at its layer. The controller therefore
never encounters a name change at reconcile time; name-change detection has been
removed from the recreate heuristic.

userData drift is not acted on against a running server: user data is first-boot
initialization data, so a change is stored on the spec and consumed by future
servers without rebuilding or recreating the running one.

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
