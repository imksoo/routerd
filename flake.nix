{
  description = "routerd: declarative router resource reconciler";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      perSystem = flake-utils.lib.eachSystem systems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          src = pkgs.lib.cleanSourceWith {
            src = ./.;
            filter = path: type:
              let
                rel = pkgs.lib.removePrefix (toString ./. + "/") (toString path);
              in
                !(pkgs.lib.hasPrefix ".git/" rel)
                && !(pkgs.lib.hasPrefix "bin/" rel)
                && !(pkgs.lib.hasPrefix "local/" rel)
                && !(pkgs.lib.hasPrefix "website/build/" rel)
                && !(pkgs.lib.hasPrefix "website/.docusaurus/" rel)
                && !(pkgs.lib.hasPrefix "website/node_modules/" rel);
          };
          routerd = pkgs.buildGoModule {
            pname = "routerd";
            version = "0.0.0-dev";
            inherit src;
            vendorHash = "sha256-IJMCaeAtyw9XQwU98rJ8e1qqFvrLt6EccYjA45Rld4o=";
            subPackages = [ "cmd/routerd" "cmd/routerctl" ];
            doCheck = true;
            meta = with pkgs.lib; {
              description = "Declarative router resource reconciler";
              license = licenses.asl20;
              mainProgram = "routerd";
              platforms = platforms.linux;
            };
          };
        in {
          packages = {
            default = routerd;
            routerd = routerd;
          };

          apps = {
            routerd = flake-utils.lib.mkApp { drv = routerd; name = "routerd"; };
            routerctl = flake-utils.lib.mkApp { drv = routerd; name = "routerctl"; };
          };

          devShells.default = pkgs.mkShell {
            packages = with pkgs; [
              go
              gnumake
              jq
              dnsmasq
              nftables
              iproute2
              conntrack-tools
              ppp
            ];
          };

          checks.routerd-build = routerd;
        });
    in perSystem // {
      nixosModules.default = import ./contrib/nix/module.nix;
      nixosModules.routerd = import ./contrib/nix/module.nix;

      overlays.default = final: prev: {
        routerd = self.packages.${final.system}.routerd;
      };
    };
}
