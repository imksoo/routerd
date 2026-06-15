---
title: Azure and PVE same-subnet SAM smoke
---

# Azure and PVE same-subnet SAM smoke

![Diagram showing Azure provider-secondary-IP capture, on-prem proxy-ARP capture, SAM /32 delivery routes, forwarding checks, and routerctl doctor verification](/img/diagrams/how-to-hybrid-azure-pve-same-subnet.png)

This guide captures the validated operational shape for an Azure routerd node
and an on-prem Proxmox VE routerd node that exchange selected `/32` addresses
with Selective Address Mobility (SAM). See the
[Selective Address Mobility reference](../reference/selective-address-mobility)
for resource semantics.

## Azure side

- Keep the Azure NIC secondary IP assigned in Azure. That provider-side object
  is what captures packets for the on-prem `/32`.
- Do not let the Ubuntu guest OS hold the captured `/32`. cloud-init or netplan
  may auto-assign secondary NIC IPs; suppress that configuration or remove it.
  routerd enforces this during reconcile for no-local capture: explicitly when
  the claim uses `configureOSAddress: false`, and also for BGP delivery to a
  remote owner even if provider capture keeps the secondary IP assigned in
  Azure. In that BGP case the Azure NIC owns ingress, while Linux forwards by
  proxy-neighbor, forwarding sysctls/rules, and the imported `/32` route instead
  of a local `/32` address.
- Enable IP forwarding on the Azure NIC and in Linux
  (`net.ipv4.ip_forward=1`).

## On-Prem PVE Side

- Use `proxy-arp` capture on the LAN or bridge interface that sees the local
  same-subnet hosts.
- Enable Linux forwarding. routerd enables `ip_forward` and `proxy_arp` for SAM
  through the normal sysctl path.
- Permit forwarding between the capture interface and the WireGuard tunnel for
  the captured `/32`. SAM does not add firewall or NAT rules.
- On cloud guest images, also check host firewall defaults before assuming the
  provider fabric is dropping packets. The router must accept the WireGuard UDP
  listen port, and it must permit forwarding between the capture interface and
  `wg-hybrid`. `routerctl doctor hybrid` warns on terminal iptables
  drop/reject patterns and missing SAM MSS clamp rules.

## Tunnel And Routing

- WireGuard should dial from on-prem to the Azure public IP.
- Set `persistentKeepalive` on the on-prem peer so NAT and cloud edge state stay
  warm.
- Run the initial smoke without UDRs. If you add UDR fallback later, avoid a
  same-subnet loop where Azure routes the captured `/32` back to the same router
  that is trying to deliver it.
- SAM delivery lowers each claim to a `/32` route over the tunnel interface; it
  does not change the default route.

## Verification

Run:

```sh
routerctl doctor hybrid
```

For `provider-secondary-ip` no-local capture, confirm the doctor reports the
captured `/32` absent from local `ip addr`, the delivery route points at the
tunnel, and `ip_forward=1`. This includes `configureOSAddress: false` claims and
BGP delivery to a remote owner. For `proxy-arp`, confirm `proxy_arp=1`, the
proxy neighbor exists, the delivery route points at the tunnel, and
`ip_forward=1`.

For low-MTU overlays, confirm `doctor hybrid` reports a SAM MSS clamp and
`nft list table inet routerd_mss` contains both capture-to-tunnel and
tunnel-to-capture rules for the selected `/32` path.
