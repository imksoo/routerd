---
title: Troubleshooting
slug: /how-to/troubleshooting
---

# Troubleshooting

The general approach for routerd issues:

1. Look at routerd's view first: `routerctl describe <kind>/<name>`.
2. Compare it to the host's view (`ip`, `ss`, `journalctl`).
3. Re-run with `routerd apply --once --dry-run` to see what routerd
   thinks needs to change.
4. If the rendered files look wrong, debug in `routerd render`.
5. Only edit `/etc/...` directly as a last resort, and re-apply right
   after to put routerd back in sync.

## "Apply succeeds but the host didn't change"

routerd's apply is idempotent. If the rendered file already matches what
routerd would write, no system service is restarted. If you expected a
service restart and it didn't happen, run:

```bash
sudo routerd render linux --config /usr/local/etc/routerd/router.yaml > /tmp/want.txt
diff /tmp/want.txt /etc/<actual-file>
```

If the diff is empty, the host already matches the YAML. If the diff is
non-empty, look for permissions errors in `journalctl -u routerd`.

## "DHCPv6-PD never produces a prefix"

Order of investigation:

1. Is the OS DHCPv6 client running?
   - Linux: `networkctl status <wan-iface>`. Look for "DHCPv6 client:
     enabled" when `client: networkd` is used. For `client: dhcp6c`, check
     `systemctl status routerd-dhcp6c-<name>.service`.
   - FreeBSD: `service dhcp6c status`. Look for an active process.
2. Is the Solicit on the wire?
   - `sudo tcpdump -ni <wan-iface> -nn -vv 'udp port 546 or udp port 547'`
3. Is the Reply on the wire?
   - Same tcpdump. Reply may come from an ephemeral source port — do
     **not** filter on `src port 547`.
4. Is the path passing IPv6 multicast?
   - On Proxmox: `cat /sys/class/net/vmbr0/bridge/multicast_snooping`
     should be `0`.
   - L2 switches: IGMP/MLD snooping disabled, or an MLD querier is
     present.
5. Is the upstream actually delegating?
   - For NTT FLET'S, see [the FLET'S IPv6 how-to](./flets-ipv6-setup).

## "After a restart routerd lost its prefix"

routerd records the last observed prefix in
`/var/lib/routerd/routerd.db`. If a restart of the OS DHCPv6 client
sends a Release, the upstream may free the binding immediately. The
NTT profile defaults to suppressing Release on shutdown to avoid this.

To verify your DHCPv6 client is not sending Release on a managed restart:

- KAME/WIDE `dhcp6c`: check that the managed command includes `-n`.
- systemd-networkd: avoid using it for NTT home-gateway PD until the
  Renew/Rebind packet shape is verified in your environment.

## "routerctl describe shows nothing"

`routerctl` talks to the daemon's local socket, not the YAML. If the
daemon isn't running, describe is empty.

```bash
sudo systemctl status routerd.service        # Linux
sudo service routerd status                  # FreeBSD
```

A one-shot `routerd apply --once` updates state but does not start a
daemon. To inspect after one-shot, query the SQLite directly (see
[Operations: state database](../operations/state-database)).

## "I changed the YAML but apply does nothing"

Most likely the YAML failed validation, or the resource that should have
changed is owned by a different scope (`Interface.spec.managed: false`,
or another tool created the file). Re-run with:

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
sudo routerd apply --once --dry-run --config /usr/local/etc/routerd/router.yaml
```

The dry-run plan tells you which resources routerd would change and
which it would skip.

## Where the logs live

- Linux: `journalctl -u routerd.service`
- FreeBSD: `/var/log/messages` (look for `routerd[pid]`)
- routerd events table:
  `sqlite3 /var/lib/routerd/routerd.db 'select * from events order by id desc limit 20'`
