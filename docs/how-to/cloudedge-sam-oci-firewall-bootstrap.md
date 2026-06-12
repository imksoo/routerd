# CloudEdge SAM: OCI Ubuntu image firewall bootstrap

![Diagram showing OCI Ubuntu guest firewall defaults blocking WireGuard and SAM forwarding, required bootstrap allowances, and routerctl doctor checks](/img/diagrams/how-to-cloudedge-sam-oci-firewall-bootstrap.png)

> Experimental (CloudEdge SAM). This documents **provider-image host firewall
> behavior** seen on OCI Canonical Ubuntu images used as SAM routers, and the
> routerd-owned allowances that must converge on a clean host.

## Symptom

On OCI, the Canonical Ubuntu 24.04 image boots with `iptables-nft` filter rules
that **reject inbound traffic except SSH/ICMP and reject all FORWARD traffic**.
With these defaults a SAM router will:

- receive no WireGuard handshake even though the OCI security list allows
  `UDP/51820` and the VNIC has `skipSourceDestCheck=true` — the host firewall drops
  the inbound WireGuard packets before they reach the `wg-hybrid` listener;
- not forward captured/overlay traffic — the default `FORWARD` reject blocks the SAM
  delivery path between the VNIC interface and `wg-hybrid`.

This is independent of the cloud security list / VNIC source-dest-check, which
operate at the fabric layer; the **guest OS firewall** is a separate layer that must
also permit the SAM paths.

## Required allowances (guest OS)

On each OCI SAM router, the host firewall must permit:

- **inbound `UDP/51820`** to the `wg-hybrid` WireGuard listener;
- **`FORWARD`** between the OCI VNIC interface (e.g. `ens3`) and `wg-hybrid` in both
  directions.

`WireGuardInterface.spec.listenPort` is routerd-owned on Linux: the
`WireGuardInterface` controller ensures an `INPUT` accept rule for that UDP
port and reports the result in `WireGuardInterface.status.hostFirewall`.

Forwarding allowances remain path-specific. For managed capture paths,
`RemoteAddressClaim` owns the capture-interface-to-tunnel `FORWARD` opening
that it needs. Until the full CloudEdge SAM path is green on clean OCI hosts,
keep `routerctl doctor hybrid` in the acceptance gate so image-level
reject-all `FORWARD` rules are detected instead of becoming a silent dataplane
failure.

## Diagnosing it

`routerctl doctor hybrid` surfaces guest-firewall reject-all `FORWARD`/`INPUT`
patterns that would block the WireGuard / SAM paths. `routerctl describe
WireGuardInterface/<name>` also shows whether the listen-port opening was
applied through `status.hostFirewall`. Run both on the OCI router after deploy:

```
routerctl doctor hybrid
routerctl describe WireGuardInterface/wg-hybrid
```

If the WireGuard endpoint shows no handshake while the peer is sending keepalives,
check the guest firewall first (this how-to), then the OCI security list, then the
VNIC source-dest-check.

## Related

- [Selective Address Mobility](../reference/selective-address-mobility)
- OCI Ubuntu images differ from AWS/Azure images in their default `iptables-nft`
  posture; AWS/Azure SAM smokes did not hit this because their images do not
  reject-all `FORWARD` by default.
