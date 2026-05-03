# Nix / NixOS support

This directory contains the NixOS integration groundwork for routerd. NixOS is a
second-tier target: static binaries, service-manager integration, and generated
NixOS configuration are actively used, while renderer parity with Ubuntu remains
incremental.

Phase 1.7 proved the important path on router02: `routerd-dhcpv6-client@wan-pd`
is now generated declaratively in `/etc/nixos/routerd-generated.nix`, survives
`nixos-rebuild switch`, and keeps the DHCPv6-PD lease Bound.

## Try the flake

```sh
nix build .#routerd
nix run .#routerctl -- status
nix develop
```

## Use the NixOS module

Add the flake as an input and import the module in your NixOS configuration:

```nix
{
  inputs.routerd.url = "github:imksoo/routerd";
  outputs = { self, nixpkgs, routerd, ... }: {
    nixosConfigurations.example = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        routerd.nixosModules.default
        ./configuration.nix
      ];
    };
  };
}
```

The generated configuration may include a concrete unit like this:

```nix
systemd.services."routerd-dhcpv6-client@wan-pd" = {
  description = "routerd DHCPv6 client wan-pd";
  after = [ "network-online.target" ];
  wants = [ "network-online.target" ];
  wantedBy = [ "multi-user.target" ];
  path = with pkgs; [ iproute2 ];
  serviceConfig = {
    Type = "simple";
    ExecStart = lib.concatStringsSep " " [
      "/usr/local/sbin/routerd-dhcpv6-client"
      "--resource" "wan-pd"
      "--interface" "ens18"
      "--socket" "/run/routerd/dhcpv6-client/wan-pd.sock"
      "--lease-file" "/var/lib/routerd/dhcpv6-client/wan-pd/lease.json"
      "--event-file" "/var/lib/routerd/dhcpv6-client/wan-pd/events.jsonl"
    ];
    Restart = "always";
    RestartSec = "5s";
    RuntimeDirectory = "routerd/dhcpv6-client";
    StateDirectory = "routerd/dhcpv6-client";
    ProtectSystem = "strict";
    ReadWritePaths = [ "/run/routerd" "/var/lib/routerd" ];
    RestrictAddressFamilies = [ "AF_UNIX" "AF_INET6" "AF_NETLINK" ];
    CapabilityBoundingSet = [ "CAP_NET_RAW" "CAP_NET_ADMIN" "CAP_NET_BIND_SERVICE" ];
    AmbientCapabilities = [ "CAP_NET_RAW" "CAP_NET_ADMIN" "CAP_NET_BIND_SERVICE" ];
  };
};
```

Use:

```sh
sudo nixos-rebuild test -I nixos-config=/etc/nixos/configuration.nix
sudo nixos-rebuild switch -I nixos-config=/etc/nixos/configuration.nix
```

Do not describe NixOS as fully equivalent to Ubuntu yet. Keep user-facing text to
"working groundwork" unless a renderer has been tested on a NixOS router VM.
