---
title: nDPI agent native package
---

# nDPI agent native package

routerd's Linux binaries are built with `CGO_ENABLED=0` and are statically
linked. The optional `routerd-ndpi` Debian package is the exception for hosts
that need native nDPI classification. It contains only
`/usr/local/sbin/routerd-ndpi-agent`, built with `CGO_ENABLED=1 -tags libndpi`
and linked to the system `libndpi`. The `routerd` package does not include an
nDPI agent.

## Install

Install it next to the `routerd` package. apt installs the matching `libndpi`
runtime from the package dependencies.

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-linux-amd64.deb
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-linux-amd64.deb.sha256
sha256sum -c routerd-ndpi-linux-amd64.deb.sha256
sudo apt install ./routerd-ndpi-linux-amd64.deb
```

Upgrading the package restarts `routerd-ndpi-agent.service` when it is enabled
or running. Verify that the native backend is active:

```sh
sudo curl --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

The response should include `"libndpiLoaded": true`.

## Configure

`routerd-dpi-classifier` must be configured with `--engine auto` or
`--engine ndpi-agent` and an `--ndpi-agent-socket` pointing at the agent socket.
`auto` is recommended because it falls back to the built-in classifier if the
native agent is unavailable.
