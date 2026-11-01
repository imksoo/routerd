# routerd 1.5 current Linux PVE DHCPv6-PD qualification

## Scope

This record qualifies the Linux DHCPv6 prefix-delegation, delegated-address,
router-advertisement and IPv6 forwarding path in the binary built from source
revision `25d761283bd65b350a4bb7d3fa19b669b1328c49`.

The isolated PVE topology used VM219 as the router and VM220 as independent
upstream and LAN-peer namespaces. The router WAN and peer upstream used VLAN
3132. The router LAN and independent client used VLAN 3131. Management remained
on VLAN 3133. VM220 ran the stock Kea 2.4.1 DHCPv6 server, not a routerd test
double.

The temporary configuration was a standalone routerd 1.5 configuration with 26
resources. It used no routerd 1.0 configuration, state database, handoff marker,
or cross-version migration path.

## Observed result

- Kea delegated `2001:db8:1500::/60` to the routerd DHCPv6 client on VM219 and
  recorded subsequent renewals.
- routerd configured `2001:db8:1500::1/64` on the LAN interface and installed
  the explicitly owned upstream route `2001:db8:17::/64` with Linux metric
  `1024`. The configuration declaration left its metric unspecified.
- The independent VM220 LAN namespace received a non-tentative SLAAC address in
  `2001:db8:1500::/64` and an IPv6 default route learned by router advertisement.
- An HTTP request from that namespace to `2001:db8:17::1` crossed VM219. The
  upstream service observed the client source address in `2001:db8:1500::/64`.
- After `systemctl restart routerd`, delegation, the LAN address, router
  advertisement route and independent HTTP traffic recovered. The final status
  query reported `Healthy` with 26 resources.

One immediate status read during the restart sequence reported `ERROR` before
the subsequent reconciliations completed. A later JSON status query reported
`Healthy`; the cause of that transient status transition was not separately
determined by this check.

## Unverified scope

This check does not establish prefix renumbering, lease expiry withdrawal,
upstream loss and return, high availability, target-device qualification, or
FreeBSD behavior. Those cases remain separate Linux-first work.
