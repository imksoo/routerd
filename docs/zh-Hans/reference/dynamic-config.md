# 动态配置

启动 YAML 仍由用户管理。经验证的运行时数据可临时加入资源，过期后于下次合并排除。请通过资源状态区分声明配置与 DHCP/PD/BGP 获取的状态。

目前 DynamicConfigSource 仅验证声明，没有运行时使用流程。声明它不会调度 Plugin，也不会以 ttl 设置 PluginResult.status.ttl。EventSubscription 可调用本机 Plugin；手动调用使用 routerctl plugin run。

Plugin 在结果中返回 TTL。仅策略允许的完全匹配 mask 会在有效期间抑制启动资源，不改写 YAML。提供商操作提案受独立 ProviderActionPolicy 控制，不会只因合并运行时数据而应用。

应用变更前，先检查有效配置：

```sh
routerctl dynamic list
routerctl dynamic describe <source-or-part>
routerctl dynamic render
routerctl dynamic diff
routerctl plugin list
```

各字段的含义与实现限制请见资源说明：

- [Plugin](resources/Plugin.md)
- [DynamicConfigSource](resources/DynamicConfigSource.md)
- [DynamicOverridePolicy](resources/DynamicOverridePolicy.md)
- [EventSubscription](resources/EventSubscription.md)
- [ProviderActionPolicy](resources/ProviderActionPolicy.md)
