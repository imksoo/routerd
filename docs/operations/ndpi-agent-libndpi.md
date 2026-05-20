---
title: nDPI agent native package
---

# nDPI agent native package

routerd's normal Linux release archives are built with `CGO_ENABLED=0` and keep
all included routerd binaries statically linked. The optional
`routerd-ndpi-agent-libndpi` archive is the exception package for hosts that
need native nDPI classification.

This archive contains only:

- `bin/routerd-ndpi-agent`
- `share/doc/README.md`
- `share/doc/VERSION`
- `share/doc/TARGET`

The binary is built with `CGO_ENABLED=1 -tags libndpi` and links to the target
system's `libndpi` runtime. It is intended as an override for a host that
already installed the normal routerd archive.

## Install

Install the normal routerd release first, then install the native agent binary:

```sh
tar -xzf routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
sudo install -m 0755 bin/routerd-ndpi-agent /usr/local/sbin/routerd-ndpi-agent
sudo systemctl restart routerd-ndpi-agent.service
sudo systemctl restart routerd-dpi-classifier.service
```

The host must provide a `libndpi` runtime package with the same shared-library
ABI as the archive was built against. On Debian/Ubuntu, install the optional
runtime dependencies with:

```sh
sudo apt-get install libndpi-bin
```

Verify that the native backend is active:

```sh
sudo curl --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

The response should include `"libndpiLoaded": true`.

## Upgrade note

The normal routerd archive includes the default static `routerd-ndpi-agent`.
During upgrades, `install.sh` preserves an existing native agent when its
`selftest` reports `"libndpiLoaded": true` and the archive agent does not.

Run the normal installer with `--with-ndpi` on hosts that require native
application-layer classification. The installer fails if the final installed
agent does not report `"libndpiLoaded": true`, so the static fallback cannot
silently satisfy a native nDPI intent.

## Configure

`routerd-dpi-classifier` must be configured with `--engine auto` or
`--engine ndpi-agent` and an `--ndpi-agent-socket` pointing at the agent socket.
`auto` is recommended because it falls back to the built-in classifier if the
native agent is unavailable.

The deprecated `--ndpi-reader` option does not enable native nDPI
classification.
