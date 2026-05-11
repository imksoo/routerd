# Phase 3.4 A1 OS resource validation

Date: 2026-05-11

Scope:
- Package / Sysctl / NetworkAdoption OS behavior baseline.
- Hosts: router02 (NixOS), router04 (FreeBSD), homert02 (Ubuntu).

Results:
- router02: latest static Linux binaries installed, local/router02/router.yaml applied, routerctl status Healthy, generation 6053, resourceCount 60. ens20 management address 192.168.123.124/24 stayed reachable.
- router04: latest FreeBSD binaries installed, local/router04.yaml applied, routerctl status Healthy, generation 1123, resourceCount 73.
- homert02: latest static Linux binaries installed, local/homert02.yaml applied, routerctl status Healthy, generation 52, resourceCount 88.

Fixes made:
- NixOS Package resources now report Applied/NixOSDeclarativePackageSet because package installation is represented by generated NixOS configuration.
- NetworkAdoption on NixOS now reports Applied/NixOSDeclarativeNetworkConfig and does not run Linux drop-in commands.
- NixOS renderer no longer turns disableDHCPv4 into DHCP=ipv6.
- Validation rejects NetworkAdoption on interfaces listed in spec.apply.protectedInterfaces.

Incident note:
- An earlier local experiment tried NetworkAdoption on the router02 mgmt interface. It made management unreachable after nixos-rebuild.
- router02 was recovered via PVE disk repair. The final committed YAML does not adopt mgmt, and validation now rejects this class of config.
