# Alpine / OpenRC deployment

On Alpine Linux, routerd treats OpenRC as the service manager. A one-shot apply
is self-contained for routerd-managed local services:

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

For `VirtualIPv4Address` or `VirtualIPv6Address` resources with `mode: vrrp`,
routerd renders `/etc/keepalived/keepalived.conf`, installs an OpenRC
`keepalived` init script, enables it with `rc-update`, and restarts it with
`rc-service keepalived restart` when the rendered config changes. The generated
script runs `keepalived --config-test --use-file /etc/keepalived/keepalived.conf`
before starting the daemon.

`routerctl show vrrp` reports the observed role from the live interface state.
On Linux/OpenRC this is derived from `ip addr show`: the node that owns the
VIP address is `master`, and the peer that does not own it is `backup`.

To preview the Alpine output without touching the host, use:

```sh
routerd render alpine --config /usr/local/etc/routerd/router.yaml
```

The preview includes OpenRC init scripts and `keepalived.conf` when the config
contains a VRRP VIP. See `examples/k8s-routerd-vip-alpine.yaml` for a Kubernetes
API VIP example that shares one VIP for DNS on port 53 and API ingress on port
6443.

On the live ISO, the login wizard is skipped when
`/usr/local/etc/routerd/router.yaml` already exists. It can also be skipped from
the boot command line with:

```text
routerd.skip-wizard=1
```
