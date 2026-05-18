# FreeBSD CARP validation notes

`tests/netns` is Linux-only, so CARP validation uses a FreeBSD VM or a VNET jail
instead of Linux network namespaces.

Use a non-production interface and documentation-prefix addresses. Confirm the
management interface is outside the test path before applying the config.

## Single-node smoke test

1. Render or apply `examples/freebsd-vrrp.yaml` on a FreeBSD host.
2. Confirm the CARP VHID is attached to the parent interface:

   ```sh
   ifconfig vtnet1 | grep 'vhid 70'
   ```

3. Confirm routerd reports the CARP backend:

   ```sh
   routerctl show vrrp
   ```

4. Remove the temporary VIP if the test was applied directly:

   ```sh
   ifconfig vtnet1 inet 192.168.70.10/32 -alias
   ```

## Two-node failover test

Use two FreeBSD hosts on the same L2 segment with the same `virtualRouterID`,
address, and authentication value. Set different priorities, then confirm the
lower-priority node reports `BACKUP` while the higher-priority node reports
`MASTER`. Stop the higher-priority node's routerd CARP service and verify the
backup becomes master.
