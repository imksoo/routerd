---
title: Isolate guest devices by MAC address
---

# Isolate guest devices by MAC address

`ClientPolicy` is routerd's guest mode. It classifies clients by MAC address on
a shared LAN and applies a stricter forwarding policy before the normal
zone-to-zone firewall matrix is evaluated.

The feature is useful when you do not want to build a separate VLAN yet, but
you still want a clear boundary between trusted devices and devices that should
only reach the public internet.

## Use cases

Common uses include:

- Home networks where a visitor phone, game console, appliance, or BYOD laptop
  should not reach the management network or home servers.
- Apartment or shared-house networks where the default should be guest access,
  and only explicitly listed devices become trusted.
- IoT isolation, where cameras, HEMS controllers, TVs, and speakers need DNS,
  DHCP, NTP, and internet access but do not need lateral access.
- Small office visitor networks where guests share a physical LAN for now, but
  internal RFC 1918 and ULA destinations must stay blocked.

For a complete example, see
[examples/guest-mode.yaml](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml).

## How it works

On Linux, routerd renders `ClientPolicy` into the nftables table
`inet routerd_filter`.

For each policy it generates:

- an nftables `ether_addr` set such as `client_policy_guest_devices`;
- `ether saddr @set` matches for `mode: include`;
- `ether saddr != @set` matches for `mode: exclude`;
- self-service accept rules for selected local router services;
- forwarding deny rules for private IPv4 and ULA IPv6 destinations;
- optional allow rules before the deny list.

The generated rules are placed early in the `input` and `forward` chains. That
means guest isolation narrows access before a `trust -> self` or `trust ->
trust` role-matrix accept rule can allow the packet.

`ClientPolicy` does not replace `FirewallZone`. The usual model still applies:

- `FirewallZone` decides the normal zone role of an interface.
- `FirewallPolicy` decides global logging behavior.
- `FirewallRule` adds explicit exceptions.
- `ClientPolicy` adds per-client restrictions inside a LAN-like zone.

DHCP leases are not required for MAC matching, because nftables sees the
Ethernet source address directly. `DHCPv4Reservation` is still useful because
it gives the same device a stable IP address and a stable name in DNS and the
Web Console.

## Specification

`ClientPolicy` belongs to `firewall.routerd.net/v1alpha1`.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    macs:
      - "18:ec:e7:33:12:6c"
    isolation:
      lanInternet: allow
      lanLAN: deny
      lanMgmt: deny
      mDNSBroadcast: deny
```

| Field | Required | Meaning |
| --- | --- | --- |
| `mode` | yes | `include` or `exclude`. |
| `interfaces` | no | LAN-side `Interface` references where the policy applies. `Interface/lan` and `lan` both resolve to the same interface. When omitted, routerd targets every `trust` `FirewallZone` interface. |
| `macs` | no | Short-form MAC list. In `include` mode these are guests. In `exclude` mode these are trusted. |
| `isolation` | no | High-level guest intent. `lanInternet`, `lanLAN`, `lanMgmt`, and `mDNSBroadcast` accept `allow` or `deny`. |
| `classification` | no | MAC address entries. Their meaning depends on `mode`. |
| `classification[].macAddress` | yes | Client MAC address. routerd normalizes the address before rendering. |
| `classification[].as` | no | `guest` or `trusted`. Empty means `guest` in include mode and `trusted` in exclude mode. |
| `classification[].name` | no | Human-readable device name. It is documentation for now. |
| `classification[].ipv4Reservation` | no | Name of a `DHCPv4Reservation`. Use a bare resource name such as `aiseg2`, not `DHCPv4Reservation/aiseg2`. |
| `guestServices` | no | Local router services allowed for guests. Default is `dhcp`, `dns`, `ntp`. Supported values are `dhcp`, `dns`, `ntp`, `mdns`, and `ssdp`. |
| `guestEgressDeny` | no | CIDR list denied for guest forwarding. Defaults to RFC 1918 plus ULA. |
| `guestEgressAllow` | no | CIDR list explicitly allowed before deny rules. |

Default `guestEgressDeny`:

- `10.0.0.0/8`
- `172.16.0.0/12`
- `192.168.0.0/16`
- `fc00::/7`

When `isolation.mDNSBroadcast: deny` is set, routerd drops guest mDNS, SSDP,
and NetBIOS discovery forwarding so a guest device does not browse LAN peers by
local multicast or broadcast discovery.

Allow rules are rendered before deny rules. This lets you create narrow
exceptions, such as a single printer or captive-portal helper, without opening
the whole private range.

## Example 1: minimal include mode

Only one MAC address is treated as a guest.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    interfaces:
      - Interface/lan
    classification:
      - macAddress: "18:ec:e7:33:12:6c"
        as: guest
        name: aiseg2
```

