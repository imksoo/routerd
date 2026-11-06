# Linux IPv6 static-route readback test

## Contract under test

Linux `ip -j -6 route` reports an IPv6 host route destination without an
explicit prefix (for example, `fd42:150::99`, not
`fd42:150::99/128`). The controller must normalize that observation to `/128`
before comparing it with the declared route. Linux `default` must likewise be
read as `::/0`. Configuration validation and exact route identity matching
remain unchanged.

Linux assigns IPv6 static routes a metric of 1024 when the configuration omits
`metric` or sets it to zero. The controller records that effective value in
its native ownership tuple and uses it for readback and deletion. A positive
configured metric is retained as specified. The configured status field can
still show zero; `routeOwner.route.metric` identifies the actual kernel value.

`pkg/controller/chain/ipv4route_test.go` covers application, observation,
status publication and withdrawal for IPv6 prefix, host and default routes.
The isolated runtime check below covers the Linux kernel readback path and
forwarding through a separate client.

## Isolated dataplane procedure

### Prerequisites

- A disposable Linux router VM running the package under test, with two
  isolated interfaces: one for a peer and one for a separate client.
- A peer address reachable through the router's first interface and a client
  address on the second interface. The peer must have a return route to the
  client; IPv6 forwarding must be enabled on the router.
- Root access to these test guests and a saved baseline from which both can be
  restored. Never attach this test topology to a production bridge.
- A test YAML declaring both interfaces, forwarding, the peer's
  `IPv6StaticRoute` as an explicit `/128` via the peer address, and the
  client's return route.

Use the actual interface names and addresses assigned by the isolated lab.
Keep the route destination, gateway, device and metric fixed for all checks.

### Apply and forward

On the router, from the directory containing the test YAML:

```sh
routerctl validate -f test-router.yaml
routerctl apply -f test-router.yaml
routerctl get IPv6StaticRoute/<route-name> -o json
ip -6 route show <destination>/128
```

Require validation and apply to succeed, status `phase: Installed` with
`routeOwner.pending: false`, and the kernel route to match the declared
destination, gateway, device and metric. The kernel may print the destination
without `/128`.

From the independent client VM, send exactly three probes with a two-second
per-probe timeout:

```sh
ping -6 -n -c 3 -W 2 <destination>
```

Require 3/3 replies. A probe from the router itself or from a namespace on the
router does not satisfy this forwarding check.

### Withdraw and reapply

On the router, withdraw only the test route:

```sh
routerctl delete IPv6StaticRoute/<route-name>
ip -6 route show <destination>/128
```

Require the kernel route to be absent. From the client, run two probes with a
one-second timeout and require the command to fail. Reapply the same test YAML
and repeat the apply/readback checks and the three-probe client check above.

### Recover after route loss

With the route declared and installed, simulate loss of kernel route state
while retaining routerd's desired state and ownership receipt:

```sh
systemctl stop routerd
ip -6 route del <destination>/128 via <gateway> dev <device> metric <metric>
systemctl start routerd
```

Poll for at most 20 seconds for the exact route identity to reappear. Require
`routerctl get IPv6StaticRoute/<route-name> -o json` to report
`phase: Installed` and `routeOwner.pending: false`, then require 3/3 client
probes. Do not loop indefinitely or infer recovery from service health alone.

### Failure handling and cleanup

On failure, inspect the resource with `routerctl describe`, the kernel route
with `ip -6 route show`, interface addresses and routes on both peers, and the
bounded recent service journal (`journalctl -u routerd -n 100 --no-pager`).
Compare the route identity and packet path before changing the test setup.
Keep the reusable commands and acceptance criteria here; do not retain raw
ping output, journal dumps or duplicate evidence bundles after the run.

Stop the disposable client and router VMs, restore their saved baselines, and
remove only media or disks created for this test after confirming they are
detached. Leave unrelated lab resources untouched.

## Latest run

On 2026-11-06, formal Linux package `1.5.0~20261106185121+g11cd2e05`
passed the two-VM check with a `/64`, a link-local gateway and no configured
metric. The native tuple used 1024. The independent client passed three ICMPv6
probes and an HTTP payload check; withdrawal blocked that payload, reapplication
restored it, and deleting the route while main was stopped was recovered on
startup. Empty configuration withdrew the route before fixture cleanup.

The first verifier incorrectly required `protocol: boot` from `ip -j`, which
omits that default. Reading `ip -j -d`, as the product does, corrected the
verifier; no product-state repair was performed. This run does not qualify
native default or host routes at the new revision, or FreeBSD.

### Earlier explicit-metric host route

Dataplane smoke on an isolated Linux router package built from
`4f81296b4fd5ca17b5901aeb4499b4572bc4a271`: apply/readback, independent VLAN
client forwarding, withdrawal, reapplication, and recovery after deleting the
kernel route while routerd was stopped all passed. Client probes returned 3/3
before and after withdrawal recovery; they failed as expected while the route
was withdrawn. The status reported `Installed` with no pending ownership after
recovery.

This verifies the Linux host-route readback and this isolated forwarding path.
It is not a clean-install qualification, a full routerd 1.0 feature-parity
claim, or a qualification of the complete routerd 1.5 release.
