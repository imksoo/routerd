# Nix / NixOS support (groundwork)

This directory contains the experimental NixOS module for routerd. The
flake lives at the repository root so local and GitHub usage both build
the same source tree. NixOS is a planned Tier 2 platform; the module is
provided so routerd can be wired into a NixOS configuration today. See
`docs/platforms.md` for the current support matrix.

## Try the flake

```sh
nix build .#routerd
nix run .#routerctl -- status
nix develop
```

## Use the NixOS module

Add the flake as an input and import the module in your NixOS
configuration:

```nix
{
  inputs.routerd.url = "github:imksoo/routerd";
  outputs = { self, nixpkgs, routerd, ... }: {
    nixosConfigurations.example = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        routerd.nixosModules.default
        ({ pkgs, ... }: {
          services.routerd = {
            enable = true;
            package = routerd.packages.${pkgs.system}.routerd;
            configFile = ./router.yaml;
          };
        })
      ];
    };
  };
}
```

## Planned generated configuration flow

NixOS should keep persistent configuration in Nix, not in files that
routerd rewrites at runtime. The intended workflow is:

1. Keep the router intent in `router.yaml`, including a `NixOSHost`
   resource for host-level settings such as boot loader, users, SSH,
   sudo, and `system.stateVersion`.
2. Run `routerd render nixos --config router.yaml --out routerd-generated.nix`.
3. Import the generated file from a small hand-written
   `configuration.nix`.
4. Apply the persistent configuration with `nixos-rebuild switch`.
5. Run `routerd serve` for non-persistent runtime decisions such as
   health checks, active route selection, AFTR resolution, status
   reporting, and connection tracking observations.

The hand-written `configuration.nix` can stay minimal. For a lab example,
see `examples/nixos-edge-configuration.nix`:

```nix
{ config, pkgs, ... }:

{
  imports = [
    ./hardware-configuration.nix
    ./routerd-generated.nix
  ];
}
```

`routerd-generated.nix` is the file routerd owns and may overwrite.
Do not hand-edit it. If extra host-level settings are needed, put them
in `configuration.nix` or another imported file.

`routerd render nixos` intentionally writes only the generated Nix file.
It does not run `nixos-rebuild switch` and does not edit the
hand-written `configuration.nix`.

## Status

- Build via `buildGoModule`: working from the repository-root flake.
- Systemd unit: rendered by the module (mirrors `contrib/systemd/routerd.service`).
- NixOS generated configuration: `routerd render nixos` emits a
  `routerd-generated.nix` module for host settings, dependency
  packages, and basic systemd-networkd interface configuration. The
  module does not depend on netplan.
- Runtime apply on NixOS should be limited to non-persistent
  decisions until each resource has a Nix-native persistent rendering
  story.
