# DS-Lite

AFTR名称解析直接使用aftrDNSServers。localAddressSource若为delegatedAddress，必须指定localDelegatedAddress。完整示例中的AFTR及ISP DNS值须按实际线路调整。

[YAML](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)

[各资源字段与YAML](../reference/resources.md)

使用前修改接口名、地址与对端配置。先在本地验证，再在目标路由器检查计划。--replace只用于完整配置。应用可能影响管理连接，请保留控制台。

```sh
routerd validate --config example-dslite-home.yaml
```
