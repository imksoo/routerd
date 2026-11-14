# Dynamic configuration

The startup YAML remains the operator-managed configuration. Validated runtime parts can add resources temporarily; expired parts stop contributing at the next merge. Use resource status to distinguish declared configuration from acquired DHCP/PD/BGP state.

DynamicConfigSource is currently a validated declaration without a runtime consumer. Declaring one does not schedule Plugin execution, and its ttl does not set PluginResult.status.ttl. An EventSubscription can invoke a local Plugin; manual invocation uses routerctl plugin run.

A Plugin returns a TTL in its result. Only policy-authorized exact masks suppress matching startup resources while active; the YAML itself is unchanged. Provider action proposals use the separate ProviderActionPolicy gates and are not applied just by merging a runtime part.

Inspect the effective configuration before applying changes:

```sh
routerctl dynamic list
routerctl dynamic describe <source-or-part>
routerctl dynamic render
routerctl dynamic diff
routerctl plugin list
```

Read the resource-specific field meanings and limitations:

- [Plugin](resources/Plugin.md)
- [DynamicConfigSource](resources/DynamicConfigSource.md)
- [DynamicOverridePolicy](resources/DynamicOverridePolicy.md)
- [EventSubscription](resources/EventSubscription.md)
- [ProviderActionPolicy](resources/ProviderActionPolicy.md)
