# ADR 0013: 受信任 overlay 路径上的 IPv4 强制分片

![ADR 0013 的示意图。从常规 MTU 推导和 MSS clamping，到非 TCP DF 黑洞风险，以及明确的 trusted-overlay routerd_forcefrag 处理](/img/diagrams/adr-0013-ipv4-force-fragmentation.png)

## 状态

已批准为预发布实现。

## 背景

routerd 已从隧道和转发意图推导路径 MTU 处理。
常规缓解措施是 TCP MSS clamping：Linux 上 `routerd_mss`
为推导出的低 MTU 转发路径的 TCP SYN MSS 进行重写，
无需防火墙 zone。

MSS clamping 对非 TCP 流量无效。设置了 DF 位的、超大的
UDP、QUIC、ICMP 及其他 IPv4 数据包，当受信任 overlay 或 underlay 的
有效 MTU 较低时，如果 PMTUD 反馈被阻止或忽略，
可能会形成黑洞。

清除 DF 不是通用互联网的默认行为。它违反发送端的明确路径 MTU 偏好，
生成转发成本高的分片且更易被丢弃。
因此此功能必须是明确的、路径范围的、默认关闭的。

## 决策

在 overlay 路径 MTU 意图中添加明确的 IPv4 强制分片选项：

- `OverlayPeer.spec.pathMTU.forceFragmentIPv4`
- `TunnelInterface.spec.pathMTU.forceFragmentIPv4`

此功能仅在 routerd 可以推导转发路径和有效 MTU 的受信任 routerd
overlay 设备上支持：`wireguard`、`ipip`、`gre`、`fou`、`gue`。
启用强制分片时，验证将拒绝 `route`、`tailscale`、`ipsec` 及其他
underlay 类型。

在 Linux 上，routerd 渲染专用的 nftables 表：

```text
table ip routerd_forcefrag {
  chain forward {
    type filter hook forward priority mangle; policy accept;
    iifname <capture> oifname <tunnel> ip length > <path-mtu> ip frag-off 0x4000 ip frag-off set 0
  }
}
```

匹配仅限 IPv4，范围限定在推导的转发路径。仅对当前未分片的
超大 DF 数据包清除 DF。之后内核在 egress 设备按常规接口
MTU 进行分片。

TCP MSS clamping 仍是 TCP 的主要缓解措施。强制分片是
明确受信任路径上非 TCP 或不当大小流量的兜底措施。

## 替代方案

- **Route MTU lock。** 对 routerd 拥有的路由更标准，但
  无法干净地覆盖包含 BGP 导入移动性路径在内的所有路由来源。
  策略分散在路由写入者之间。
- **iptables。** 现有的库存目标在 DF 清除的跨路径表达上
  不比 nftables 更干净。
- **首阶段即支持 FreeBSD pf。** pf 有 `scrub ... no-df`，但
  routerd 的 SAM/overlay 实际数据平面以 Linux 为主。
  FreeBSD 支持留在后续阶段而非假装已对等。

## 结论

- 默认行为不变。
- Linux 在 `routerd_mss` 旁获得第二个路由器拥有的路径 MTU nftables 表
  `routerd_forcefrag`。
- 操作员需要按 overlay 路径或隧道接口逐个选择启用。
- 分片可能降低吞吐量并增加丢包敏感性。
  文档应将其描述为受信任 overlay PMTU 黑洞的最后手段。
