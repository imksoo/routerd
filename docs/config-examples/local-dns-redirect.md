# Local service redirect

LocalServiceRedirect redirects matching incoming client traffic to a local port. It does not start a listener. Configure DNSResolver or the destination service separately; destinationSetRef refers to IPAddressSet. This is not a general HTTP/HTTPS proxy.

[YAML](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)

[Resource fields and YAML](../reference/resources.md)

Change the example interface names, addresses and peer settings before using it. Validate locally, then inspect the plan on the target router. Use --replace only for a complete router configuration. Applying can change management access; keep a console.

```sh
routerd validate --config example-local-dns-redirect.yaml
```
