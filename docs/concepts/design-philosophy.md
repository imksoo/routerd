# Configuration philosophy

routerd applies the network configuration declared in YAML. Keep the complete configuration in Git; use named resources and references rather than duplicating addresses. Acquired DHCP/PD/BGP state is not static configuration.

Change the example interface names, addresses and peer settings before using it. Validate locally, then inspect the plan on the target router. Use --replace only for a complete router configuration. Applying can change management access; keep a console.

For Live ISO, the working root and state database are volatile. Keep YAML and secrets on the configured persistent medium; do not swap a running VM’s backing ISO. Shut down, confirm it has stopped, then replace the medium and start it.
