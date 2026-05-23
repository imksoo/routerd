---
title: 具备健康检查的多 WAN 切换
---

# 具备健康检查的多 WAN 切换

## 适用场景

路由器有多条对外路径，希望 routerd 能够：

- 从可用路径中自动选择最佳路径。
- 主线路异常时自动切换至备援线路。
- 切换时不中断现有连接，平滑完成转移。

常见场景如下：

- 家庭路由器以 DS-Lite 隧道为主线路，以上游 HGW NAT 为备援。
- SOHO 路由器以两条 ISP 线路（光纤 + LTE 等）进行冗余。
- 分支办公室路由器优先使用企业 VPN 线路，仅在无法连接时切换至公共互联网。

## routerd 的解决方式

使用 `EgressRoutePolicy` 声明候选路径与选择方式。
routerd 会持续选取「ready（上游资源已稳定）且 healthy（`HealthCheck` 通过）」的候选中，
weight 最高的路径。切换时更新 OS 路由表，并重新应用跟踪策略的 `NAT44Rule`，
但刻意不清除 conntrack。现有连接继续维持，只有新连接使用新路径。

这样的设计让 weight 较低的备援路径从启动之初即可使用，之后等主线路确认可用后再平滑切回。

测试用 PPPoE 等会消耗 session 额度的备援路径，可保留在 YAML 中同时设为停用。
在 `PPPoESession`、对应的 `HealthCheck` 及 `EgressRoutePolicy` 的候选中指定 `disabled: true`，
正常应用时即会停止并停用已生成的服务，需要时可手动进行测试。

## 最小配置

配置由三个组件组成：各候选路径的 `HealthCheck`、统整这些路径的 `EgressRoutePolicy`，
以及跟踪策略的 `NAT44Rule`。

### 健康检查

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: internet-via-primary
spec:
  target: 1.1.1.1
  protocol: tcp
  port: 443
  interval: 30s
  timeout: 3s
```

每个检查从对应的 `EgressRoutePolicy` 候选中参照。routerd 从该参照推导探针的来源绑定与 socket mark，
因此配置中无需写入主机特定的设定。
ICMP 容易被中途的过滤器丢弃，建议对稳定目的地（如 1.1.1.1）使用 TCP/443 较为可靠。

### Egress 策略

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: EgressRoutePolicy
metadata:
  name: ipv4-default
spec:
  family: ipv4
  destinationCIDRs:
    - 0.0.0.0/0
  selection: highest-weight-ready
  hysteresis: 30s
  candidates:
    - name: ds-lite-primary
      source: DSLiteTunnel/ds-lite-primary
      deviceFrom:
        resource: DSLiteTunnel/ds-lite-primary
        field: interface
      gatewaySource: none
      weight: 90
      healthCheck: internet-via-primary

    - name: hgw-fallback
      source: Interface/wan
      deviceFrom:
        resource: Interface/wan
        field: ifname
      gatewaySource: static
      gateway: 192.0.2.1
      weight: 50
      healthCheck: internet-via-hgw
```

`hysteresis` 用于防止频繁切换（chatter）。候选路径变为 unhealthy 后，在此时间内不会降级。

### 跟踪策略的 NAT

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-egress
spec:
  type: masquerade
  egressPolicyRef: ipv4-default
  sourceRanges:
    - 192.0.2.0/24
```

masquerade 的源地址取自 routerd 当时所选候选路径的接口地址。
切换后，下一个数据包即以新路径的地址进行 NAT。

## 不对 RFC1918 目的地执行 NAT

当上游网关拥有返回 LAN 的路由时，可对公共互联网执行 NAT，同时避免对其他内部网络目的地执行 NAT。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-wan-hgw
spec:
  type: masquerade
  egressInterface: wan
  sourceRanges:
    - 192.0.2.0/24
  excludeDestinationCIDRs:
    - 192.168.0.0/16
    - 172.16.0.0/12
    - 10.0.0.0/8

---

apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: hgw-lan
spec:
  destination: 192.168.0.0/16
  device: wan
```

这样可让 RFC1918 目的地通过路由分配，公共互联网流量则经由所选的 egress 路径。

## 操作建议

- 请务必确保带外管理路径（mgmt 接口、控制台、专用 SSH NIC 等）。通过 WAN 侧 SSH 修改路由或防火墙设置有一定风险。
- 一个 `HealthCheck` 建议只由一个候选路径参照。这样 routerd 能推导出单一探针路径，「探针失败 = 该路径故障」的解读更清晰。
- 切换时请勿清除 conntrack。routerd 刻意不清除，让已完成握手的 TCP 连接自然结束。
- 当前选用的候选路径可通过 `routerctl describe EgressRoutePolicy/<name>` 的 `status.selectedCandidate` 确认。

## 相关项目

- [Path MTU 与 MSS clamping](../concepts/path-mtu.md)
- [防火墙规则基础](./firewall-rule.md)
- [DS-Lite 设定](./flets-ipv6-setup.md)
