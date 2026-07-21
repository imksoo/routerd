# FreeBSD VM 115 native smoke (2026-07-19)

Scope: disposable PVE VM 115 (`routerd-freebsd-smoke`), FreeBSD 14.3-RELEASE
amd64, 2 vCPU, 4 GiB RAM. Its only NIC is on `vmbr404`, an isolated bridge with
no uplink or NAT. The routerd binaries were built with:

```text
make build-daemons-freebsd CGO_ENABLED=0
```

The static binaries and test configs were attached over a local ISO.

## #853 EgressRoutePolicy behavior

The marked policy was rejected by the native FreeBSD binary:

```text
net.routerd.net/v1alpha1/EgressRoutePolicy/marked uses mark/table/hash policy routing, which is not supported on FreeBSD; pf route-to parity is not implemented
```

The simple, mark-free priority failover policy passed native validation:

```text
+ /cdrom/routerd validate --config /cdrom/failover.yaml
config /cdrom/failover.yaml exists
config is valid
```

`routerd serve --config /cdrom/failover.yaml --controllers bgp` started its
control API on `/var/run/routerd/routerd.sock` and its read-only API on
`/var/run/routerd/routerd-status.sock`.

## routerctl plan socket diagnosis

The socket files and listeners are present:

```text
srw-rw-rw- 1 root wheel 0 Jul 19 11:10 routerd-status.sock
srw-rw-rw- 1 root wheel 0 Jul 19 11:10 routerd.sock
root routerd ... stream /var/run/routerd/routerd.sock
root routerd ... stream /var/run/routerd/routerd-status.sock
```

Nevertheless, native `routerctl plan -f /cdrom/failover.yaml --socket
/var/run/routerd/routerd.sock` reports `routerd serve is not reachable for
plan` and the underlying error is `SQL logic error: no such table: objects
(1)`. This is not a Unix-socket path or listener failure; it needs a separate
control-API/state initialization investigation before it is treated as a
FreeBSD code bug.

## Host tool checks

```text
pfctl -nf /etc/pf.conf
pfctl: /etc/pf.conf: No such file or directory
pfctl: cannot open the main config file!: No such file or directory

dnsmasq --test
/cdrom/smoke.sh: dnsmasq: not found

service sshd onestatus
sshd is running as pid 788.
```

The base FreeBSD installation has no `/etc/pf.conf` and no dnsmasq package;
the next smoke pass installs dnsmasq offline from a package ISO without adding
an uplink or NAT to `vmbr404`.
