# Install and upgrade

Download the Debian package and verify its checksum. On arm64, replace `linux-amd64` with `linux-arm64`. apt installs the OS packages it depends on and keeps an existing configuration.

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.deb
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.deb.sha256
sha256sum -c routerd-linux-amd64.deb.sha256
sudo apt install ./routerd-linux-amd64.deb
```

Upgrade by installing the newer package the same way. Before the files are replaced, the old package stops routerd and its helper services. The new package starts routerd again when `/usr/local/etc/routerd/router.yaml` exists, and routerd starts the helpers it needs. To pin a version, use a versioned file such as `routerd_YYYYMMDD.HHMM_amd64.deb`. Remove routerd with `sudo apt remove routerd`. For FreeBSD, see [FreeBSD getting started](tutorials/freebsd-getting-started.md).

[Releases](https://github.com/imksoo/routerd/releases/latest)

Change the example interface names, addresses and peer settings before using it. Validate locally, then inspect the plan on the target router. Use --replace only for a complete router configuration. Applying can change management access; keep a console.

```sh
routerd validate --config router.yaml
# On an existing router:
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
routerctl apply -f router.yaml --replace
```

On an existing router, inspect the diff and use routerctl. On a fresh Linux host, install the reviewed configuration and start routerd.service.

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 router.yaml /usr/local/etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
sudo routerctl get status
sudo routerctl get events --limit 20
```

For Live ISO, the working root and state database are volatile. Keep YAML and secrets on the configured persistent medium; do not swap a running VM’s backing ISO. Shut down, confirm it has stopped, then replace the medium and start it.

[Live ISO](how-to/pve-live-iso-nocloud.md) · [USB](operations/usb-persistence.md) · [DB](operations/state-database.md)
