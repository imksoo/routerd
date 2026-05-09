# ClientPolicy lab validation

Date: 2026-05-09

Target:

- router: router05
- OS: Ubuntu
- management address: 192.168.123.127
- tested release binary: 20260509.13

Scope:

- Validate `ClientPolicy` on Linux nftables.
- Do not enable `ClientPolicy` on homert02 production.
- Do not test router04 because FreeBSD pf intentionally rejects
  `ClientPolicy`.

## Configuration

`local/router05.yaml` was extended with an include-mode client policy:

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    interfaces:
      - Interface/ens19
    guestServices:
      - dns
      - dhcp
      - ntp
    classification:
      - macAddress: bc:24:11:df:9e:c2
        as: guest
        name: router05-lan-observed-client
      - macAddress: 02:00:00:00:05:99
        as: guest
        name: router05-dummy-guest
```

The first MAC address was observed on router05 `ens19` with `ip neigh`.
The second MAC address is a dummy entry used to verify multi-entry set
generation.

## Apply result

Commands:

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once
routerctl status --socket /run/routerd/routerd.sock
```

Result:

```text
phase: Healthy
generation: 4
resourceCount: 35
ClientPolicy/guest-devices: Healthy
```

router05 remained reachable over the management network. IPv4 and IPv6
generate_204 checks succeeded after apply.

## nftables result

The live `inet routerd_filter` table contains the expected MAC set:

```text
set client_policy_guest_devices {
  type ether_addr
  elements = { 02:00:00:00:05:99, bc:24:11:df:9e:c2 }
}
```

Guest-to-router service exceptions were generated before the normal
zone-to-self jump:

```text
iifname "ens19" ether saddr @client_policy_guest_devices udp dport 53 accept
iifname "ens19" ether saddr @client_policy_guest_devices tcp dport 53 accept
iifname "ens19" ether saddr @client_policy_guest_devices udp dport { 67, 547 } accept
iifname "ens19" ether saddr @client_policy_guest_devices udp dport 123 accept
iifname "ens19" ether saddr @client_policy_guest_devices drop
```

Guest forwarding denies were generated before the normal zone-to-zone jump:

```text
iifname "ens19" ether saddr @client_policy_guest_devices ip daddr 10.0.0.0/8 drop
iifname "ens19" ether saddr @client_policy_guest_devices ip daddr 172.16.0.0/12 drop
iifname "ens19" ether saddr @client_policy_guest_devices ip daddr 192.168.0.0/16 drop
iifname "ens19" ether saddr @client_policy_guest_devices ip6 daddr fc00::/7 drop
```

`nft -c -f /usr/local/etc/routerd/nftables.nft` passed.

The counters stayed at zero during the validation window because no active
guest packet from the classified client was observed while the check ran.

## Include and exclude mode coverage

Live router05 validation covered include mode.

Exclude mode was not applied to the live lab LAN to avoid unexpectedly
classifying every unlisted client as guest. It remains covered by the focused
renderer test:

```sh
go test ./pkg/render -run 'TestNftablesClientPolicy(IncludeGuestMACs|ExcludeTrustedMACs)' -v
```

That test confirms the exclude-mode form:

```text
iifname "ens19" ether saddr != @client_policy_byod_default_guest ...
```

## Production decision

homert02 was not changed. Enabling guest mode on production should be a
separate user decision with the production MAC list and desired include/exclude
mode.
