# IPv4 dynamic gateway and saved DHCP identity

## Requirement

A required `IPv4Route.spec.gatewayFrom` without a value or static fallback must
remain `Pending / GatewayMissing`, with no owned kernel route. Losing the source
withdraws the previous route instead of replacing it with an on-link route.
Acquisition restores forwarding. Explicit direct routes, optional sources and
static gateway fallback remain supported.

A DHCP client starting with a saved lease for a different interface, MAC address,
client identifier or resource must acquire afresh without requesting that old
address. It must not reuse that lease or loop on startup failure. A matching lease
still requires INIT-REBOOT confirmation; malformed matching snapshots and file
read failures remain errors.

## Local regression

From the repository root:

```sh
go test ./pkg/controller/chain -run 'TestOrdinaryRoute' -count=1
go test ./pkg/dhcpv4client ./cmd/routerd-dhcpv4-client -count=1
```

`ipv4route_gateway_test.go` validates a declared DHCP/interface/route configuration
and passes it through runtime selection with a real SQLite store. Supplied native
command responses verify cold absence, acquisition, loss and reacquisition.
`TestRestoreLeaseIdentityChangeStartsFreshDiscovery` exercises daemon restoration,
actual DHCP encoding and persistence. These are local tests, not kernel/peer proof.

## Isolated Linux procedure

Use a dedicated Ubuntu guest with the formal matching base/addon packages. Check
package version and `dpkg -V`, management access, empty canonical configuration,
and absence of old test links/routes. Do not delete or edit the saved DHCP lease
that reproduces an identity change. Stop the empty routerd service before setup.

1. Create an isolated veth pair with its peer in a separate namespace. Configure
   independent dnsmasq there to offer a short lease and its own address as router.
   Put a separate test /32 on the peer loopback and serve a fixed HTTP payload.
   Verify peer DHCP socket and HTTP locally before starting the product. Do not
   give the SUT an address or route manually.
2. Declare the SUT interface as external. Declare a DHCPv4Client with `useRoutes:
   false`, `useDNS: false`, gated by a TCP HealthCheck. Declare an IPv4Route to the
   peer's loopback /32 with the SUT device, a metric and `gatewayFrom` referencing
   `DHCPv4Client/wan`, field `gateway`. Leave `gateway` unset. Validate and apply.
3. Keep the health endpoint off. Within 90 seconds require `GatewayMissing`, no
   route ownership receipt, no exact /32 kernel route and no DHCP address.
4. Start the health endpoint. Within 90 seconds require a fresh DHCP lease,
   `Installed`, non-pending route ownership and `ip -j -d -4 route show <prefix>`
   matching gateway/device/metric. Fetch and compare the independent HTTP payload
   with a three-second timeout and the SUT interface explicitly selected.
5. Stop the health endpoint. Require the same absent-route/address checks. Start
   it again and repeat the positive checks. Restart routerd and repeat the positive
   checks without editing configuration, database, saved lease or kernel routes.
6. Apply an empty configuration. Require Healthy, no helper processes and no owned
   route/address **before** stopping peers or deleting fixture networks. Then stop
   fixture services, delete namespaces/veth and temporary files, and stop the guest.

Each wait is bounded to 90 seconds; the whole guest procedure should finish within
15 minutes. On failure, retain only the relevant status, exact route and daemon
error needed for diagnosis. End the run and reproduce locally before changing
product code. Never turn a failed native check into a pass by deleting the saved
lease or injecting product state. Reusable private lab orchestration belongs in
routerd-labs; production addresses and credentials do not belong in this document.

## Latest run

2026-11-06, Linux product `a6d4e3bb45376ade0d5119c435bd47e97c965764`, formal
base/addon `1.5.0~20261106191737+ga6d4e3bb`, dedicated Ubuntu guest with independent
dnsmasq and HTTP peer in a network namespace: **passed** cold pending, actual DHCP
acquisition, exact next-hop /32 readback, HTTP payload, conditional DHCP withdrawal,
route absence, reacquisition, main-service restart and full empty withdrawal.
Fixture cleanup completed after Healthy/no-effect checks.

The first run at `ee376c5a` passed cold route absence but DHCP startup failed because
a saved lease belonged to an earlier interface. The run ended without editing or
deleting that lease. Local test `313f3d7b` reproduced the startup failure; `a6d4e3bb`
keeps identity rejection in the protocol client while allowing the daemon to begin
fresh acquisition. The successful native run retained that old snapshot before
installation and observed the identity-change log and subsequent acquisition.

Full local Go passed (122 packages, 15 without tests); schemas, examples and the
four-locale website build passed at the product revision. This result covers
router-originated traffic to the independent off-link peer, not a separate LAN
client forwarding campaign, FreeBSD or complete routerd 1.0 parity.
