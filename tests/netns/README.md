# Network Namespace Tests

These host-network integration tests are intentionally outside `go test ./...`.
They create Linux network namespaces, veth pairs, temporary keepalived state,
and nftables rules inside test namespaces. Run them only on a disposable Ubuntu
24.04-style host with `iproute2`, `keepalived`, `nftables`, and Python 3
installed.

Scenario prerequisites also include `timeout` (coreutils), `ping`
(iputils-ping), `arping` (iputils-arping), and `iptables`. Go uses the repository
toolchain when a case builds a driver. The disposable host must support the
namespace/veth/VXLAN/bridge/firewall kernel facilities used by the selected case.

| Cases | Additional prerequisite |
| --- | --- |
| `keepalived-vip-failover`, `keepalived-no-spurious-restart` | keepalived; the latter also builds with Go |
| `graceful-vrrp-activation` | arping; Go or `ROUTERD_GRACEFUL_VRRP_DRIVER` |
| `ingress-conntrack-survive`, `forcefrag-df-forward`, `vxlan-l2-mss-clamp` | nftables; forcefrag also needs ping |
| `nat44-portforward` | curl; Go builds the current routerd validator and NAT controller |
| `nat44-source-snat` | curl; Go builds the current routerd validator and NAT controller |
| `nat44-flow-dnat-pinhole` | nftables; Go builds the current routerd validator and NAT controller |
| `local-service-redirect` | dnsmasq, `dig`; Go builds the current routerd validator and NAT controller |
| `sam-empty-forward-chain` | iptables/nftables; Go or `ROUTERD_SAM_FORWARD_PROBE` |
| `arp-observer-ignore-member-mac`, standalone `vxlan-when-ha-active-standby` | Go builds the observer/controller |
| `sam-proxy-arp-garp-transition`, `vxlan-l2-control-plane-transparency` | Common iproute2/Python/timeout and corresponding kernel facilities |
| Standalone `vrrp-vmac-lan-address-lifecycle` | Explicit `ROUTERD_RUN_NETNS=1`; built vmac helper or `ROUTERD_VRRP_VMAC_BIN`; unshare/mount/ip/grep/sha256sum |
| `render-compatibility` | make/Go; render golden target without root |

`run-all.sh` selects its listed scenarios. It does not implicitly run standalone
cases.

Every script requires explicit root privileges and cleans up its namespaces and
temporary files with a trap. Successful runs remove temporary output. A failed
run retains only a temporary diagnostic directory until its cause is resolved.

```sh
cd tests/netns
sudo ./run-all.sh
```

To run one scenario:

```sh
sudo ./keepalived-vip-failover.sh
sudo ./keepalived-no-spurious-restart.sh
sudo ./graceful-vrrp-activation.sh
sudo ./ingress-conntrack-survive.sh
sudo ./nat44-portforward.sh
sudo ./nat44-source-snat.sh
sudo ./nat44-flow-dnat-pinhole.sh
sudo ./local-service-redirect.sh
sudo ./forcefrag-df-forward.sh
sudo ./sam-empty-forward-chain.sh
sudo ./vxlan-l2-control-plane-transparency.sh
sudo ./vxlan-l2-mss-clamp.sh
sudo ./dhcpv6-stateful.sh
sudo ROUTERD_RUN_NETNS=1 ./vrrp-vmac-lan-address-lifecycle.sh
./render-compatibility.sh
```

`make run` is a convenience wrapper for `sudo ./run-all.sh`.

The scripts cover:

