# FreeBSD VNET dataplane coverage matrix

Issue #891 extends the existing production-rendered FreeBSD PF VNET smoke.
The runner is deliberately bounded, root-only, and refuses a pre-existing PF
ruleset or state table before it creates disposable jails/epairs.

| Linux netns scenario | FreeBSD disposition | Native oracle |
| --- | --- | --- |
| Ingress DNAT conntrack survival | Deferred: the Linux scenario depends on nftables DNAT backend replacement and conntrack identity. PF NAT state is covered here, but changing an active backend is not claimed equivalent. | Explicit deferred boundary. |
| IPv4 DF force-fragment forwarding | Linux-only: production uses nftables `routerd_forcefrag`; FreeBSD has no equivalent portable PF rendering in the supported slice. | Explicit Linux-only boundary. |
| Keepalived VIP failover / no-spurious restart | Deferred to the native WAN/VPN/HA lifecycle matrix (#899); no FreeBSD CARP equivalence is invented here. | Explicit deferred boundary. |
| SAM proxy-ARP/GARP transition | Covered by the native observer/SAM work, not duplicated in the PF runner. | Existing observer VNET evidence. |
| Egress source affinity and PF NAT44 | Applicable and covered by this runner. | Production `routerd render freebsd`, PF syntax/load, two VNET sources, routehost packet captures, PF states, source translation, and owned cleanup. |

The added NAT44 assertion verifies that packets which PF routes to each
routehost are translated to that routehost interface address. It rejects an
egress capture containing the original `192.0.2.0/24` source. It does not
claim DNAT backend replacement or health failover semantics.
