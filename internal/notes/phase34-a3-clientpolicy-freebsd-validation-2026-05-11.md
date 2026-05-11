# Phase 3.4 A3: ClientPolicy FreeBSD validation

Date: 2026-05-11
Host: router04 (FreeBSD, 192.168.123.126)

## Implementation

FreeBSD pf cannot match Ethernet source MAC addresses in the same way as Linux nftables. The FreeBSD renderer therefore supports ClientPolicy with an IP-based approximation:

- ClientPolicy classification entries must reference `ipv4Reservation`.
- The referenced DHCPv4Reservation supplies the IPv4 address used in pf rules.
- Guest service holes are rendered for DNS, DHCPv4, and NTP.
- Guest private IPv4 egress deny rules are rendered for 10/8, 172.16/12, and 192.168/16.
- IPv6 guest deny is intentionally not rendered from IPv4 reservations to avoid invalid pf family combinations.

Linux nftables keeps MAC-based matching.

## router04 validation

Local config: `local/router04.yaml`

Added test resources:

- `ClientPolicy/lab-guest-devices`
- `DHCPv4Reservation/lab-client` fixed to `192.168.160.184`
- guest services: dns, dhcp, ntp

Commands and checks:

- `go test ./pkg/render`: pass
- `make check-schema`: pass
- `ROUTERD_OS=freebsd GOOS=freebsd GOARCH=amd64 make build-daemons`: pass
- `routerd validate --config /usr/local/etc/routerd/router.yaml`: pass
- `routerd apply --config /usr/local/etc/routerd/router.yaml --once`: Healthy
- `pfctl -nf /run/routerd/firewall.pf`: pass
- `pfctl -s info`: Enabled
- `routerctl status --json`: `Healthy gen=1129 resources=74`

Evidence excerpt:

```text
Status: Enabled for 0 days 00:01:47
Healthy gen=1129 resources=74
pass in quick on vtnet1 inet proto udp from 192.168.160.184 ... label "routerd:client-policy:lab-guest-devices:dns"
block drop in log quick on vtnet1 inet from 192.168.160.184 to 10.0.0.0/8 label "routerd:client-policy:lab-guest-devices:deny"
block drop in log quick on vtnet1 inet from 192.168.160.184 to 172.16.0.0/12 label "routerd:client-policy:lab-guest-devices:deny"
block drop in log quick on vtnet1 inet from 192.168.160.184 to 192.168.0.0/16 label "routerd:client-policy:lab-guest-devices:deny"
```

Rollback cron was installed during pf apply and removed after SSH, pf, and routerd status were confirmed.
