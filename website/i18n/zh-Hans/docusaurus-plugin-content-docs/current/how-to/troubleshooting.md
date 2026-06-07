---
title: 故障排查
slug: /how-to/troubleshooting
---

# 故障排查

![从 routerctl status 和 dry-run intent 到 OS state、daemon socket、event、DHCP/DNS/conntrack check 的 routerd troubleshooting order](/img/diagrams/how-to-troubleshooting.png)

排查 routerd 问题时，请先区分 **routerd 的意图** 与 **主机的实际状态**。
确认 routerd 意图达成什么之后，再与 OS 的实际状态进行比对。

## 基本排查顺序

1. `routerctl status` — 总览全局
2. `routerctl describe <kind>/<name>` — 深入查看目标资源
3. `routerctl apply --dry-run` — 确认下次应用将会发生什么变更
4. OS 命令（`ip`、`nft`、`ss`、`journalctl`）— 确认实际状态
5. 对应守护进程的 `/v1/status` 与事件日志

## DHCPv6-PD

```bash
curl --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://unix/v1/status
tail -n 20 /var/lib/routerd/dhcpv6-client/wan-pd/events.jsonl
```

确认以下项目：

- `phase` 是否为 `Bound`
- `currentPrefix` 是否已填入
- `renewAt` 是否为未来时刻
- 事件日志中是否记录了 `Reply` 或 `Renew`

若非 `Bound` 状态，LAN 侧的 IPv6 RA、AAAA 记录、DHCPv6 应停止运作。
不继续下发旧有前缀，是 routerd 在安全层面的承诺。

## DHCPv4

```bash
curl --unix-socket /run/routerd/dhcpv4-client/wan.sock http://unix/v1/status
```

确认 `DHCPv4Client` 是否处于 `Bound` 状态。
若需要立即更新，可通过 `POST /v1/commands/renew` 发出请求。

## dnsmasq

当前的 routerd 中，dnsmasq 专责 DHCPv4、DHCPv6、DHCP 中继及 Router Advertisement。
DNS 响应与转发由 `routerd-dns-resolver` 负责。

请确认生成的 dnsmasq 配置是否符合以下条件：

- 包含预期的 `dhcp-range`
- 配置为 `port=0`（禁用 DNS 功能；DNS 是 `routerd-dns-resolver` 的职责）
- 包含 `dhcp-script=/usr/local/libexec/routerd/dhcp-event-relay`（将租约变更通知 routerd 的路径）
- 按需加入 `enable-ra`

## DNS resolver

```bash
sudo curl --unix-socket /run/routerd/dns-resolver/<resource>.sock http://unix/v1/healthz
dig @<lan-ip> router.lan.example.org A
dig @<lan-ip> example.com A
```

依次确认以下项目：

- 监听地址与端口是否符合预期（`ss -lnup`）
- 本地权威区域是否正常响应（`DNSZone` 的手动记录与 DHCP 生成的记录）
- 条件式转发是否送达指定的上游（`dig @<lan-ip> <forwarded-domain>`）
- 默认上游是以 DoH / DoT / TCP / 明文 UDP 哪种方式响应（查看解析器 status 及上游 health）

## DS-Lite

```bash
ip -6 tunnel show
ip route show default
nft list table ip routerd_nat
```

若 AFTR 的 FQDN 无法解析，请确认 `DNSResolver` 的 `forward` source 配置。
特定接入网络的 AFTR 记录，通常无法通过公开 DNS 解析。

## conntrack

依环境不同，`/proc/net/nf_conntrack` 可能不存在。
此时 routerd 会退回使用 sysctl 来源的汇总统计。
即使详细流量清单为空，也不一定代表 NAT 已损坏。请查看 `routerctl connections` 的摘要。

## 排查时应避免的事项

- 请勿在生产环境的 WAN 上，同时运行旧有的 DHCP 客户端或手动测试用守护进程与 routerd 并行。从同一接口同时发出多个 DHCPv6-PD 客户端，可能会破坏上游的租约状态。
- 更换路由时，请勿 flush `nf_conntrack`。routerd 刻意不进行 flush，强制 flush 会中断已建立的连接会话。
- 请勿在同一主机上编辑 `/usr/local/etc/routerd/router.yaml` 的同时，在其他位置放置临时的 YAML 覆盖文件。每台主机保持单一配置文件，可维持调和（reconcile）的可预测性。

## 相关参考

- [状态与拥有权](../concepts/state-and-ownership.md)
- [Reconcile loop](../operations/reconcile)
