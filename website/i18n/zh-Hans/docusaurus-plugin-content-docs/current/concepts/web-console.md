# Web 管理界面

`WebConsole` 是用来读取 routerd 状态的 HTTP 界面。
设计上以管理网络的本地运用为前提。
不执行配置变更、服务重新启动、资源应用或状态数据库的编辑。

配置的变更仅限于 YAML 文件和 `routerctl` 命令。
浏览器仅作为观测用途。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: WebConsole
metadata:
  name: mgmt
spec:
  enabled: true
  listenAddressFrom:
    resource: Interface/mgmt
    field: ipv4Addresses
  port: 8080
  title: edge-router
```

请将监听地址限定于管理地址。
请勿在 untrust 的 WAN 接口上公开。
若管理地址由操作系统或 IPAM 提供，请使用 `listenAddressFrom`。
routerd 会在启动时从资源状态解析其值。
若需要固定的备用地址，也可以同时使用 `listenAddress`。

## 读取的信息

Web 管理界面会读取以下信息。

- routerd 守护进程状态
- SQLite 状态数据库中的资源状态
- SQLite 事件表格中的总线事件
- 从 conntrack 或 pf 状态取得的连接观测值
- `dns-queries.db` 的 DNS 查询记录
- `traffic-flows.db` 的连接流量记录
- `firewall-logs.db` 的防火墙拒绝记录
- 当前 dnsmasq 的 DHCP 租约文件，显示设备名称、MAC 地址及本地厂商候选名称
- 当前的 YAML 配置，以只读方式显示

## 当前的界面

当前的 Fluent UI 版 Web 应用程序显示以下内容。

- PD、DS-Lite、DNS、NAT、路由、健康检查、VPN、软件包、sysctl、
  systemd 单元、日志资源的状态摘要
- 阶段或观测值发生变化的资源高亮显示
- 所选事件的详细面板，不因庞大的属性而破坏事件表格版面
- DHCP 租约事件的详细信息，显示 MAC 地址、IP 地址、主机名及资源名称
- 按地址族与协议分类的 Connections 界面，
  支持过滤、排序、分页及行数选择
- 基于独立记录数据库的 DNS 查询、连接流量、防火墙记录界面
- `/bgp`、`/vrrp`、`/ingress` 专属的 BGP、VRRP、IngressService 运维页面。
  这些页面通过 Server-Sent Events 更新资源表格，并在浏览器端保留
  5/15/60 分钟的轻量 SVG 趋势图，以及仅显示相关资源的事件记录
- Firewall 列汇整显示防火墙记录、DNS 响应、DHCP 租约、
  MAC 厂商候选，以及当前 conntrack 的回程 tuple，
  方便判断被拒绝的数据包究竟是不必要的对外连接，还是接近现有 NAT 转换的另一路径响应
- 具有结构化折叠树与原始 YAML 显示的只读 Config 界面

连接列基本上显示去程方向。
conntrack 虽然会以双向报告同一连接，但不会将回程作为主要列重叠显示。

## API 边界

Web 管理界面 API 为只读。
JSON 端点位于 `/api/v1` 下，SSE 流也可通过短名称
`/api/events/stream` 访问。

| 路径 | 内容 |
| --- | --- |
| `/api/v1/summary` | 状态、资源阶段、最近事件、连接摘要 |
| `/api/v1/resources` | 状态数据库中的资源状态 |
| `/api/v1/events?limit=200&resourceKind=&resourceName=&q=` | 含任意过滤条件的最近总线事件 |
| `/api/v1/events/stream` 或 `/api/events/stream` | `routerd.*` 总线事件的 Server-Sent Events 流 |
| `/api/v1/connections` | 从 conntrack 或 pf 状态取得的连接观测值 |
| `/api/v1/dns-queries?since=1h&client=&qname=&limit=100` | DNS 查询记录列 |
| `/api/v1/traffic-flows?since=1h&client=&peer=&limit=100` | 含 DNS 来源主机名的连接流量记录列 |
| `/api/v1/firewall-logs?since=24h&action=drop&src=&limit=100` | 防火墙记录列 |
| `/api/v1/bgp`、`/api/v1/vrrp`、`/api/v1/ingress` | Kubernetes edge 路由 / VIP 资源的运维状态 |
| `/api/v1/config` | 当前的 YAML 配置 |
| `/api/v1/generations?limit=100` | 已完成的应用世代及 YAML 快照的有无 |
| `/api/v1/generations/<id>/config` | 某一应用世代保存的 YAML |
| `/api/v1/generations/<from>/diff/<to>` | 两个 YAML 世代的差异（unified diff） |
