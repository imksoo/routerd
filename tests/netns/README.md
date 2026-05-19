# Network Namespace Tests

These host-network integration tests are intentionally outside `go test ./...`.
They create Linux network namespaces, veth pairs, temporary FRR/keepalived
state, and nftables rules inside test namespaces. Run them only on a disposable
Ubuntu 24.04-style host with `iproute2`, `frr`, `keepalived`, `nftables`, and
Python 3 installed.

Every script requires explicit root privileges and cleans up its namespaces and
temporary files with a trap:

```sh
cd tests/netns
sudo ./run-all.sh
```

To run one scenario:

```sh
sudo ./frr-config-rollback.sh
sudo ./keepalived-vip-failover.sh
sudo ./bgp-event-ordering.sh
sudo ./ingress-conntrack-survive.sh
sudo ./bgp-import-policy-reject.sh
```

`make run` is a convenience wrapper for `sudo ./run-all.sh`.

The scripts cover:

| Script | Check |
| --- | --- |
| `frr-config-rollback.sh` | FRR rejects a bad reload and keeps the previous running config. |
| `keepalived-vip-failover.sh` | Two keepalived instances move a VIP to standby within advert/preempt timing. |
| `bgp-event-ordering.sh` | Repeated 1 Hz-ish BGP peer flaps do not expose prefix observations before peer establishment observations. |
| `ingress-conntrack-survive.sh` | Existing DNAT conntrack flows stay on the old backend while new flows use the new backend. |
| `bgp-import-policy-reject.sh` | FRR import policy accepts allowed prefixes and rejects disallowed prefixes. |

Do not add tests here that mutate the default host namespace. New scenarios must
create their own namespaces and links, run with explicit `sudo`, and tear down
all host artifacts on exit.
