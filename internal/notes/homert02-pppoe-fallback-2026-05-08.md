# homert02 PPPoE fallback validation, 2026-05-08

Target: homert02, Ubuntu production router.

Scope:

- Add `PPPoEInterface/pppoe-flets` on `ens18`.
- Add `DSLiteTunnel/ds-lite-ra` as an RA/SLAAC outer-address fallback.
- Keep existing `ds-lite-a/b/c` as the highest priority group.
- Keep existing `ix2215-fallback`.
- Add `hgw-direct` as the final native IPv4 fallback.
- Do not run DS-Lite fault injection on production traffic.

Configured candidate order:

| Candidate | Device | Weight | Health check | Result |
| --- | --- | ---: | --- | --- |
| `ds-lite-a` | `ds-lite-a` | 120 | `internet-via-dslite-a` | Healthy |
| `ds-lite-b` | `ds-lite-b` | 120 | `internet-via-dslite-b` | Healthy |
| `ds-lite-c` | `ds-lite-c` | 120 | `internet-via-dslite-c` | Healthy |
| `ds-lite-ra` | `ds-lite-ra` | 80 | `internet-via-dslite-ra` | Healthy |
| `pppoe-flets` | `ppp-flets` | 70 | `internet-via-pppoe` | Healthy |
| `ix2215-fallback` | `ens19` via `172.17.0.1` | 50 | `internet-via-ix2215` | Healthy |
| `hgw-direct` | `ens18` via `192.168.1.1` | 40 | `internet-via-hgw-direct` | Healthy |

Validation:

- `routerd validate --config /usr/local/etc/routerd/router.yaml`: OK
- `routerd apply --config /usr/local/etc/routerd/router.yaml --once`: OK
- `routerd.service`: active
- `routerctl status`: Healthy, generation 34, resourceCount 88
- `ppp-flets`: UP, `100.64.4.253 peer 202.222.12.149/32`
- Host IPv4 smoke test: `curl https://www.google.com/generate_204`: HTTP 204

Notes:

- PPPoE is provided by the generated `routerd-pppoe-pppoe-flets.service`.
- `routerd-healthcheck@internet-via-pppoe.service` is active and checks `208.67.222.222:443` over `ppp-flets`.
- `hgw-direct` was initially tested with gateway `192.168.1.254`, but the health check timed out.
- The working native IPv4 gateway for this host is `192.168.1.1`, so `hgw-direct` uses that gateway.
- Production selected path remained `ds-lite-a`; no DS-Lite fault injection was performed on homert02.
