# 有状态防火墙

routerd 管理 nftables 的 `inet routerd_filter` 表。
防火墙以 4 种资源表达。

- `FirewallZone` 将接口分配至区域。
- `FirewallPolicy` 表示拒绝日志等全局配置。
- `FirewallRule` 表示无法以角色组合表达的例外。
- `ClientPolicy` 依 MAC 地址分类 LAN 客户端。

角色有 `untrust`、`trust`、`mgmt` 三种。`wan`、`lan`、
`management` 等是区域名称，需与角色区分看待。

默认行为由以下表格决定：

| 来源 | self | mgmt | trust | untrust |
| --- | --- | --- | --- | --- |
| `mgmt` | 允许 | 允许 | 允许 | 允许 |
| `trust` | 允许 | 拒绝 | 允许 | 允许 |
| `untrust` | 拒绝 | 拒绝 | 拒绝 | 拒绝 |

所生成的规则始终允许已建立的通信、关联通信、loopback，以及必要的 ICMPv6。
DHCPv6 PD、DS-Lite、dnsmasq 的 DHCP、`routerd-dns-resolver` 等所需的开口也由 routerd 在内部生成。

NAT44 使用独立的 `ip routerd_nat` 表。

## Rule expression

`FirewallRule` 支持依 CIDR 匹配来源 / 目的地、可复用的 `IPAddressSet` 目的地引用、TCP/UDP 的 `sourcePorts` / `destinationPorts`、ICMP / ICMPv6 的 type 名称、nftables 的 `rateLimit`，以及以来源为单位的 `connLimit`。
`rateLimit` 与 `connLimit` 在超过阈值的通信时相符，因此基本上与 `drop` / `reject` 规则组合使用，以缓解扫描与暴力破解攻击。

## 访客设备隔离

`ClientPolicy` 是依 MAC 地址分类同一 LAN 网段内设备的访客模式。
适用于可信任设备与访客设备从同一 DHCP 服务器获取地址的环境。
访客设备仅能使用路由器的最小限度服务，无法访问私有网络。

策略有两种：

| mode | 行为 |
| --- | --- |
| `include` | 仅将列表中的 MAC 地址视为访客，其余为 trusted。 |
| `exclude` | 仅将列表中的 MAC 地址视为 trusted，对象接口上的其余设备视为访客。 |

`ClientPolicy.spec.macs` 是常用场景的简写形式。省略 `interfaces` 时，对象为所有属于 `trust` `FirewallZone` 的接口。`spec.isolation` 可以易读的方式表达互联网、LAN、management、本地探索的 allow / deny。

访客设备默认可使用 DNS、DHCP、NTP。
目的地为 `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`、`fc00::/7` 的通信会被拒绝。
面向全球互联网的通信，依一般的区域矩阵与路由策略处理。

`ClientPolicy` 以 Linux nftables 的 Ethernet 来源地址集合实现。
FreeBSD pf 在 routed filter path 上不具备相同的 MAC 匹配模型，routerd 明确将此资源视为不支持。

## 日志

`FirewallPolicy.spec.logDeny` 为 true，且 `FirewallEventLog` 资源有效时，
所生成的 nftables 规则会将被拒绝的数据包送至配置好的 NFLOG group。
在 Linux 上，`routerd-firewall-logger` 直接从 nfnetlink 读取该 group，
并存储至 `firewall-logs.db`，不另外启动数据包捕获程序。
可完整存储 NFLOG 的 prefix、接口、数据包 family、协议、
地址及端口。

Web 管理界面的 Firewall 标签页与 `routerctl firewall-logs` 会读取此数据库。
请以受管理的 `generated service artifacts` 方式启用 logger。
例如使用 `routerd-firewall-logger daemon --path /var/lib/routerd/firewall-logs.db --nflog-group 1`。

若要将已接受的流量用于 DPI 观测，可通过 `FirewallEventLog.spec.log.copyRange`
配置 NFLOG 从每个数据包复制的数据量上限。
配置为 `1536` 或 `2048` 字节左右，可在看到 TLS/HTTP/DNS 分类所需的数据包开头的同时，
避免持续将整个数据包复制至用户空间。
