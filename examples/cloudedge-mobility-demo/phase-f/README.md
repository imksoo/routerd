# CloudEdge Mobility Phase F overlays

These snippets are declarative diffs for the live demo operator. They are not
standalone router configs.

Phase F starts from the steady-state demo config where `onprem-router` owns
`10.77.60.10/32` through `staticOwnedAddresses`. The steady-state config must
be applied to every router first.

## F2 release

Apply `release-static-owned.yaml` to every rendered site config by removing
`10.77.60.10/32` from the `onprem-router` member. Verify withdrawal in BGP and
the applied local dataplane before continuing; saving a desired plan alone is
not evidence that proxy ARP, routes or forwarding state have been removed.

## F3 handover

Apply `handover-to-aws-a.yaml` after the release config. Keep
`staticOwnedAddresses` empty for `.10` and add the `staticHandovers` entry.
Give this transfer a new `id` shared by all participating routers. Cloud nodes
must not project `aws-router-a` as owner until they observe the matching on-prem
release for this pool, ID, address, source and target. An ordinary DHCP/ARP expiry
from F2 is not that confirmation. The source first withdraws its BGP ownership
and persists the replacement plan. Its SAM, route and address owners then
acknowledge withdrawal of the effects they own, for this exact transfer and plan.
Only after all three acknowledgements does the source publish the release.
Pending kernel operations survive a controller restart; failed cleanup or a
failed receipt write keeps the target waiting. An independently declared static
address for the same host must also be withdrawn.

The release is a five-minute lease refreshed after reconfirming withdrawal.
Without a fresh matching release, the target withholds ownership. Keep the source
available during the handover. Once actual cloud ownership is established, use
its provider discovery result for steady-state ownership. A live release permits
the old home router to install delivery effects toward the new owner; refreshing
that lease does not tear down legitimate relay traffic. The barrier covers
routerd-owned effects, not arbitrary administrator-created network state. Inspect
the applied dataplane and qualify the transition with real traffic.

## Rollback

Apply `rollback-onprem-owned.yaml` to return to the steady-state owner intent:
`onprem-router` again owns `10.77.60.10/32`, and `staticHandovers` is absent.
Withdraw the target intent first and verify it before restoring the old source.
For a subsequent handover, use a new `id`; reusing an old ID reuses that operation's
idempotency identity and is not a new transfer attempt.

