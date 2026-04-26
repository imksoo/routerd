---
title: Getting Started
---

# Getting Started

This tutorial walks through the smallest useful routerd workflow: install the
binary, validate a YAML config, inspect the dry-run plan, and run one reconcile.

routerd is still v1alpha1 software. Start in a lab VM or a host with console
access before applying it to a remote router.

## 1. Build routerd

```bash
make build
```

The build creates:

- `bin/routerd`
- `bin/routerctl`

## 2. Start From A Small Config

Use the basic static example first:

```bash
routerd validate --config examples/basic-static.yaml
routerd reconcile --config examples/basic-static.yaml --once --dry-run
```

The dry-run output is JSON status. It tells you which resources are healthy,
which are drifted, and what routerd would do.

## 3. Install The Source Layout

routerd defaults to a `/usr/local` layout:

```bash
sudo make install
sudo install -m 0644 examples/basic-static.yaml /usr/local/etc/routerd/router.yaml
```

Important default paths:

- Config: `/usr/local/etc/routerd/router.yaml`
- Binary: `/usr/local/sbin/routerd`
- Plugins: `/usr/local/libexec/routerd/plugins`
- Runtime: `/run/routerd`
- State: `/var/lib/routerd`

## 4. Reconcile Once

Always run one-shot mode before enabling the daemon:

```bash
sudo /usr/local/sbin/routerd reconcile \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

Remove `--dry-run` only after the plan is expected.

## 5. Enable The Daemon

Install the systemd unit after the one-shot run looks correct:

```bash
sudo make install-systemd
sudo systemctl daemon-reload
sudo systemctl enable --now routerd.service
```

`routerd serve` keeps a local control API socket under `/run/routerd/` and runs
scheduled reconciliation.

## Next Steps

- Read the [resource API reference](/docs/reference/api-v1alpha1).
- Try the [router lab tutorial](/docs/tutorials/router-lab).
- Review the [control API](/docs/reference/control-api-v1alpha1) for status and
  operational tooling.
