# Apply and recovery

Configuration is the source of settings. Reconciliation repairs missing or changed configured objects and removes extra objects not needed by configuration or valid dynamic state. A manually created object is not preserved merely because routerd did not create it. OS foundations and required live DHCP/PD/BGP/SAM state are not static configuration.

Use a complete file with --replace. Without --replace, applying a resource subset updates that subset rather than deleting everything omitted. Inspect the deletion plan and keep an independent console.

```sh
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
routerctl apply -f router.yaml --replace
routerctl get status
routerctl get events --limit 20
```

The long-running daemon performs runtime reconciliation. Validate/plan alone do not prove traffic works. After applying, inspect status and events, and check actual peer connectivity.

```sh
routerd serve --config router.yaml
```

Rollback reapplies stored configuration, not historical kernel state, active connections or daemon leases.

```sh
routerctl rollback --list
routerctl rollback --to <generation>
```

Import is an explicit action for selected physical interfaces and static addresses. Integrate the result into the complete configuration before using --replace; do not import acquired dynamic state as static YAML.

```sh
routerctl import --interface ens18 --address 192.0.2.1/24 --config router.yaml > candidate.yaml
```

[DB](state-database.md) · [YAML](../reference/resources.md)
