---
title: Reconcile and removal
---

# Reconcile and removal

routerd compares the intent declared in YAML with the host's current state. When they differ, routerd computes a plan, optionally previews it as a dry-run, and then applies it.

## Standard sequence

```bash
routerd validate --config router.yaml
routerd plan     --config router.yaml
routerd apply    --config router.yaml --once --dry-run
routerd apply    --config router.yaml --once
```

For a remote router, confirm that the management connection (SSH, console, hypervisor console) will survive the change before running the non-dry-run `apply`.

## Long-running mode

```bash
routerd serve --config router.yaml
```

In serve mode, routerd reacts to events on the bus and re-evaluates only the resources affected. Inputs include DHCPv6-PD renewals, health-check results, derived events, and configuration changes detected by inotify.

`routerd serve` runs the controller runtime directly from the declared config.
There is no public flag to choose a partial controller set; if a resource is
declared, the matching controller decides how to converge it, and `--dry-run`
remains a pre-apply check rather than a persistent operating mode.

When the config contains resources that forward traffic, such as
`IngressService`, `PortForward`, NAT, BGP, or static/policy routes, apply and
controller reconcile also converge the runtime kernel forwarding switches:
`net.ipv4.ip_forward=1` and `net.ipv6.conf.all.forwarding=1`. This is applied
even when the YAML does not include an explicit `SysctlProfile`, so a live ISO
or freshly booted router does not silently keep forwarding disabled.

## Drift checks

routerd does not treat the status database as the only source of truth. The
status store records what the previous apply observed, but controllers also
check the host state they are responsible for before deciding to skip work.
Examples include systemd unit enabled/active state, whether dnsmasq is running
with the expected config file, whether a DHCPv4 lease address is still present
on the interface, and whether managed nftables tables exist on the host.

This matters after a reboot, a failed manual edit, or an interrupted upgrade:
the status database can still say "Applied" while the OS state has drifted.
Controllers should converge the OS back to the declared YAML instead of
assuming that the previous status row is still true.

## Derived resources

Some host objects are generated from higher-level intent instead of being
written in YAML directly. For example, `routerd.service`,
`routerd-healthcheck@*.service`, firewall log daemons, and helper DPI services
are derived service units. Use this view to inspect those generated resources:

```bash
routerctl show derived-resources
```

If a removed or unsupported resource kind is still present in YAML, routerd
fails config loading instead of silently ignoring it.

## Managed cleanup

When a resource disappears from YAML, the owning controller removes or disables
only the artifacts it owns. Stale `routerd-healthcheck@*.service` and supervised
client daemon units are disabled and removed when no matching owning resource
remains. NAT44 clears the managed `routerd_nat` table or pf anchor when no NAT
rules remain.

Firewall rendering keeps the managed nftables table in place and reloads it in
one `nft -f` batch. For named sets such as firewall zone interface sets and
client-policy MAC sets, routerd destroys the managed set before redefining it so
removed elements do not remain. It does not destroy and recreate the whole
filter table during normal apply.

## Removal

routerd only deletes objects whose ownership it can attribute (i.e. that routerd previously created or adopted). It does not remove third-party configuration or manual changes.

Full rollback to a previous configuration is not in scope today. For changes that include deletions, always run `routerd plan` and `routerd apply --dry-run` first and confirm the deletion list before applying.

## See also

- [State and ownership](../concepts/state-and-ownership.md)
- [Apply and render](../concepts/apply-and-render.md)
- [Troubleshooting](../how-to/troubleshooting.md)
