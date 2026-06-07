---
title: 插件协议
slug: /reference/plugin-protocol
---

# 插件协议

routerd 的插件是受信任的本机可执行文件。
这是一种机制，可在同一主机上以小型程序的形式添加不内置于核心的资源专用处理逻辑。

目前不支持从远端注册插件、远端安装或公开注册表。

## 部署位置

标准部署路径如下：

```text
/usr/local/libexec/routerd/plugins/<name>/
```

每个插件包含一个 manifest 与可执行文件：

```text
plugin.yaml
bin/<plugin>
```

## 职责

插件可负责下列类型的处理：

- 资源验证
- 变更计划创建
- 主机状态观测
- 应用至主机

不过，会修改网络状态的处理应拆分为易于测试的小单元。
与核心相同，修改主机网络的测试应在 `tests/netns` 等隔离环境中进行。

## 当前定位

routerd 的主要路由器功能正持续通过核心资源与专用守护进程实现。
插件是用于安全集成各用户本地扩展功能的基础架构。
在正式固定为公开兼容 API 之前，manifest 与输入输出格式可能会有所变更。

## CloudEdge MVP

CloudEdge MVP 的插件仅限受信任的本机可执行文件。routerd 不会从远端注册表获取插件，
也不会远端安装插件。

插件输出在写入 dynamic-config 或用于构建 effective-config 之前总会被验证。插件可提出
resource、directive、provider action plan 与 event。`actionPlans` 在 dynamic-config
内部是 inert 的；plugin runner 与 merge path 不会执行它们。operator 可将其导入
provider-action journal，只有在 `ProviderActionPolicy`、approval、allowlist 与
dry-run/live mode gate 通过后，才会交给 executor plugin。

![dynamic config 图：trusted local plugin observation 进入 DynamicConfigPart，inert provider action plan 通过 gated action journal 与 executor plugin path 处理](/img/diagrams/dynamic-config-provider-actions.png)

可用 capability 包括 `observe.cloud`、`observe.providerPrivateIPs`、
`propose.dynamicConfig`、`propose.providerAction`、`execute.providerAction`。
executor plugin 不会从 routerd core 接收 cloud credential；它在自己的进程中使用
cloud-native identity 或自身环境认证。

常用 CLI：

```text
routerctl plugin list [--config <startup>] [-o table|json|yaml]
routerctl plugin run <name> [--dry-run] [--config <startup>] [--state-file <db>] [-o table|json|yaml]
routerctl action import|list|show|approve|execute|journal|rollback ...
```