| Script | Check |
| --- | --- |
| `keepalived-vip-failover.sh` | Two keepalived instances move a VIP to standby within advert/preempt timing. |
| `keepalived-no-spurious-restart.sh` | Repeated routerd VRRP reconciles do not restart an unchanged keepalived instance for 60 seconds. |
| `graceful-vrrp-activation.sh` | An elected router withholds its IPv4 VIP until a readiness status matches, sends gratuitous ARP on activation, and removes the VIP again on BACKUP. |
| `vrrp-vmac-lan-address-lifecycle.sh` | The role hook retains staged delegated IPv6 addresses, records graceful election ownership, removes the deferred VIP before BACKUP, and never publishes it directly on MASTER. |
| `ingress-conntrack-survive.sh` | Existing DNAT conntrack flows stay on the old backend while new flows use the new backend. |
| `nat44-portforward.sh` | A prepared 1.5 config passes validation; the production NAT controller serves WAN DNAT and LAN hairpin traffic, then withdrawal resets an existing flow and blocks new flows. |
| `nat44-source-snat.sh` | An independent WAN HTTP peer observes selected static SNAT, excluded destination/source behavior, address-reference updates and withdrawal through the production NAT controller. |
| `nat44-flow-dnat-pinhole.sh` | Independent UDP peers verify production flow learning and response DNAT, source/destination/interface/port filters, timeout, withdrawal, and changed-policy re-add without reviving the prior client's learned tuple. |
| `local-service-redirect.sh` | The production NAT controller redirects selected IPv4/IPv6 TCP/UDP DNS traffic by destination and LAN interface, applies an interface change, and restores upstream delivery after withdrawal. |
| `forcefrag-df-forward.sh` | Linux nftables `routerd_forcefrag` clears IPv4 DF on an oversized forwarded packet before a low-MTU egress link. |
| `sam-empty-forward-chain.sh` | The real SAM controller treats an absent forwarding chain as a no-op, preserves incompatible existing chains as errors, and removes stale owned forwarding rules and `accept_local`. Run with both Ubuntu 22.04 iptables-nft 1.8.7 and a newer backend. |
| `arp-observer-ignore-member-mac.sh` | `routerd-arp-observer` ignores configured SAM member sender MACs while preserving real-client observations on passive packet and ARP table scan paths. |
| `vxlan-l2-control-plane-transparency.sh` | A unicast-peer `VXLANTunnel` bridge carries ARP, DHCPv4 Discover/Offer, IPv6 RS/RA/NS/NA, and DHCPv6 Solicit/Reply between isolated endpoint namespaces. |
| `vxlan-l2-mss-clamp.sh` | A/B proves bridge-family IPv4/IPv6 SYN MSS lowering across a VXLAN bridge, preserves smaller MSS and non-TCP Ethernet frames, then removes the owned policy. |
| `dhcpv6-stateful.sh` | The rendered DHCPv6 server completes IA_NA Solicit/Advertise/Request/Reply with independent `dhcpcd` clients in `stateful` and `both` modes and assigns from the configured pool. |
| `render-compatibility.sh` | Non-root render golden compatibility check for Linux and FreeBSD/rc.d output snapshots. |

Do not add tests here that mutate the default host namespace. New scenarios must
create their own namespaces and links, run with explicit `sudo`, and tear down
all host artifacts on exit.

`sam-empty-forward-chain.sh` builds its small controller test driver with Go.
For a guest without Go, build `./tests/netns/sam-forward-probe` with
`CGO_ENABLED=0` and set `ROUTERD_SAM_FORWARD_PROBE` to that executable. Select
the iptables backend through `PATH`; do not change the host's alternatives.
The test driver refuses to execute in the PID 1 network namespace.

Host veth ends are registered for cleanup immediately after creation, before namespace moves or bridge attachment. The existing Go tests cover partial preparation and retained cleanup failures without root or namespace operations: `go test ./tests/netns`.

## DHCP address-family isolation

The Linux `dhcp-family-isolation.sh` test creates two isolated namespaces and
an IPv6-only macvlan. It reproduces duplicate DHCPv4 delivery with dnsmasq 2.90,
then loads the actual candidate renderer output. Completed DHCPv4 offer/ACK,
DHCPv6 information reply containing the requested DNS server, a positive-lifetime
RA, and the guard counter are required. Missing replies are acquisition failures;
the log's absence alone cannot establish success. No production network is used.

```sh
rules="$(mktemp /tmp/routerd-dhcp-family.XXXXXX)"
trap 'rm -f -- "$rules"' EXIT
go run ./tests/netns/dhcp-family-render > "$rules"
sudo ./tests/netns/dhcp-family-isolation.sh /usr/sbin/dnsmasq "$rules"
```

The test needs `ip`, `nft`, `tcpdump`, Python 3 and an explicit dnsmasq binary.
Successful runs print the asserted result and delete temporary output. A failed
run prints a diagnostic path; remove it once the cause is understood and fixed.
The test never installs a daemon or writes network state outside its own
namespaces/veth pair. Its waits are bounded acquisition watchdogs, not
performance requirements. This covers DHCP/RA preservation, not HA failover or
production application connectivity. Do not add `no-dhcpv4-interface`: in
dnsmasq 2.90 it also suppresses IPv6 prefix construction.

## DHCPv4 reservation and dynamic pool

`dhcpv4-reservation.sh` renders the production DHCPv4 configuration, starts
dnsmasq in a private router namespace, and uses two independent BusyBox DHCP
clients. One client must receive its MAC reservation; the other may receive
any address in the configured dynamic pool. Both must receive the configured
router and DNS options, then renew the same address after dnsmasq restarts.
This covers reservation matching, pool allocation and lease-file recovery with
the actual renderer and daemon. It does not qualify package installation,
service-manager supervision, lease expiry, conflict handling, NAT or other OSes.

```sh
sudo ./tests/netns/dhcpv4-reservation.sh
```

The test needs Linux network namespaces, iproute2, dnsmasq, BusyBox and Go.
Successful runs remove temporary state and logs; failures retain the current
temporary directory for local diagnosis.

## DNS resolver runtime

`dns-resolver-runtime.sh` starts the actual `routerd-dns-resolver` binary in a
private router namespace. A separate `dig` client checks a local zone and
conditional upstream forwarding over both UDP and TCP, then checks that a
configuration reload and worker restart serve the current answer. The test
uses a separate dnsmasq process as the upstream; it does not exercise the
routerd controller or service manager.

