# Supported platforms

routerd targets one platform fully today and exposes groundwork for two
more. The matrix is intentionally explicit so operators do not assume
parity that the code does not yet provide.

## Tier 1 — Ubuntu (and other Debian-family Linux)

- Full source-install layout under `/usr/local`.
- systemd unit at `contrib/systemd/routerd.service`.
- Installed and tested in CI. All currently implemented resource kinds
  (interface aliases, IPv4 static/DHCP, dnsmasq DHCP/DHCPv6/RA, IPv6 PD
  through systemd-networkd drop-ins, conditional DNS forwarding, PPPoE,
  DS-Lite, IPv4 source NAT through nftables, IPv4 policy routing, IPv4
  default-route policy with health checks, reverse-path filters, MTU
  propagation, default-deny home-router firewall, sysctl, hostname,
  systemd-timesyncd, log sinks) work end-to-end.

## Tier 2 — NixOS (groundwork)

- A flake under `contrib/nix/` builds the same Go binaries via
  `buildGoModule`, ships a NixOS module that wires routerd into the
  systemd unit graph, and exposes a development shell.
- Reuses Linux renderers: netplan and systemd-networkd drop-ins are
  generated as on Ubuntu. On a NixOS host without netplan, point
  `--netplan-file` at a path inside `/etc/systemd/network/` (or set the
  flag to a discardable location until a NixOS-native renderer lands).
- A NixOS-native interface renderer that emits to `networking.*`
  options or to systemd-networkd `.network` files is not implemented
  yet.

## Tier 2 — FreeBSD (groundwork)

- `pkg/platform` declares FreeBSD defaults and feature flags. Cross-
  compiling `cmd/routerd` and `cmd/routerctl` for `GOOS=freebsd`
  succeeds.
- An rc.d script at `contrib/freebsd/routerd` is installed via
  `make install-rc-freebsd` (selected automatically by `make
  install-service` when `uname -s` is `FreeBSD`).
- Not yet implemented for FreeBSD:
  - pf renderer to replace nftables for source NAT and firewall.
  - rc.conf / `ifconfig`-based interface renderer to replace netplan
    and systemd-networkd.
  - mpd5 or native FreeBSD PPPoE wiring (Linux uses pppd / rp-pppoe).
  - dnsmasq orchestration via `service` instead of `systemctl`.
  - IPv6 prefix delegation via `rtsold` / `rtadvd` instead of
    systemd-networkd.

Until those land, `routerd reconcile` on FreeBSD will validate, plan,
and dry-run, but will refuse or no-op for resource kinds that depend on
Linux-only host integrations. Use `routerd validate` and `routerd plan
--dry-run` to test configurations on FreeBSD today.

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
