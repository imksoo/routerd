---
title: Apply, prune, and delete
---

# Apply, prune, and delete

routerd uses a kubectl-style apply model.

By default, `routerd apply` is additive:

```bash
sudo routerd apply --config wan.yaml --once
sudo routerd apply --config lan-services.yaml --once
```

The second command updates resources in `lan-services.yaml` and leaves the
resources from `wan.yaml` in place. This is the normal workflow when a router
configuration is split across several files or generated in stages.

Use `--prune` only when the file is intended to be the complete desired set:

```bash
sudo routerd apply --config full-router.yaml --once --prune
```

With `--prune`, routerd may remove routerd-owned artifacts that are no longer
present in the applied file. Always dry-run first on a remote router:

```bash
sudo routerd apply --config full-router.yaml --once --dry-run --prune
routerctl describe orphans
```

For deliberate removal, prefer an explicit delete:

```bash
sudo routerd delete IPv6PrefixDelegation/wan-pd
routerctl delete IPv6PrefixDelegation/wan-pd
```

`routerd delete -f old-resource.yaml` deletes every resource listed in that
Router YAML. `routerctl delete` asks the running daemon to delete one resource
through the local control socket.
