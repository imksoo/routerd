# DS-Lite

AFTR discovery uses aftrDNSServers directly. If localAddressSource is delegatedAddress, configure localDelegatedAddress. Adapt the AFTR and ISP DNS values in the full example.

[YAML](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)

[Resource fields and YAML](../reference/resources.md)

Change the example interface names, addresses and peer settings before using it. Validate locally, then inspect the plan on the target router. Use --replace only for a complete router configuration. Applying can change management access; keep a console.

```sh
routerd validate --config example-dslite-home.yaml
```
