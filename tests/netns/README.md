# Network namespace tests

These tests are reserved for host-network integration checks. They are not run
by `go test ./...` because they require Linux network namespaces, FRR,
keepalived, nftables, conntrack, and explicit `sudo`.

Do not add tests here that mutate the default host namespace. Each test must:

- create its own network namespaces and veth links
- run with explicit `sudo`
- clean up namespaces, temporary FRR/keepalived state, and nftables tables on
  exit
- avoid touching production interfaces, routes, or firewall tables

Required scenarios before deploying the BGP/VRRP/IngressService stack to
homert02:

1. FRR config syntax or reload failure leaves the previous working config in
   place.
2. Two keepalived instances hand a VIP to the standby node within the configured
   advert/preempt timing.
3. BGPStateWatcher preserves peer and prefix event ordering under repeated
   BGP peer flaps.
4. IngressService failover keeps existing conntrack flows while new flows use
   the selected backend.
5. Prefixes outside `BGPRouter.spec.importPolicy.allowedPrefixes` are rejected
   by FRR import policy.

