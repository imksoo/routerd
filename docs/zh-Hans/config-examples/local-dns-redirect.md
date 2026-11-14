# 转发至本机服务

LocalServiceRedirect将匹配的客户端传入流量转发到本机端口，不会启动监听服务。请另行配置DNSResolver等服务；destinationSetRef引用IPAddressSet。这不是通用HTTP/HTTPS代理。

[YAML](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)

[各资源字段与YAML](../reference/resources.md)

使用前修改接口名、地址与对端配置。先在本地验证，再在目标路由器检查计划。--replace只用于完整配置。应用可能影响管理连接，请保留控制台。

```sh
routerd validate --config example-local-dns-redirect.yaml
```
