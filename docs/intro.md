# routerd documentation

routerd applies the network configuration declared in YAML. Keep the complete configuration in Git; use named resources and references rather than duplicating addresses. Acquired DHCP/PD/BGP state is not static configuration.

- [Install and upgrade](install-and-upgrade.md)
- [Configuration philosophy](concepts/design-philosophy.md)
- [Resource fields and YAML](reference/resources.md)
- [Configuration examples](config-examples/index.md)
- [Operations](operations/reconcile.md)

Change the example interface names, addresses and peer settings before using it. Validate locally, then inspect the plan on the target router. Use --replace only for a complete router configuration. Applying can change management access; keep a console.
