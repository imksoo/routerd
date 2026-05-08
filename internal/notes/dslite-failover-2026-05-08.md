# DS-Lite fail-over validation (2026-05-08)

Scope: router04 (FreeBSD) and homert02 (Linux). The default DS-Lite inner source uses the RFC 6333 B4-AFTR link prefix 192.0.0.0/29: .2, .3, and .4 for the three parallel tunnels.

## router04 FreeBSD

Baseline after apply:
- routerd: Healthy, generation 1067, resourceCount 57.
- DS-Lite tunnels: gif41, gif42, gif43 Up.
- Default route: gif41.
- PF NAT anchor: routerd_nat, selected NAT egress gif41.
- nwadmin03 ens19 client baseline curl: 1 MiB from speed.cloudflare.com in 3.95s, HTTP 200.

Fault injection:
- Forced gif41 down in a 1s loop.
- Default route and PF NAT moved to gif42 at 22:29:04Z, about 6s after injection started.
- Existing long TCP transfer did not survive the tunnel switch. New connections recovered.
- New nwadmin03 ens19 curl during the outage: 1 MiB in 5.06s, HTTP 200.
- tcpdump on gif42 captured traffic from 192.0.0.3 to 162.159.140.220, including SYN MSS 1414.

Recovery:
- Restored gif41 and stopped the fault loop.
- Default route and PF NAT returned to gif41 within the 30s health-check interval.
- Post-recovery curl: 1 MiB in 0.75s, HTTP 200.

Root-cause fix found during validation:
- FreeBSD PF NAT for NAT44Rule.egressPolicyRef was not rendered/applied.
- FreeBSD default route used the shared AFTR inner gateway 192.0.0.1, which is ambiguous across gif41/gif42/gif43.
- Fixed by loading a PF NAT anchor from the resolved EgressRoutePolicy device and by rendering DS-Lite default routes as interface routes (`route -n change default -interface gifXX`).

## homert02 Linux

Baseline after apply:
- routerd: Healthy, generation 31, resourceCount 76.
- DS-Lite tunnels: ds-lite-a, ds-lite-b, ds-lite-c Up.
- Default route: ds-lite-a.
- Baseline curl from the router: 1 MiB in 0.10s, HTTP 200, local source 192.0.0.2.

Fault injection:
- Forced ds-lite-a down in a 1s loop.
- Default route moved to ds-lite-b at 22:32:27Z, about 20s after injection started.
- Curl during the outage: 1 MiB in 0.11s, HTTP 200, local source 192.0.0.3.
- tcpdump on ds-lite-b captured traffic from 192.0.0.3 to 162.159.140.220, including SYN MSS 1405.

Recovery:
- Restored ds-lite-a and stopped the fault loop.
- Default route returned to ds-lite-a within the 30s health-check interval.
- Post-recovery curl: 1 MiB in 0.11s, HTTP 200, local source 192.0.0.2.

Notes:
- Cloudflare's documented 100 MiB URL returned HTTP 403 in this environment. Validation used 1 MiB for success checks and 10 MiB for long-transfer interruption behavior.
- Health checks use interval 30s, healthyThreshold 1, unhealthyThreshold 3. Route selection reacted before the Unhealthy phase finalized when link state/status updates were observed; the route returned after the next healthy observation.
