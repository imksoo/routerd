# Supported platforms

routerd targets one platform fully today and exposes groundwork for two
more. The matrix is intentionally explicit so operators do not assume
parity that the code does not yet provide.

## Tier 1 — Ubuntu (and other Debian-family Linux)

- Full source-install layout under `/usr/local`.
- Makefile builds use `CGO_ENABLED=0` by default, so source installs and
  remote-install tarballs contain static Go binaries. This avoids dynamic
  loader surprises on minimal router hosts and NixOS systems.
- systemd unit at `contrib/systemd/routerd.service`.
- Firewall rendering permits WAN-side DHCPv6 client replies by UDP
  destination port 546 only. It must not require a server source port of
  547, because some home gateways reply from ephemeral UDP ports.
- Installed and tested in CI. All currently implemented resource kinds
  (interface aliases, IPv4 static/DHCP, dnsmasq DHCP/DHCPv6/RA, IPv6 PD
  through systemd-networkd drop-ins, conditional DNS forwarding, PPPoE,
  DS-Lite, IPv4 source NAT through nftables, IPv4 policy routing, IPv4
  default-route policy with health checks, reverse-path filters, MTU
  propagation, default-deny home-router firewall, sysctl, hostname,
  systemd-timesyncd, log sinks) work end-to-end.

## Tier 2 — NixOS (groundwork)

- The repository-root flake builds the same Go binaries via
  `buildGoModule`, ships a NixOS module from `contrib/nix/` that wires
  routerd into the systemd unit graph, and exposes a development shell.
- The NixOS module does not depend on netplan. Today, NixOS
  configurations should either observe externally managed interfaces or
  use Linux primitives that are already available on the host. Ubuntu's
  netplan renderer remains available for Ubuntu, but it is not a NixOS
  runtime dependency.
- Persistent NixOS settings are produced as Nix and applied with
  `nixos-rebuild switch`, not by having the daemon rewrite `/etc`
  configuration files. The flow is `routerd render nixos` producing
  `routerd-generated.nix`, a small `configuration.nix` importing it,
  then `routerd serve` handling non-persistent runtime decisions. The
  lab sample `examples/nixos-router02-configuration.nix` shows the
  hand-written side of this split.
- The current NixOS renderer emits host settings, dependency packages,
  persistent sysctl values, and basic systemd-networkd `.network`
  declarations. More resource kinds still need Nix-native persistent
  rendering.
- For router hosts, the generated NixOS module disables the built-in
  reverse-path firewall check. routerd's own firewall rules then apply
  the same DHCPv6 client rule as other Linux targets: accept UDP
  destination port 546 without constraining the server source port.

## Tier 2 — FreeBSD (groundwork)

- `pkg/platform` declares FreeBSD defaults and feature flags. Cross-
  compiling `cmd/routerd` and `cmd/routerctl` for `GOOS=freebsd`
  succeeds.
- An rc.d script at `contrib/freebsd/routerd` is installed via
  `make install-rc-freebsd` (selected automatically by `make
  install-service` when `uname -s` is `FreeBSD`).
- When building from Linux for a FreeBSD test host, pass
  `ROUTERD_OS=freebsd` to `make build`, `make dist`, or
  `make remote-install` so the installed binaries target FreeBSD.
- `routerd render freebsd` emits rc.conf values, dhclient.conf, and
  dhcp6c.conf. Runtime reconcile can apply this limited set with `sysrc`,
  `service netif`, and `service dhcp6c`.
- FreeBSD hosts need the base networking tools plus `jq`, `dnsmasq`, and `dhcp6`
  packages for the current groundwork. The `dhcp6` package provides the
  `dhcp6c` command and rc.d service used for DHCPv6-PD experiments.
- The FreeBSD DHCPv6-PD renderer configures delegated prefixes through
  `dhcp6c`. It does not yet honor `IPv6DelegatedAddress.spec.addressSuffix`
  because the packaged KAME `dhcp6c` configuration accepts `sla-id` and
  `sla-len` here but not the Linux-style interface identifier statement.
- Not yet implemented for FreeBSD:
  - pf renderer to replace nftables for source NAT and firewall.
  - mpd5 or native FreeBSD PPPoE wiring (Linux uses pppd / rp-pppoe).
  - dnsmasq orchestration via `service` instead of `systemctl`.
  - router advertisement service orchestration with `rtadvd`.

When the FreeBSD pf renderer is added, it must follow the same DHCPv6
client rule: accept WAN-side UDP destination port 546 without requiring
the server source port to be 547.

Until those land, `routerd reconcile` on FreeBSD only applies the supported
host pieces above plus runtime sysctl and hostname. Resource kinds that
depend on Linux-only host integrations are left for later platform-specific
renderers.

## How the platform is selected

`pkg/platform` resolves OS-specific defaults at compile time using Go
build tags. Renderers and the reconciler should consult
`platform.Current()` rather than `runtime.GOOS`. New OS-specific
behavior should be added in three places:

1. A new `platform_<os>.go` with build tags returning `Defaults` and
   `Features`.
2. A renderer guarded by `platform.Features.HasX` (or a build tag if
   the dependency would not even compile).
3. A `contrib/<os>/` directory with the service-manager integration.

## Non-goals (for now)

- Windows and macOS targets.
- Proprietary embedded router platforms.
- Containerized deployments. routerd manipulates host network state
  and is expected to run on the router itself.
