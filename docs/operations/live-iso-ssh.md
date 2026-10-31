---
title: SSH remote management on the live ISO
---

# SSH remote management on the live ISO

The current live ISO uses Ubuntu and runs `/opt/routerd-live/firstboot.sh` from
`routerd-live-setup.service`. Firstboot regenerates SSH host keys, imports root
public keys from NoCloud or supported provider user-data, and attempts to enable
and start the bundled `ssh.service` (or `sshd.service`).

The script calls SSH setup even when it finds no public key. An empty key file
does not mean the daemon is stopped. The current Ubuntu path does not implement
the older `routerd.ssh=1` boot flag or `/usr/share/routerd/live-ssh.sh` entrypoint.
Check the running service and effective authentication settings before using a
management network.

## Supplying public keys

Attach a NoCloud config drive labeled `cidata` or `CIDATA`, with a `user-data`
file containing `ssh_authorized_keys`:

```yaml
#cloud-config
hostname: routerd-lab
ssh_authorized_keys:
  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... admin@example
```

See [PVE NoCloud bootstrap](../how-to/pve-live-iso-nocloud) for the config-drive
attachment and optional `routerd.config_url` settings. Only public keys belong in
`user-data`; keep the corresponding private key on the management client.

Firstboot installs imported keys into `/root/.ssh/authorized_keys`, with mode
`0600`, inside a root-owned directory with mode `0700`. It removes duplicate
lines when merging keys. A new live boot regenerates the image's SSH host keys,
so verify the new host identity through the local or serial console.

The separate `ROUTERD_CONFIG`/`ROUTERD` disk supplies `router.yaml` and its
`secrets/` directory. The current Ubuntu firstboot does not read a standalone
`authorized_keys` file from that disk or the older per-host key-directory layout.
Use NoCloud user-data for SSH keys.

## Checking the booted image

Use the local or serial console first. The builder permits passwordless root
login on those consoles; network authentication is controlled separately by
OpenSSH's effective configuration.

```sh
cat /etc/routerd-live-version /etc/routerd-live-commit
systemctl status routerd-live-setup.service ssh.service --no-pager
journalctl -b -u routerd-live-setup.service -u ssh.service --no-pager
ss -ltnp
/usr/sbin/sshd -T | grep -E '^(permitrootlogin|permitemptypasswords|passwordauthentication|pubkeyauthentication) '
```

Check `/root/.ssh/authorized_keys` and its ownership and modes when public-key
login fails. Firstboot logs to the setup service's journal; the obsolete
`/run/routerd/logs/routerd-ssh.log` path is not this entrypoint's diagnostic log.

These commands inspect the actual booted state. Source checks of the builder and
firstboot do not establish that SSH is listening, that a client can authenticate,
or that a particular management network is isolated. Keep ISO build, VM boot,
and remote-client results as separate checks.
