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

![配置示例阅读流程图：拓扑编号、图示对应表、YAML 摘录、本地编辑、独立 validate、隔离 dry-run、live apply 与 routerctl 确认](/img/diagrams/config-example-workflow.png)

:::tip 第一次请从隔离 IPv4 NAT 实验开始
第一次使用 routerd 时，请从[基本 IPv4 NAT 路由器](./basic-ipv4-nat.md)开始，并使用带有
控制台的隔离 Ubuntu Server VM。不要把
[`examples/home-router-mgmt-protected.yaml`](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
当成所有家庭都可直接应用的安全默认值：它假定三张网卡、NTT profile、Transix 的
AFTR/DNS 和 PPPoE 认证信息。理解这个小实验后，再根据自己的线路和管理路径逐项设计进阶配置。
:::

## 阅读方式

每个示例均依照相同的流程说明：

1. **构成图**：物理构成或逻辑构成。
2. **图示对应表**：说明图中各编号所代表的含义。
3. **配置示例**：完整 YAML 置于 `examples/` 目录，页面内以编号摘录要点。
4. **检查步骤**：先以独立的 `routerd validate` 和隔离的 dry-run 检查 YAML。
5. **确认方式**：服务启动后用于确认收敛状态的命令。

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
| [访客 / IoT 端点隔离](./guest-isolation.md) | 路由器 policy 示例；不能替代 VLAN、SSID、交换机或 AP 隔离 | 用路由器上的 MAC 条件规则限制部分端点访问外网和 LAN。 |
| [防火墙速率限制与 ICMP 规则](./firewall-rate-limit.md) | firewall 功能基础；不可作为唯一安全边界 | 开放多个端口、匹配 ICMP type，以及缓解 SSH 暴力破解。 |
| [多 WAN IPv4 故障转移](./multi-wan-failover.md) | 目前实现可用，健康检查需谨慎调整 | 从多个 IPv4 出口中选出正常的默认路由。 |
| [将公共 DNS 重定向至本地解析器](./local-dns-redirect.md) | Linux nftables 可用 | 将 LAN 客户端对外的明文 DNS 查询集中导向路由器的 DNS。 |
| [Tailscale 子网 / exit node](./tailscale-subnet-exit.md) | 可使用 Tailscale 的环境可用 | 将 LAN 路由及 exit node 广播至 tailnet。 |
| [WireGuard 中心—分支模板](./wireguard-hub-spoke.md) | 替换密钥与 peer 路由的模板 | 需要一个路由式 WireGuard hub 的出发点。 |
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
- 先执行独立的 `routerd validate` 和隔离的 dry-run。
- 确认 dry-run 结果中的管理接口地址、路由和预期生成的主机文件均符合预期。
- 使用路由器上已安装的 release 二进制文件执行 apply，勿从其他开发目录执行。

下方命令假定你是可运行 `sudo` 的本地用户。安装程序会创建 `routerd` 组；若管理员选择通过该组授予本地 socket 访问，加入后必须重新登录才会生效。首次使用时，请对 `routerctl`、`nft` 和主机 `ip` 检查保留 `sudo`。

```bash
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config router.yaml
sudo routerd apply --config router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

`routerctl validate`、`routerctl plan` 和 `routerctl apply` 都会向运行中的 daemon 提交请求，不能替代上述首次检查。只有启动 `routerd.service` 后，才可运行 `sudo routerctl get status`；live apply 前仍须确认有控制台或独立管理路径。

## 相关页面

- [启动第一台路由器](../tutorials/first-router.md)
- [WAN 侧服务](../tutorials/wan-side-services.md)
- [LAN 侧服务](../tutorials/lan-side-services.md)
- [基本 NAT 与 firewall policy](../tutorials/basic-firewall.md)
