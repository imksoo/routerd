# DNS 解析器

routerd 的 DNS 将权威数据、解析器程序、转发规则与上游端点，分别以小型资源表达。

`DNSZone` 保存本地的权威数据，包含手动录入的记录，以及从 DHCP 租约衍生的记录。

`DNSResolver` 管理守护进程实例，定义监听地址、缓存、metrics 及查询日志。一个 `DNSResolver` 资源启动一个 `routerd-dns-resolver` 进程。

`DNSForwarder` 是隶属于解析器的一条 match 规则，可从 `DNSZone` 响应，或将符合的查询转发至 `DNSUpstream`。

`DNSUpstream` 是一个上游端点，可表示纯文本的 UDP/TCP DNS、DoT 及 DoH。

## 启动与部分拉起

`DNSResolver` 不会等到所有依赖都就绪才开始服务。启动时，它会使用当时已经解析出的监听地址与
source 拉起守护进程，并在其余依赖就绪后收敛：

- listen entry 会绑定当前已解析出的地址；若某个地址的 `*From` source 尚未就绪
  （例如仍在等待 DHCPv6 prefix delegation 的 delegated-prefix address），会在后续
  reconcile 中加入。
- 若某个 forward/upstream source 的动态上游尚未解析（例如上游来自
  `DHCPv6Information` server 的 AFTR forwarder），会先省略该 source，直到该上游出现。
  zone source 以及使用静态或已解析上游的 source 会立即提供服务。

当仍有部分内容在等待时，资源会报告 `phase: Degraded`，并带有 `waiting` list，列出每个
listen/source 正在等待什么。这是正常的 bootstrap 状态，不是故障：通用 DNS 已经可以响应。
依赖资源发布 status 后，controller 会重新 reconcile，并以完整配置收敛到
`phase: Applied`（与一开始就全部解析完成的启动结果相同）。只有在完全没有监听地址可解析，
或没有任何可用 source 保留时，解析器才会报告 `phase: Pending`（不提供任何服务）。

这消除了等待 DHCPv6 prefix delegation 时 DNS 被拒绝的启动窗口（在生产路由器上的实测：
AFTR forwarder 显示 `Degraded` 时，通用 DNS 从第一秒起即可响应；delegated prefix 到达后
收敛到 `Applied`）。主动重启 `routerd` 时，进程自身重启期间仍会有不到一秒的间隙。

## 响应来源的评估顺序

引用解析器的 `DNSForwarder` 依配置中的顺序评估。
具有 `zoneRefs` 的转发器从 `DNSZone` 响应。
具有 `upstreams` 的转发器将符合的查询转发至上游。
`match: ["."]` 是默认的递归查询路径。

解析器支持 DoH、DoT、TCP DNS 及纯文本 UDP DNS。
上游依优先顺序尝试。
优先级高的上游失败时，切换至下一个上游。

## 多监听配置

`spec.listen` 是数组。
每个监听可选择要使用的响应来源名称子集合。
这让 LAN 用与 VPN 用的监听可有不同的行为，
同时仍共用一个解析器资源。
`listen[].sources` 中的名称引用 `DNSForwarder`。省略时，使用该解析器所属的所有转发器。

若监听地址需从其他资源状态获取，请使用 `listen[].addressFrom`。
由于明确表达了依赖关系，当来源资源变更时，可重新配置守护进程。

```yaml
listen:
  - name: lan
    addresses:
      - 192.0.2.1
    addressFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    port: 53
```

若所需地址尚无法解析，解析器不会以旧地址启动，
而是保持 `Pending(AddressUnresolved)` 状态等待。

## 动态区域记录

`DNSZone.spec.records[].ipv4` 与 `ipv6` 为固定值。
若记录的地址需从其他资源状态获取，请使用 `ipv4From` 或
`ipv6From`。

```yaml
records:
  - hostname: router
    ipv4From:
      resource: IPv4StaticAddress/lan-base
      field: address
    ipv6From:
      resource: IPv6DelegatedAddress/lan-base
      field: address
```

若所需的引用来源尚无法解析，该记录会记录至 `DNSZone.status.pendingRecords`。
来源资源变更时，解析器会重新生成，并在成功解析后发布记录。

## 限定网络的上游

`DNSUpstream.spec.sourceInterface` 在 Linux 上绑定发出接口。
以固定值指定 `ens18` 或 `wg0` 等 OS 接口名称。
若隧道或 VRF 资源负责建立该接口，请通过资源的拥有权或顺序明确表达依赖关系，
让解析器等待接口就绪后再启动。

`DNSUpstream.spec.bootstrap` 是用于解析 DoH 或 DoT 连接目标名称的辅助 DNS 服务器，
适用于连接目标名称只能在接入网络内部解析的场景。

若上游服务器列表需从其他资源状态获取，请使用 `addressFrom`。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DNSForwarder
metadata:
  name: ngn-aftr
spec:
  resolver: DNSResolver/lan-resolver
  match:
    - transix.jp
  upstreams:
    - DNSUpstream/wan-dns
---
apiVersion: net.routerd.net/v1alpha1
kind: DNSUpstream
metadata:
  name: wan-dns
spec:
  protocol: udp
  addressFrom:
    - resource: DHCPv6Information/wan-info
      field: dnsServers
```

用户编写的 YAML 不接受 `DNSResolver.spec.sources`。请将旧式的内嵌 source 拆分为 `DNSForwarder` 与 `DNSUpstream`。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DNSForwarder
metadata:
  name: default
spec:
  resolver: DNSResolver/lan-resolver
  match:
    - "."
  upstreams:
    - DNSUpstream/cloudflare
---
apiVersion: net.routerd.net/v1alpha1
kind: DNSUpstream
metadata:
  name: cloudflare
spec:
  protocol: doh
  address: cloudflare-dns.com
  path: /dns-query
```

## 与 dnsmasq 的界线

dnsmasq 仅负责 DHCPv4、DHCPv6、DHCP 中继及 RA。
不生成 `server=`、`local=`、`host-record=`。
DNS 的响应与转发全部由 `routerd-dns-resolver` 负责。
