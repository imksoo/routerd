---
title: Reconcile and removal
---

# Reconcile and removal

routerd compares the intent declared in YAML with the host's current state. When they differ, routerd computes a plan, optionally previews it as a dry-run, and then applies it.

## Standard sequence

```bash
routerd validate --config router.yaml
routerd plan     --config router.yaml
routerd apply    --config router.yaml --once --dry-run
routerd apply    --config router.yaml --once
```

For a remote router, confirm that the management connection (SSH, console, hypervisor console) will survive the change before running the non-dry-run `apply`.

## Long-running mode

```bash
routerd serve --config router.yaml
```

In serve mode, routerd reacts to events on the bus and re-evaluates only the resources affected. Inputs include DHCPv6-PD renewals, health-check results, derived events, and configuration changes detected by inotify.

## Removal

routerd only deletes objects whose ownership it can attribute (i.e. that routerd previously created or adopted). It does not remove third-party configuration or manual changes.

Full rollback to a previous configuration is not in scope today. For changes that include deletions, always run `routerd plan` and `routerd apply --dry-run` first and confirm the deletion list before applying.

## See also

- [State and ownership](../concepts/state-and-ownership.md)
- [Apply and render](../concepts/apply-and-render.md)
- [Troubleshooting](../how-to/troubleshooting.md)
