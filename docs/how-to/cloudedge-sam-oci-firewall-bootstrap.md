# CloudEdge SAM: OCI Ubuntu image firewall bootstrap

![Diagram showing OCI Ubuntu guest firewall defaults blocking WireGuard and SAM forwarding, required bootstrap allowances, and routerctl doctor checks](/img/diagrams/how-to-cloudedge-sam-oci-firewall-bootstrap.png)

> Experimental (CloudEdge SAM). This is **host bootstrap / provider-image
> behavior**, not a routerd dataplane concern. It applies to OCI Canonical Ubuntu
> images used as SAM routers.

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

On each OCI SAM router, ensure the host firewall permits:

- **inbound `UDP/51820`** to the `wg-hybrid` WireGuard listener;
- **`FORWARD`** between the OCI VNIC interface (e.g. `ens3`) and `wg-hybrid` in both
  directions.

Express these declaratively in the router config as part of host bootstrap (the
same way other "router prerequisites" are declared so a clean host proves them),
rather than relying on ad-hoc `iptables` rules that do not survive a rebuild.

## Diagnosing it

`routerctl doctor hybrid` surfaces guest-firewall reject-all `FORWARD`/`INPUT`
patterns that would block the WireGuard / SAM paths, so a missing allowance is
reported rather than appearing as a silent "no handshake". Run it on the OCI router
after deploy:

```
routerctl doctor hybrid
```

If the WireGuard endpoint shows no handshake while the peer is sending keepalives,
check the guest firewall first (this how-to), then the OCI security list, then the
VNIC source-dest-check.

## Related

- [Selective Address Mobility](../reference/selective-address-mobility)
- OCI Ubuntu images differ from AWS/Azure images in their default `iptables-nft`
  posture; AWS/Azure SAM smokes did not hit this because their images do not
  reject-all `FORWARD` by default.
