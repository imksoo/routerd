# OCI inventory observe-only plugin example

This example is a static CloudEdge inventory plugin. It reads the routerd
`PluginRequest` JSON from stdin, ignores the request body, and writes one
`PluginResult` JSON object to stdout.

The result contains one `CloudAddressClaim` candidate and one OCI-style
`actionPlan`. The action plan is display-only in the CloudEdge MVP; routerd
does not execute it and this script does not call any OCI API or `oci` CLI.

Run it through the startup config example:

```sh
routerctl plugin run oci-inventory --dry-run --config examples/cloud-inventory-plugin.yaml
```

In dry-run mode, `routerctl` prints the candidate `DynamicConfigPart` and the
display-only action plan without writing the state database.

The `observedAt` timestamp is fixed at `2026-05-29T12:00:00Z` so tests and
documentation have stable output.