## Example 2: include mode with several devices

Multiple devices can share the same guest rule set.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: household-guests
  spec:
    mode: include
    interfaces:
      - Interface/lan
    classification:
      - macAddress: "18:ec:e7:33:12:6c"
        as: guest
        name: aiseg2
        ipv4Reservation: aiseg2
      - macAddress: "7c:2f:80:11:22:33"
        as: guest
        name: guest-phone
      - macAddress: "90:09:d0:44:55:66"
        as: guest
        name: smart-tv
```

## Example 3: exclude mode for BYOD

All clients become guests by default. Only listed MAC addresses remain trusted.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: byod-default-guest
  spec:
    mode: exclude
    interfaces:
      - Interface/lan
    classification:
      - macAddress: "bc:24:11:e0:8e:3a"
        as: trusted
        name: admin-laptop
      - macAddress: "4e:20:15:aa:e0:67"
        as: trusted
        name: owner-phone
```

## Example 4: custom deny and allow lists

This policy keeps the default private deny behavior but allows guests to reach
one printer.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-with-printer
  spec:
    mode: include
    interfaces:
      - Interface/lan
    guestEgressAllow:
      - 172.18.20.10/32
    guestEgressDeny:
      - 10.0.0.0/8
      - 172.16.0.0/12
      - 192.168.0.0/16
      - fc00::/7
    classification:
      - macAddress: "7c:2f:80:11:22:33"
        as: guest
        name: guest-phone
```

## Example 5: local discovery services

By default, guests can use DHCP, DNS, and NTP. If the router is also running a
local discovery proxy or relay, add `mdns` or `ssdp` deliberately.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: media-guests
  spec:
    mode: include
    interfaces:
      - Interface/lan
    guestServices:
      - dhcp
      - dns
      - ntp
      - mdns
      - ssdp
    classification:
      - macAddress: "90:09:d0:44:55:66"
        as: guest
        name: smart-tv
```

Only enable discovery services when you understand what the local relay exposes.
mDNS and SSDP are convenient, but they can reveal device names and service
metadata.

## Example 6: IoT isolation with reservations

Stable reservations make troubleshooting easier. They also make Web Console
client inventory and DNS records less ambiguous.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: thermostat
  spec:
    server: lan-v4
    macAddress: "02:11:22:33:44:55"
    hostname: thermostat
    ipAddress: 172.18.0.151

- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: iot-isolation
  spec:
    mode: include
    interfaces:
      - Interface/lan
    classification:
      - macAddress: "02:11:22:33:44:55"
        as: guest
        name: thermostat
        ipv4Reservation: thermostat
```

## DHCPv4Reservation integration

`classification[].ipv4Reservation` is a reference check. routerd validates that
the named `DHCPv4Reservation` exists. The firewall match is still based on MAC
address, not on the leased IP address.

This split is intentional:

- MAC matching catches the client before IP-layer policy decisions.
- A fixed IPv4 lease gives the device stable DNS and Web Console identity.
- If the device changes IP address, the guest isolation still follows the MAC.

When a device uses randomized MAC addresses, create the reservation and
classification for the actual MAC address used on that SSID or wired segment.

## Verify generated rules

Render or inspect the active nftables table:

```sh
routerd render nftables --config /usr/local/etc/routerd/router.yaml
sudo nft list table inet routerd_filter
```

Look for:

```nft
set client_policy_guest_devices {
  type ether_addr
  elements = { 18:ec:e7:33:12:6c }
}

