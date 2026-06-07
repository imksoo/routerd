# Alpine / OpenRC deployment

![Diagram showing Alpine and OpenRC deployment from routerd validation and render preview through OpenRC service management, keepalived config testing, live ISO wizard skipping, DHCP renewal, and VRRP status observation](/img/diagrams/operations-alpine-deployment.png)

On Alpine Linux, routerd treats OpenRC as the service manager. A one-shot apply
is self-contained for routerd-managed local services:

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

For `VirtualAddress` resources with `mode: vrrp`,
routerd renders `/etc/keepalived/keepalived.conf`, installs an OpenRC
`keepalived` init script, enables it with `rc-update`, and applies config
changes through the same VRRP controller path used by daemon mode. The
controller reloads keepalived with `rc-service keepalived reload` when the
daemon is already running and falls back to `restart` when needed. The generated
script runs `keepalived --config-test --use-file /etc/keepalived/keepalived.conf`
before starting the daemon.

`routerctl show vrrp` reports the observed role from the live interface state.
On Linux/OpenRC this is derived from `ip addr show`: the node that owns the
VIP address is `master`, and the peer that does not own it is `backup`.
`LAST_TRANSITION` is the time routerd or `routerctl show vrrp` most recently
observed the node's role changing, so a keepalived-only failover updates it when
the CLI next reads the live VIP ownership.

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

When neither condition is true, the live ISO waits 5 seconds at login before
starting the wizard. If there is no input, it exits the wizard path and leaves
the system running in ephemeral mode; start it later with
`/usr/share/routerd/install.sh configure`.

The live ISO starts `udhcpc` as a persistent DHCP client during the autostart
path so leases are renewed after boot. It sends a DHCP hostname derived from
`routerd.hostname=`, `routerd.live_hostname=`, the top-level Router
`metadata.name`, or a stable MAC-derived fallback. By default it does not send
DHCP option 61, so servers that identify clients by Ethernet MAC keep seeing the
same client identity. To force a specific DHCP client ID, pass a hex value with
`routerd.dhcp_client_id=`.

With the example Kubernetes VIP profile and a 1 second `advertInterval`,
stopping keepalived on the active node should move the VIP to the backup in
roughly a few seconds. The keepalived detection window is approximately
`advertInterval * 3`; reclaim by the higher-priority node then follows the
configured `preemptDelay` plus the next advert convergence window.
