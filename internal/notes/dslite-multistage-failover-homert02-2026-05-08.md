# homert02 multi-stage WAN fallback validation, 2026-05-08

Target: homert02, Ubuntu production router.

Purpose:

- Run the same staged WAN fallback test as router04.
- Prove that Linux and FreeBSD use the same resource model for the chain.
- Keep production traffic restored to the primary DS-Lite path after the test.

Config under test:

- `ds-lite-a`: `ds-lite-a`, weight 120, source `192.0.0.2/29`
- `ds-lite-b`: `ds-lite-b`, weight 120, source `192.0.0.3/29`
- `ds-lite-c`: `ds-lite-c`, weight 120, source `192.0.0.4/29`
- `ds-lite-ra`: `ds-lite-ra`, weight 80, source `192.0.0.5/29`, outer address from RA/SLAAC
- `pppoe-flets`: `ppp-flets`, weight 70, SoftEther `open@open.ad.jp`
- `ix2215-fallback`: `ens19` via `172.17.0.1`, weight 50
- `hgw-direct`: `ens18` via `192.168.1.1`, weight 40

One issue was found before the final run:

- `IPv4DefaultRoutePolicy/lan-forward-egress` had an aggregate health check on the `dslite-pd-balanced` route set.
- When `ds-lite-a` was forced down, the route set was skipped even though `ds-lite-b` and `ds-lite-c` were still Healthy.
- The route set already filters each target by its own health check.
- The aggregate `healthCheck: internet-via-dslite-a` was removed from the route-set candidate.

Final staged fault test:

| Stage | Forced down | Egress selected | Forward selected | Device | curl result | tcpdump observation |
| --- | --- | --- | --- | --- | --- | --- |
| baseline | none | `ds-lite-a` | `dslite-pd-balanced` | `ds-lite-a` | `200`, 1 MiB | `192.0.0.2 -> 162.159.140.220` |
| a down | `ds-lite-a` | `ds-lite-b` | `dslite-pd-balanced` | `ds-lite-b` | `200`, 1 MiB | `192.0.0.3 -> 162.159.140.220` |
| a+b down | `ds-lite-a,ds-lite-b` | `ds-lite-c` | `dslite-pd-balanced` | `ds-lite-c` | `200`, 1 MiB | `192.0.0.4 -> 162.159.140.220` |
| a+b+c down | `ds-lite-a,ds-lite-b,ds-lite-c` | `ds-lite-ra` | `ds-lite-ra` | `ds-lite-ra` | `200`, 1 MiB | `192.0.0.5 -> 162.159.140.220` |
| a+b+c+ra down | plus `ds-lite-ra` | `pppoe-flets` | `pppoe-flets` | `ppp-flets` | `200`, 1 MiB | `100.64.4.253 -> 162.159.140.220` |
| plus PPPoE down | plus `routerd-pppoe-pppoe-flets.service stop` | `ix2215-fallback` | `ix2215-fallback` | `ens19` | `200`, 1 MiB | `172.17.0.2 -> 162.159.140.220` |
| plus IX2215 health down | temporary blackhole for `1.0.0.1/32` | `hgw-direct` | `hgw-direct` | `ens18` | `200`, 1 MiB | `192.168.1.249 -> 162.159.140.220` |

Restore state:

- Temporary blackhole route for `1.0.0.1/32` was removed.
- `routerd.service` is active.
- `EgressRoutePolicy/ipv4-default` selected `ds-lite-a`.
- `IPv4DefaultRoutePolicy/lan-forward-egress` selected `dslite-pd-balanced`.
- DS-Lite health checks `a`, `b`, `c`, and `ra` are Healthy.
- IX2215 and HGW direct health checks are Healthy.

PPPoE caveat:

- PPPoE was established during the fallback test and carried traffic successfully.
- After the intentional PPPoE stop, `pppd` retried on `ens18` but received no PADO packets.
- `pppoe-flets` therefore remained Unhealthy after cleanup.
- The candidate stayed out of selection, so production traffic stayed on `ds-lite-a`.
- A follow-up stop/start retry was run three times on 2026-05-09.
- Each retry left `routerd-pppoe-pppoe-flets.service` active, but `pppd` still logged `Timeout waiting for PADO packets`.
- `HealthCheck/internet-via-pppoe` stayed Unhealthy.
- This points to a transient or rate-limited SoftEther public test endpoint after forced disconnect.
- The fallback behavior is still correct: PPPoE stays out of selection while Unhealthy.
- PPPoE later redialed successfully at 2026-05-09T00:25:15Z and created session `17032`.
- Because that consumed one access-line PPPoE session slot, the test unit was stopped and disabled.
- `routerd-healthcheck@internet-via-pppoe.service` was also stopped and disabled so stale success would not keep the candidate looking ready.
- `routerd apply --once` re-enabled the declared PPPoE SystemdUnit, so the final operational release stopped and disabled it again without another apply.
- Final service state: `routerd.service` active, `routerd-pppoe-pppoe-flets.service` disabled and stopped, and `routerd-healthcheck@internet-via-pppoe.service` disabled and stopped.
- Final link state: no `ppp-flets` interface and no `pppd` process.
- Final routing state: default route still uses `ds-lite-a`.
- The long-term config decision is still open: keep PPPoE declared as a manually enabled test candidate, or remove it from production YAML.
- After the session was released, a final redial test was run on 2026-05-09.
- `routerd-pppoe-pppoe-flets.service` created session `19521`.
- `ppp-flets` came up with `100.64.5.10 peer 202.222.12.149/32`.
- `curl --interface ppp-flets https://www.google.com/generate_204` returned HTTP 204.
- `HealthCheck/internet-via-pppoe` returned Healthy while the link was up.
- After this confirmation, the PPPoE and PPPoE health check units were stopped and disabled again to keep the access-line session slot free.
- routerd now supports keeping this fallback declared but disabled in YAML.
- homert02 uses `PPPoEInterface.spec.disabled: true`, `HealthCheck.spec.disabled: true`, and `EgressRoutePolicy.candidates[].disabled: true` for `pppoe-flets`.
- A normal `routerd apply --once` leaves `routerd-pppoe-pppoe-flets.service` present but inactive and disabled.
- The PPPoE health check status is `Disabled`, and the PPPoE egress candidate is `ready: false`.
- Manual retest remains possible by explicitly enabling the rendered unit and health check.

Parity result:

- router04 FreeBSD validated six stages: `ds-lite-a/b/c`, `ds-lite-ra`, PPPoE, and HGW direct.
- homert02 Linux validated seven stages: `ds-lite-a/b/c`, `ds-lite-ra`, PPPoE, IX2215, and HGW direct.
- Both OSes use the same YAML resource model. OS differences are isolated to renderer commands.