```sh
sudo ./tests/netns/dns-resolver-runtime.sh
```

The test needs Linux network namespaces, iproute2, dnsmasq, `dig`, curl and Go.
Successful runs remove temporary state and logs; failures retain the current
temporary directory for diagnosis. This check does not cover DHCP-derived
lease updates or tombstones, negative replies, DoT/DoH, controller-driven
configuration changes, service-manager supervision or FreeBSD runtime.

## PortForward DNAT and hairpin

`nat44-portforward.sh` validates a prepared routerd 1.5 YAML config, then runs
the production NAT44 controller and nftables inside a private router namespace.
Independent WAN, LAN server and LAN client namespaces verify WAN DNAT, LAN
hairpin source translation and direct-LAN source preservation. Withdrawing the
rule must reset the established WAN TCP flow, remove the routerd NAT table and
prevent new WAN flows from reaching the backend. The test does not import 1.0
configuration or state, install a package, exercise the service manager, or
qualify pinholes, session synchronization, multi-backend ingress, or FreeBSD.

```sh
sudo ./tests/netns/nat44-portforward.sh
```

The test needs Linux network namespaces, iproute2, nftables, curl, Python 3 and
Go. Successful runs remove temporary state and logs; failures retain the
temporary directory for local diagnosis.

## NAT44 source SNAT selectors

`nat44-source-snat.sh` validates a prepared routerd 1.5 configuration and runs
the production NAT44 controller with nftables in a private router namespace.
An independent WAN HTTP peer observes the source address of forwarded client
traffic. The test checks SNAT selected by source and destination CIDRs, an
excluded destination and a client outside the selected source range. Changing
the `snatAddressFrom` reference must change the source on new connections;
withdrawing the rule removes the NAT table and restores the original client
source. Successful temporary output is deleted; failures keep the current
diagnostic directory for reproduction.

```sh
sudo ./tests/netns/nat44-source-snat.sh
```

The test needs Linux network namespaces, iproute2, nftables, curl, Python 3 and
Go.

## NAT44 dynamic UDP response pinhole

`nat44-flow-dnat-pinhole.sh` validates a prepared 1.5 configuration and runs
the production NAT44 controller with nftables in isolated router, WAN and LAN
namespaces. Independent UDP peers verify outbound learning, permitted response
DNAT, denied source/destination/port/interface cases, timeout and withdrawal.
It then changes the authorized source set and re-adds the pinhole under the same
resource name; an old learned client tuple must not be reused by the changed
policy. The nft map identity is scoped to the pinhole's policy and referenced
address sets. This is a clean 1.5 config test; it does not import 1.0 config or
state. Successful temporary output is deleted; a failed run keeps only its
current diagnostic directory until the cause is resolved.

```sh
sudo ./tests/netns/nat44-flow-dnat-pinhole.sh
```

The test needs Linux network namespaces, iproute2, nftables, Python 3 and Go.

## LocalServiceRedirect DNS interception

`local-service-redirect.sh` validates a prepared routerd 1.5 configuration and
runs the production NAT controller with real nftables in a private router
namespace. Independent LAN clients and a WAN-side dnsmasq distinguish the local
DNS answer from the upstream answer over IPv4/IPv6 and TCP/UDP. The checks cover
the configured destination-address set, destination port, ingress interface,
changing the selected LAN interface, and withdrawing the redirect so queries
reach the upstream again. A name-backed set then uses the production DNS resolver,
SQLite status and nftables updater. Changing the independent server's A/AAAA
answers must retire interception of the old addresses and intercept the new
addresses over TCP/UDP, without recreating NAT tables or editing stored status.
The configured one-second refresh interval elapses between controller calls;
this checks refresh decisions, not the daemon's scheduling loop. The resolver
configuration is bind-mounted only inside a private mount namespace.
This exercises a fresh 1.5 config; it does not import
old config or state. Successful temporary output is deleted; a failed run keeps
its current diagnostic directory for reproduction.

```sh
sudo ./tests/netns/local-service-redirect.sh
```

The test needs Linux network namespaces, iproute2, nftables, dnsmasq, BIND
`dig`, Python 3, Go, util-linux `unshare`/`mount` and IPv6 support in the kernel.

## Stateful DHCPv6 server

`dhcpv6-stateful.sh` requires `dnsmasq`, `dhcpcd`, Go and the common Linux
namespace tools. It uses the repository renderer and actual dnsmasq process;
`dhcpcd` supplies an independent DHCPv6 client. It asserts the complete IA_NA
exchange and an assigned address inside the configured range in both
`stateful` and `both` modes. It does not start an RA service, so it deliberately
does not assert a default route or routed ping. Those behaviors belong to the
separate RA and forwarding tests. Successful output and generated state are
removed; failure output remains temporary until diagnosis is complete.
