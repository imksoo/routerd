---
title: 配置示例集
sidebar_position: 0
---

# 配置示例集

本节汇整了一系列便于参考的路由器配置模式。
相较于设计文档，本节更接近设备厂商的配置示例集格式。
每个页面均以构成图开头，说明目前 routerd 可管理的范围，并附上最小化的 YAML 配置。

这里的配置是出发点。投入正式环境之前，请务必依照您的实际环境调整接口名称、地址范围、
ISP 专属值及管理访问路径。

![配置示例阅读流程图：拓扑编号、图示对应表、YAML 摘录、本地编辑、validate-plan-dry-run、apply 与 routerctl 确认](/img/diagrams/config-example-workflow.png)

:::tip 推荐起点
如需用 routerd 替换家庭路由器，请从
[`examples/home-router-mgmt-protected.yaml`](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
开始。该示例为**安全最小的 canonical 配置**：3-role 防火墙（untrust / trust / mgmt）、
DS-Lite 优先 + PPPoE 备援、用于 apply 时锁出保护的 `ManagementAccess`，
以及绑定到管理地址的 `WebConsole`。请将接口与 ISP 替换为您自己的环境，
按下方安全检查清单的顺序应用。
:::

## 阅读方式

每个示例均依照相同的流程说明：

1. **构成图**：物理构成或逻辑构成。
2. **图示对应表**：说明图中各编号所代表的含义。
3. **配置示例**：完整 YAML 置于 `examples/` 目录，页面内以编号摘录要点。
4. **应用步骤**：事先执行的 validate、plan、dry-run。
5. **确认方式**：用于确认收敛状态的命令。

构成图中的 `[1]` 与 YAML 注释中的 `# [1]` 指向同一个对象。
通过对照图示，可以追踪每个资源管理的位置。

## 可立即试用的示例

| 示例 | 状态 | 适用场景 |
| --- | --- | --- |
| [基本 IPv4 NAT 路由器](./basic-ipv4-nat.md) | 目前实现可用 | WAN 使用 DHCPv4，LAN 使用私有 IPv4 与 DHCPv4。 |
| [LAN DHCP 与本地 DNS](./lan-dns-dhcp.md) | 目前实现可用 | 在单一 LAN 上提供 DHCPv4、本地 DNS 区域及 DHCP 派生名称。 |
| [DS-Lite 家用路由器](./dslite-home.md) | 填入 ISP 专属值后可用 | 以 IPv6 为主线路，IPv4 流量通过 DS-Lite 通道。 |
| [PPPoE IPv4 NAT 路由器](./pppoe-ipv4-nat.md) | 填入 ISP 认证信息后可用 | 在 Ethernet WAN 上建立 PPPoE 连接以访问 IPv4 互联网。 |
| [内部 Web 服务器的端口转发](./port-forward-web.md) | 确认 WAN 地址后可用 | 公开一台内部 HTTPS 服务器，并让 LAN 端也能以相同公开名称访问。 |
| [带有 BGP 的 Kubernetes API VIP](./kubernetes-api-vip.md) | 搭配 `routerd-bgp` GoBGP 与 keepalived 可用 | 由 routerd 持有 Kubernetes API VIP、对 control plane 进行健康检查，并通过 BGP 接收 Service 前缀。 |
| [访客 / IoT 端点隔离](./guest-isolation.md) | Linux nftables 可用 | 仅允许部分 MAC 地址访问互联网，禁止其到达 LAN 与管理网络。 |
| [防火墙速率限制与 ICMP 规则](./firewall-rate-limit.md) | Linux nftables 可用 | 开放多个端口、匹配 ICMP type，以及缓解 SSH 暴力破解。 |
| [Multi-WAN IPv4 failover](./multi-wan-failover.md) | 目前实现可用，健康检查需谨慎调整 | 从多个 IPv4 出口中选出正常的默认路由。 |
| [将公共 DNS 重定向至本地解析器](./local-dns-redirect.md) | Linux nftables 可用 | 将 LAN 客户端对外的明文 DNS 查询集中导向路由器的 DNS。 |
| [Tailscale subnet / exit node](./tailscale-subnet-exit.md) | 可使用 Tailscale 的环境可用 | 将 LAN 路由及 exit node 广播至 tailnet。 |
| [WireGuard hub & spoke template](./wireguard-hub-spoke.md) | 替换密钥与 peer 路由的 template | 需要一个路由式 WireGuard hub 的出发点。 |
| [将 telemetry 导出至 OTLP collector](./telemetry-export.md) | 有 collector 即可用 | 将 routerd 的 logs、metrics、traces 发送至可观测性基础设施。 |

## 尚未标示为可直接执行的示例

对于初次接触者而言这些内容很重要，但在对应的生成（render）与操作指引完备之前，
不作为可直接应用的 YAML 提供。

| 模式 | 现况 |
| --- | --- |
| MAP-E / v6plus 类 IPv4 over IPv6 | 尚未作为一级资源实现。 |
| OSPF 等 BGP 以外的动态路由 | 未实现。Kubernetes 风格的 Service 前缀导入可使用 `routerd-bgp` GoBGP。 |
| IPsec site-to-site cookbook | IPsec 基础已备，但正式环境的生成（render）尚未达到同等水准。 |

## 安全检查

在正式使用中的路由器应用之前，请务必确认以下事项：

- 保留可从控制台或 hypervisor 进入的路径。
- 确认管理通信经由哪个接口传输。
- 先执行 `routerctl validate` 和 `routerctl plan`。
- 确认 plan 不会删除管理接口的地址、路由及防火墙开放规则。
- 使用路由器上已安装的 release 二进制文件执行 apply，勿从其他开发目录执行。

```bash
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
routerctl apply -f router.yaml --replace
routerctl status
```

## 相关页面

- [启动第一台路由器](../tutorials/first-router.md)
- [WAN 侧服务](../tutorials/wan-side-services.md)
- [LAN 侧服务](../tutorials/lan-side-services.md)
- [基本 NAT 与 firewall policy](../tutorials/basic-firewall.md)
