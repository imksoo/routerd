---
title: Apply and delete
---

# Apply and delete

routerd uses a kubectl-style apply model.

By default, `routerd apply` is additive:

```bash
sudo routerd apply --config wan.yaml --once
sudo routerd apply --config lan-services.yaml --once
```

The second command updates resources in `lan-services.yaml` and leaves the
resources from `wan.yaml` in place. This is the normal workflow when a router
configuration is split across several files or generated in stages.

Apply never deletes resources that are omitted from the submitted file. Check
possible leftovers before deleting anything from a remote router:

```bash
routerctl describe orphans
```

For deliberate removal, use an explicit delete:

```bash
sudo routerd delete IPv6PrefixDelegation/wan-pd
routerctl delete IPv6PrefixDelegation/wan-pd
```

`routerd delete -f old-resource.yaml` deletes every resource listed in that
Router YAML. `routerctl delete` asks the running daemon to delete one resource
through the local control socket.
