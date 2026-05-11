# Phase 3.4 A5: router01/router03 lab state

Date: 2026-05-11

## PVE placement

Both VMs are on pve06.

| host | vmid | status | net0 | net1 | net2 | mgmt IPAM |
| --- | ---: | --- | --- | --- | --- | --- |
| router01 | 115 | running | vmbr0 | svnet3 | svnet1 | 192.168.123.120 |
| router03 | 111 | running | vmbr0 | svnet3 | svnet1 | 192.168.123.125 |

## router01

SSH works as `nwadmin@192.168.123.120`.

Initial state:

- `/usr/local/sbin/routerd`: `routerd 0.3.0`
- no `/usr/local/etc/rc.d/routerd` service was installed
- `/usr/local/etc/routerd` was missing

Actions completed:

- Installed current FreeBSD binaries from `bin/freebsd-amd64`.
- Installed the FreeBSD rc.d script at `/usr/local/etc/rc.d/routerd`.
- Installed `local/router01.yaml` at `/usr/local/etc/routerd/router.yaml`.
- Added `Telemetry`, `Package/service-deps`, and `SystemdUnit/routerd.service` resources to `local/router01.yaml`.
- `routerd validate --config /usr/local/etc/routerd/router.yaml`: pass.
- `routerd apply --dry-run`: Healthy with resource count 24.
- `service routerd onestart`: started successfully.

Evidence:

```text
routerd v20260511.1240
routerd validate: config is valid
routerd apply --dry-run: Healthy
routerd is running as pid 20652
```

Remaining router01 blocker:

- The host has mgmt reachability but no default IPv4 route and an empty `/etc/resolv.conf`.
- `pkg install` cannot resolve `pkg.FreeBSD.org`.
- Non-dry-run apply cannot install dnsmasq until the host has an outbound package path.

Observed routing/DNS:

```text
192.168.123.0/24 via vtnet2 is present
ping 192.168.123.1 succeeds
ping 1.1.1.1 fails: No route to host
/etc/resolv.conf is empty
```

## router03

PVE IPAM says `router03` should be `192.168.123.125`, but pve06 sees ARP `FAILED` and SSH returns `No route to host` from the working environment.

Additional findings:

- QEMU guest agent is not configured.
- No serial interface is configured, so `qm terminal 111` cannot be used.
- `local/router03.yaml` still contains older resource shapes such as `DNSConditionalForwarder`; it does not validate against the current API.

Router03 is therefore not safely upgradable without first restoring a management path. Recommended next step is to add a temporary serial console or use VNC/console access, then update the local YAML to current resource shapes before non-dry-run apply.
