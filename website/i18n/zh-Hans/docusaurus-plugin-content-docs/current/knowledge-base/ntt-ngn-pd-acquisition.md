---
title: NTT NGN 系接入网络的 DHCPv6-PD 与 AFTR
---

# NTT NGN 系接入网络的 DHCPv6-PD 与 AFTR

![Diagram showing DHCPv6-PD and AFTR acquisition on NTT NGN-style access from prefix delegation and information request through carrier DNS AFTR resolution to DS-Lite tunnel, IPv4 route, NAT44, and LAN connectivity checks](/img/diagrams/knowledge-base-ntt-ngn-pd-acquisition.png)

本文是在 NTT NGN（日本 IPv6 光纤线路）等 IPv6 接入网络的 HGW 下使用 routerd 的实地笔记。
同样结合 DHCPv6-PD 与网络内 AFTR 进行 DS-Lite 的其他运营商，也可套用相同模式。

## DHCPv6-PD

`routerd-dhcpv6-client` 在这些 HGW 下可稳定获取 DHCPv6-PD。
无需过多重传或特殊获取程序，标准的 solicit / advertise / request / renew 即已足够。

稳定运作时，可观察到以下行为：

- 同一 HGW 下的多台路由器各自获取互不重叠的前缀。
- 依 T1 / T2 时序，Renew 持续成功。
- 重新启动守护进程后，可从 `lease.json` 还原租约。

## DHCPv6 information-request 可能不返回 AFTR

部分 HGW / ONU 配置下，DHCPv6 的 information-request 会返回 DNS、SNTP、domain-search，
但不返回 AFTR 选项。AFTR 为空本身是正常现象。

此情况下，DS-Lite 需明确指定以下其中一项：

- `DSLiteTunnel.spec.aftrIPv6` — 直接固定 AFTR 的 IPv6 地址。
- `DSLiteTunnel.spec.aftrFQDN` — 解析 FQDN。

## AFTR 的 FQDN 通常需要条件式 DNS 转发

运营商管理的 AFTR FQDN（例如：`gw.transix.jp`），往往只能通过运营商内部 DNS 解析，
公开解析器会返回 NXDOMAIN。

在 routerd 中，通过 `DNSResolver` 的 `forward` source 来表达：

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: resolver
  spec:
    listen:
      - name: local
        addresses: [127.0.0.1]
        port: 53
    sources:
      - name: aftr
        kind: forward
        match: [transix.jp]
        upstreams:
          - udp://[2404:8e00::feed:101]:53
```

DS-Lite 控制器通过 `routerd-dns-resolver` 解析 AFTR 的 FQDN，不经过系统的 stub 解析器。

## DS-Lite 端到端确认清单

DS-Lite 正常运作时，应可观察到以下状态：

- 条件式转发能解析 AFTR 的 FQDN。
- `ip6tnl` 隧道设备存在。
- IPv4 默认路由指向隧道。
- nftables 的 NAT44 已为 LAN 往外的 IPv4 流量配置完毕。
- LAN 客户端能成功连接至外部 IPv4（HTTP / ICMP）。

## 本笔记的定位

以上内容为在 routerd 评估环境中，使用运营商出厂 HGW 所得到的观测结果。
可作为类似部署的参考指引，但并非对国内所有 ISP 方案或 HGW 固件版本的保证。
请将其作为自行验证的起点。