iifname "ens19" ether saddr @client_policy_guest_devices udp dport 53 counter accept
iifname "ens19" ether saddr @client_policy_guest_devices ip daddr 10.0.0.0/8 counter log prefix "routerd client-policy guest-devices deny " drop
```

## Verify from a guest device

From a guest client:

```sh
curl -4 https://www.google.com/generate_204
curl -4 --connect-timeout 3 http://192.168.1.1/
curl -4 --connect-timeout 3 http://172.18.0.1:8080/
```

Expected result:

- public internet succeeds;
- private destinations time out or fail;
- DNS, DHCP, and NTP continue to work.

On the router, use tcpdump to see the packet path:

```sh
sudo tcpdump -ni ens19 ether host 18:ec:e7:33:12:6c
sudo nft list chain inet routerd_filter forward
```

The nftables counters on the generated `ClientPolicy` rules should increment
when private destinations are denied.

## Troubleshooting

### MAC address does not match

Check the MAC address seen by the router:

```sh
ip neigh show dev ens19
sudo tcpdump -eni ens19
```

Wireless clients often use different MAC addresses per SSID. Phones and laptops
may also use private randomized MAC addresses. Use the address visible on the
router-facing LAN, not the hardware address printed on the device.

### Guest can still reach private networks

Check these points:

- The policy references the correct `Interface`.
- The packet enters through that interface.
- `routerd apply` installed the current nftables table.
- `guestEgressAllow` does not contain a broad private prefix.
- Another path, such as a VPN client on the endpoint itself, is not bypassing
  the router.

### Guest cannot reach the internet

`ClientPolicy` only narrows private and self traffic. Internet failure usually
comes from route policy, NAT44, DS-Lite, DNS, or IP forwarding. Check:

```sh
routerctl status
sysctl net.ipv4.ip_forward
sudo nft list table ip routerd_nat
```

### guestServices ordering

`guestServices` only controls access to local router services. It does not
permit forwarding to private subnets. Forwarding exceptions belong in
`guestEgressAllow`.

## Security considerations

MAC-based isolation is useful, but it is not a cryptographic identity system.
A malicious user can spoof a trusted MAC address if they have enough control
over their device.

Use `ClientPolicy` as a practical home and small-office control, not as the
only boundary for hostile users. Stronger designs include:

- separate VLANs or SSIDs;
- WPA3 Enterprise or 802.1X;
- switch port isolation;
- per-device credentials;
- a dedicated guest bridge or VRF.

`ClientPolicy` is still valuable with those designs because it documents the
intended classification in routerd's resource model and gives consistent
firewall rendering.

## OS support

Linux nftables is supported.

FreeBSD pf does not provide the same MAC-based routed filtering model in the
path routerd uses for `FirewallZone` and `FirewallRule`. routerd therefore
returns an explicit unsupported error for `ClientPolicy` on FreeBSD instead of
silently applying a weaker policy.

Possible future FreeBSD designs include bridge-level filtering or a dedicated
layer-2 segmentation resource, but those should be designed separately because
they are not equivalent to routed pf rules.

## Related resources

- `FirewallZone`: assigns the interface to `trust`, `untrust`, or `mgmt`.
- `FirewallPolicy`: enables deny logging and shared firewall behavior.
- `FirewallRule`: expresses exceptions that are not tied to MAC
  classification.
- `DHCPv4Reservation`: gives a classified device a stable IPv4 address and
  hostname.
- `PathMTUPolicy`: still applies to forwarded guest traffic when its interface
  and route conditions match. Guest isolation does not bypass MSS clamping.
