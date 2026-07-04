# Network Namespace Tests

These host-network integration tests are intentionally outside `go test ./...`.
They create Linux network namespaces, veth pairs, temporary keepalived state,
and nftables rules inside test namespaces. Run them only on a disposable Ubuntu
24.04-style host with `iproute2`, `keepalived`, `nftables`, and Python 3
installed.

Every script requires explicit root privileges and cleans up its namespaces and
temporary files with a trap:

```sh
cd tests/netns
sudo ./run-all.sh
```

To run one scenario:

```sh
sudo ./keepalived-vip-failover.sh
sudo ./keepalived-no-spurious-restart.sh
sudo ./ingress-conntrack-survive.sh
sudo ./forcefrag-df-forward.sh
./render-compatibility.sh
```

`make run` is a convenience wrapper for `sudo ./run-all.sh`.

The scripts cover:

| Script | Check |
| --- | --- |
| `keepalived-vip-failover.sh` | Two keepalived instances move a VIP to standby within advert/preempt timing. |
| `keepalived-no-spurious-restart.sh` | Repeated routerd VRRP reconciles do not restart an unchanged keepalived instance for 60 seconds. |
| `ingress-conntrack-survive.sh` | Existing DNAT conntrack flows stay on the old backend while new flows use the new backend. |
| `forcefrag-df-forward.sh` | Linux nftables `routerd_forcefrag` clears IPv4 DF on an oversized forwarded packet before a low-MTU egress link. |
| `arp-observer-ignore-member-mac.sh` | `routerd-arp-observer` ignores configured SAM member sender MACs while preserving real-client observations on passive packet and ARP table scan paths. |
| `render-compatibility.sh` | Non-root render golden compatibility check for Linux and FreeBSD/rc.d output snapshots. |

Do not add tests here that mutate the default host namespace. New scenarios must
create their own namespaces and links, run with explicit `sudo`, and tear down
all host artifacts on exit.
