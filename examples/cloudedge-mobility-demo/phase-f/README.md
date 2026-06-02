# CloudEdge Mobility Phase F overlays

These snippets are declarative diffs for the live demo operator. They are not
standalone router configs.

Phase F starts from the steady-state demo config where `onprem-router` owns
`10.77.60.10/32` through `staticOwnedAddresses`. The steady-state config must
be applied to every router first.

## F2 release

Apply `release-static-owned.yaml` to every rendered site config by removing
`10.77.60.10/32` from the `onprem-router` member. The on-prem router emits a
`routerd.client.ipv4.expired` event, withdraws proxy ARP/GARP advertisement,
and cloud sites remove delivery for `.10`.

## F3 handover

Apply `handover-to-aws-a.yaml` after the release config. Keep
`staticOwnedAddresses` empty for `.10` and add the `staticHandovers` entry.
Cloud nodes must not project `aws-router-a` as owner until they observe the
on-prem expired event.

## Rollback

Apply `rollback-onprem-owned.yaml` to return to the steady-state owner intent:
`onprem-router` again owns `10.77.60.10/32`, and `staticHandovers` is absent.

