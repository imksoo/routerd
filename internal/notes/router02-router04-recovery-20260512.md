# router02/router04 SSH recovery and DPI backfill

Date: 2026-05-12
Phase: 3.12

## Scope

- Restore authenticated access to router02 and router04 without destructive VM operations.
- Backfill Phase 3.7 through Phase 3.11 binaries and verification on both routers.
- Confirm Healthy status, DPI-related daemons, connection observation, and Web Console/API enrichment paths.

## Access recovery

- Initial `imksoo@router02`, `imksoo@router04`, and direct root SSH attempts failed with public-key denial.
- The declarative local configs identify the managed operator account as `nwadmin`.
- `nwadmin@192.168.123.124` restored access to router02.
- `nwadmin@192.168.123.126` restored access to router04.
- Root access is available to the Proxmox hosts, not to the guest routers.

## Proxmox evidence

- `root@pve06` hosts both lab guests:
  - router02: VMID 114, running, management bridge `vmbr0`, LAN bridges `svnet3` and `svnet1`.
  - router04: VMID 131, running, management bridge `vmbr0`, LAN bridges `vmbr404` and `svnet1`, snapshot parent `pre-libedit-repair-20260511`.
- `root@pve07` does not host router02 or router04.
- No `qm destroy`, disk reset, or other destructive VM action was used.

## Backfill deployment

- Built and deployed routerd binaries at `v20260512.0633`.
- router02 received Linux static binaries through `rsync` into `/tmp/routerd-backfill-bin/`.
- router04 does not provide `rsync`; FreeBSD static binaries were copied with `scp`.
- Existing `/usr/local/sbin` binaries were backed up under timestamped `/usr/local/sbin/routerd-backup-phase312-*` directories before replacement.

## router02 verification

- `/usr/local/sbin/routerctl version`: `routerctl v20260512.0633`.
- `/usr/local/sbin/routerd version`: `routerd v20260512.0633`.
- `routerd.service`, `routerd-dpi-classifier.service`, and `routerd-firewall-logger.service` are active.
- `sudo /usr/local/sbin/routerctl status` reports `Healthy`, generation 7047, resource count 61.
- Web Console API responds on `http://192.168.123.124:8080`.
- `/api/v1/connections` returns Linux conntrack entries.
- `/api/v1/traffic-flows` returns recent traffic flow entries.
- `/api/v1/firewall-logs` returns recent deny entries including DPI hints such as `dpi.app=netbios`.

## router02 fix found during backfill

- `routerd-firewall-logger.service` previously logged repeated failures:
  - `conntrack destroy watcher stopped: exec: "conntrack": executable file not found in $PATH`
- NixOS had `conntrack` installed at `/run/current-system/sw/bin/conntrack` and `/run/current-system/sw/sbin/conntrack`, but the generated service PATH did not include those package-profile directories.
- Added a shared host command resolver that checks PATH and common host-profile directories, including NixOS `/run/current-system/sw/{bin,sbin}`.
- `routerd-firewall-logger` now resolves the conntrack destroy watcher command through this helper.
- `pkg/observe` and Web Console host command execution now use the same resolver.
- After redeploy, router02 shows a running watcher:
  - `/run/current-system/sw/bin/conntrack -E -e DESTROY -o timestamp,extended`
- No new `conntrack` PATH errors were emitted after the restart.

## router04 verification

- `/usr/local/sbin/routerctl version`: `routerctl v20260512.0633`.
- `/usr/local/sbin/routerd version`: `routerd v20260512.0633`.
- `file /usr/local/sbin/routerd /usr/local/sbin/routerd-firewall-logger /usr/local/sbin/routerd-dpi-classifier` reports FreeBSD 12.3 static binaries.
- `service routerd status`: running.
- `service routerd_dpi_classifier status`: running.
- `sudo /usr/local/sbin/routerctl status` reports `Healthy`, generation 1136, resource count 75.
- Web Console API responds on `http://192.168.123.126:8080`.
- `/api/v1/connections` returns FreeBSD pf state entries.

## Notes

- router02/router04 direct root SSH remains intentionally unavailable.
- router04 backfill should continue to use `scp` unless `rsync` is explicitly installed.
- On NixOS targets, service-managed host commands should not assume `/usr/bin` or inherited interactive shell PATH.
