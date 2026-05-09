# homert02 upgrade to 20260509.13

Date: 2026-05-09

Target:

- host: homert02
- management address: 192.168.123.129
- release: 20260509.13

## Procedure

1. Downloaded `routerd-20260509.13-linux-amd64.tar.gz` on homert02.
   `gh` is not installed on the host, so the release asset was fetched with
   `curl` from GitHub releases.
2. Extracted the tarball and ran `sudo ./install.sh --no-install-deps`.
   Dependencies are already declared by the `Package` resource and present on
   the host, so package installation was skipped for the production upgrade.
3. Confirmed binary versions:
   - `routerd 20260509.13`
   - `routerctl 20260509.13`
4. Ran `routerd validate --config /usr/local/etc/routerd/router.yaml`.
5. Ran `routerd apply --config /usr/local/etc/routerd/router.yaml --once`.
6. Restarted `routerd.service` after the one-shot apply, because the service
   unit is managed by the same YAML and must run with the post-apply unit.

## Configuration correction

The first post-upgrade run exposed a pre-existing homert02 YAML mismatch:

- `PPPoEInterface/pppoe-flets` is intentionally disabled.
- `HealthCheck/internet-via-pppoe` is disabled.
- `IPv4Route/pppoe-healthcheck` still tried to install a route via
  `ppp-flets`.
- `PathMTUPolicy/lan-to-dslite-mtu` still tried to set MTU on `ppp-flets`.

The local production YAML was adjusted before the final apply:

- `IPv4Route/pppoe-healthcheck` now depends on
  `PPPoEInterface/pppoe-flets` reaching `Connected`.
- The legacy `IPv4DefaultRoutePolicy` PPPoE candidate also depends on the same
  connected phase.
- `PathMTUPolicy/lan-to-dslite-mtu` no longer lists `pppoe-flets` while PPPoE
  is disabled.

After applying the corrected YAML, no `pppoe-healthcheck`, `ppp-flets`, or
`path-mtu` warnings appeared in `routerd.service` logs.

## Final state

`routerctl status`:

```text
phase: Healthy
generation: 46
resourceCount: 88
lastApplyTime: 2026-05-09T11:05:21.90350353Z
```

Services:

```text
routerd.service: active
routerd-firewall-logger.service: active
routerd-pppoe-pppoe-flets.service: inactive / disabled
```

Key resources:

```text
DHCPv6PrefixDelegation/wan-pd: Healthy
DHCPv6Information/wan-info: Healthy
PPPoEInterface/pppoe-flets: Disabled
HealthCheck/internet-via-dslite-a: Healthy
HealthCheck/internet-via-dslite-b: Healthy
HealthCheck/internet-via-dslite-c: Healthy
HealthCheck/internet-via-dslite-ra: Healthy
HealthCheck/internet-via-pppoe: Disabled
HealthCheck/internet-via-ix2215: Healthy
HealthCheck/internet-via-hgw-direct: Healthy
DNSResolver/lan-resolver: Healthy
PathMTUPolicy/lan-to-dslite-mtu: Healthy, toInterfaces=ds-lite-a,ds-lite-b,ds-lite-c,ds-lite-ra
FirewallZone/wan: Healthy, ifnames=ens18,ds-lite-a,ds-lite-b,ds-lite-c,ds-lite-ra,ppp-flets
FirewallZone/lan: Healthy, ifnames=ens19,tailscale0
FirewallZone/management: Healthy, ifnames=ens20
FirewallPolicy/home: Healthy
```

Routes and traffic:

```text
IPv4 default: dev ds-lite-a
IPv6 default: via RA on ens18
curl -4 https://www.google.com/generate_204: 204
curl -6 https://www.google.com/generate_204: 204
DNS @127.0.0.1 example.com A/AAAA: success
```

Firewall:

- `inet routerd_filter` is loaded.
- Input and forward chains use `policy drop`.
- `wan`, `lan`, and `management` zones are present.
- Deny logging is active through NFLOG group 1.

## Notes

- The install script preserved `/usr/local/etc/routerd/router.yaml` and wrote a
  sample config separately, as intended.
- The install script initially wrote the generic packaged `routerd.service`.
  Running `routerd apply --once` restored the homert02 YAML-defined unit.
