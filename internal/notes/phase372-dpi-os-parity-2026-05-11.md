# Phase 3.7.2 DPI OS parity validation

Date: 2026-05-11

Scope:
- Validate `routerd-dpi-classifier` as an isolated process across Ubuntu, NixOS, and FreeBSD.
- Keep nDPI as an optional external runtime tool; routerd does not link against nDPI.

## Package availability

- Ubuntu / homert02: `libndpi-bin` provides `/usr/bin/ndpiReader`.
- NixOS / router02: `ndpi` is available through nixpkgs and provides `/run/current-system/sw/bin/ndpiReader` after declarative apply.
- FreeBSD / router04: `pkg search -x '^(ndpi|nDPI|libndpi)'` returned `ndpi-5.0.d20251224,1`; `pkg install ndpi` provides `/usr/local/bin/ndpiReader`.

## Validation evidence

### homert02 Ubuntu

- Binary set upgraded to the current Phase 3.7.2 build.
- `routerd-dpi-classifier.service`, `routerd-firewall-logger.service`, and `routerd.service` restarted.
- Classifier status:

```json
{"ok":true,"name":"default","version":"v20260511.1846","engine":"routerd-dpi-parser","ndpiTool":"ndpiReader","ndpiToolPath":"/usr/bin/ndpiReader","ndpiToolAvailable":true,"mode":"subprocess-ipc"}
```

- `routerctl status`: `phase=Healthy generation=53 resources=89`.
- IPv4 production smoke: `curl4=204`.

### router02 NixOS

- Added `ndpi` to declarative NixOS packages and `routerd-dpi-classifier.service` to the generated NixOS module.
- `routerd apply --once` wrote `/etc/nixos/routerd-generated.nix` and reached `phase=Healthy generation=6452 resources=61`.
- `systemctl is-active routerd-dpi-classifier.service`: `active`.
- `systemctl is-enabled routerd-dpi-classifier.service`: `enabled`.
- Classifier status:

```json
{"ok":true,"name":"default","version":"v20260511.1846","engine":"routerd-dpi-parser","ndpiTool":"/run/current-system/sw/bin/ndpiReader","ndpiToolPath":"/run/current-system/sw/bin/ndpiReader","ndpiToolAvailable":true,"mode":"subprocess-ipc"}
```

### router04 FreeBSD

- Added `ndpi` to `Package/service-deps` and explicit `SystemdUnit/routerd-dpi-classifier.service` rendered as rc.d.
- Fixed FreeBSD rc.d rendering to create `/var/run/${name}` before invoking `daemon -P/-p`; without this, the classifier child could start manually but the rc.d service could not create pidfiles.
- `routerd apply --once` installed `pkg:ndpi`, rendered `/usr/local/etc/rc.d/routerd_dpi_classifier`, and started it.
- `service routerd_dpi_classifier status`: `routerd_dpi_classifier is running as pid 28353.`
- `routerctl status`: `phase=Healthy generation=1135 resources=74`.
- Classifier status:

```json
{"ok":true,"name":"default","version":"v20260511.1846","engine":"routerd-dpi-parser","ndpiTool":"/usr/local/bin/ndpiReader","ndpiToolPath":"/usr/local/bin/ndpiReader","ndpiToolAvailable":true,"mode":"subprocess-ipc"}
```

## License/distribution note

`nDPI` remains an optional runtime dependency used through subprocess IPC. The routerd binaries and live ISO do not statically or dynamically link against nDPI in this phase. `THIRD_PARTY_LICENSES.md` already records nDPI as an optional LGPL-3.0 runtime dependency.
