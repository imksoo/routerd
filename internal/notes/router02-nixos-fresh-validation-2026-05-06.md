# router02 NixOS Fresh Validation - 2026-05-06

Scope:
- Target: router02 at 192.168.123.124 on pve06.
- Source of truth: `local/router02/router.yaml`.
- Build: current main plus router02 validation fixes.
- Cleanup scope: routerd-managed runtime state, `/usr/local/sbin/routerd*`, `/run/routerd`, `/var/lib/routerd`, old DS-Lite links. The NixOS base installation was not reinstalled.

Validation:
- `routerd validate --config /usr/local/etc/routerd/router.yaml`: passed.
- `routerd apply --once --dry-run`: wrote `/etc/nixos/routerd-generated.nix` and completed `nixos-rebuild test`.
- `routerd apply --once`: completed NixOS module application and runtime resources.
- `routerctl status`: Healthy, 57 resources.
- Management path: `ens20` kept `192.168.123.124/24` throughout.

Observed runtime:
- `routerd.service`: active.
- `routerd-dhcpv6-client@wan-pd.service`: active.
- `routerd-dnsmasq.service`: active.
- `routerd-healthcheck@internet-via-dslite-{a,b,c}.service`: active.
- DHCPv6-PD: Bound, `2409:10:3d60:1230::/60`.
- DS-Lite tunnels:
  - ds-lite-a: remote `2404:8e00::feed:100`, local `2409:10:3d60:1230::100`.
  - ds-lite-b: remote `2404:8e00::feed:101`, local `2409:10:3d60:1230::101`.
  - ds-lite-c: remote `2404:8e00::feed:102`, local `2409:10:3d60:1230::102`.
- Health checks for all three DS-Lite tunnels reached Healthy.
- DNS resolver answered `gw.transix.jp` AAAA and local `router.router02.internal`.
- IPv4 external curl via router02 succeeded with HTTP 200.
- Web Console `/api/v1/summary` returned Healthy.
- OTLP collector on nwadmin03 received router02 `routerd` and `routerd-healthcheck` telemetry.

Fixes found during validation:
- `routerd.service` on NixOS did not have `nixos-rebuild` in PATH. The NixOS apply helper now prefers `/run/current-system/sw/bin/nixos-rebuild`.
- controller-chain dnsmasq did not set a lease file. On NixOS this caused dnsmasq to look for `/var/lib/misc/dnsmasq.leases`. It now writes the lease file beside the managed pid/config under routerd runtime state.
- Runtime systemd unit writes are not appropriate on NixOS because units are generated through the NixOS module. `local/router02/router.yaml` keeps `--controller-chain-dry-run-systemd-unit=true`.
