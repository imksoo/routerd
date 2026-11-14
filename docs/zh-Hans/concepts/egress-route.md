# EgressRoutePolicy

`EgressRoutePolicy` 选择对外通信所使用的路由，
取代此前的 WAN 路由策略。
不接受旧 Kind 名称。

此策略观察候选资源与 HealthCheck，并将所选候选存储于 status。
其他资源可引用该 status。

`spec.mode` 的不同会影响 status 的拥有者。省略 `mode` 时，
egress-route 选择器仅输出含选择结果的 status，以及附有 `role: advisory` / `advisory: true`
的 `routerd.lan.route.changed` 事件。此 status 是运行中控制器的输出，并非应用的模拟执行结果。
`mode: priority`、`mark`、`hash` 时，policy 路由控制器成为
实际应用的路由与 NAT mark 状态的拥有者。相依的控制器改为监听 `routerd.resource.status.changed`，而非旧式的 route-changed 事件。

`mode: priority` 同样使用 `selection: highest-weight-ready`。
从准备就绪的候选中选出 weight 最高的那一个，`priority` 作为平局决胜与
policy 路由规则的优先级。`priority` 不是选择策略的替代品。`weighted-ecmp` 是实现前的保留值，不会静默忽略，而是报告为 `UnsupportedSelection`。`enabled: false` 的候选不列入选择对象，也不拥有所生成的 policy 路由规则与路由表。

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
  candidates:
    - name: ds-lite
      source: DSLiteTunnel/ds-lite
      deviceFrom:
        resource: DSLiteTunnel/ds-lite
        field: interface
      gatewaySource: none
      weight: 80
      healthCheck: internet-tcp443
    - name: ix2215
      source: Interface/ix2215
      deviceFrom:
        resource: Interface/ix2215
        field: ifname
      gatewaySource: static
      gateway: 172.17.0.1
      weight: 50
```

`destinationCIDRs` 是策略的目的地范围。
省略时，IPv4 默认使用 `0.0.0.0/0`。
IPv6 默认使用 `::/0`。

`gatewaySource` 决定网关的选取方式。

- `none`：用于 DS-Lite 等点对点设备。
- `static`：在 `gateway` 中填写 next hop 地址。
- `dhcpv4` 与 `dhcpv6`：用于来自 DHCP 客户端的网关。

选择结果写入以下 status：

- `status.selectedCandidate`
- `status.selectedDevice`
- `status.selectedGateway`
- `status.selectedWeight`
- `status.selectedTargets`
- `status.destinationCIDRs`

启动后，首先选择准备就绪的候选，不会无限期等待 weight 最高的路由。
若之后 weight 较高的候选进入就绪状态，
routerd 会发出 `routerd.lan.route.changed`，
进而更新 `IPv4Route` 与 `NAT44Rule`。
此时不会清除 conntrack。
现有通信依内核持有的状态继续，
新通信则使用新路由与新 NAT 方向。

`IPv4Route` 可引用这些 status：

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: default-route
spec:
  destination: 0.0.0.0/0
  deviceFrom:
    resource: EgressRoutePolicy/ipv4-default
    field: selectedDevice
  gatewayFrom:
    resource: EgressRoutePolicy/ipv4-default
    field: selectedGateway
```

不应经由 DS-Lite（或任何隧道）的内部目的地，以一般路由方式表达。
上游网关侧的私有子网指向 WAN 侧，内部的 `10.0.0.0/8` 或 `172.16.0.0/12` 使用专属路由，需要丢弃的范围使用 `type: blackhole`。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: private-10-blackhole
spec:
  type: blackhole
  destination: 10.0.0.0/8
```

## HealthCheck

`HealthCheck` 声明探测的意图。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: internet-tcp443
spec:
  target: 1.1.1.1
  protocol: tcp
  port: 443
```

`HealthCheck` 被 `EgressRoutePolicy` 的候选或 target 引用时，
routerd 自动从该路由 target 导出 health-check 守护进程、socket mark 及来源绑定。配置中只需描述探测的意图，各平台的 socket 机制则封闭在控制器与生成器内部。
