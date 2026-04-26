# Nix / NixOS support (groundwork)

This directory contains an experimental Nix flake and NixOS module for
routerd. NixOS is a planned Tier 2 platform; the flake is provided so
the module can be wired into a NixOS configuration today, but the
module currently relies on the same Linux renderers as the Ubuntu
target. See `docs/platforms.md` for the current support matrix.

## Try the flake

```sh
nix build ./contrib/nix#routerd
nix run ./contrib/nix#routerctl -- status
nix develop ./contrib/nix
```

## Use the NixOS module

Add the flake as an input and import the module in your NixOS
configuration:

```nix
{
  inputs.routerd.url = "github:imksoo/routerd?dir=contrib/nix";
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

## Status

- Build via `buildGoModule`: working in this scaffold.
- Systemd unit: rendered by the module (mirrors `contrib/systemd/routerd.service`).
- NixOS-native interface configuration: not yet wired. routerd still
  drops a `90-routerd.yaml` for netplan and systemd-networkd in the
  default code paths; on NixOS without netplan, prefer overriding
  `--netplan-file` to a path inside `/etc/systemd/network/`. A proper
  NixOS-native renderer is tracked in `docs/platforms.md`.
