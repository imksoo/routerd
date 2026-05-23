---
title: 替换 Hypervisor 间的 Overlay VPN
---

# 替换 Hypervisor 间的 Overlay VPN

## 适用场景

在 Hypervisor 集群（Proxmox VE、KVM 等）中，节点间的 bridge 已架设于某种重量级的 overlay VPN 之上（厂商制 SoftEther bridge、其他 tap 式隧道等）。
常见症状包括：不同主机上的 guest 之间通信缓慢、MTU 不一致、由于是 Hypervisor 本体与路由器以外的独立产品而导致运维脆弱。

希望将其替换为具备以下特性的方案：

- 与其他网络配置一样，以声明式方式管理。
- 原则上采用 L3 路由，而非 L2 延伸。
- MTU 行为可预测。
- 可从既有的 `routerctl` 与 Web 管理界面进行观测。

## routerd 的解决方式

routerd 以四个原语对 overlay 进行建模：

| 资源 | 角色 |
| --- | --- |
| `WireGuardInterface` | Hypervisor 间的加密 L3 underlay |
| `WireGuardPeer` | 各对等节点（远端主机）的公钥、端点、允许 IP |
| `VXLANTunnel` | 架设于 underlay 上的 L2 区段（仅在确实需要 L2 延伸时使用） |
| `EgressRoutePolicy` + `HealthCheck` | underlay 就绪确认与 L3 故障切换（可选） |

请尽可能优先采用 L3 路由。L2 延伸不仅叠加 MTU 限制（Ethernet 标头 + WireGuard 额外开销 + VXLAN 标头），广播风暴也会扩散至多台主机的规模。
`VXLANTunnel` 请仅用于确实需要跨主机延伸的区段。

## 最小配置

以两台 Hypervisor（`alpha` / `beta`）已通过现有 IP 传输连接为前提，先建立 WireGuard underlay，若有需要再载入一个 VXLAN 区段。

### 两主机间的 underlay

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: WireGuardInterface
metadata:
  name: wg-cluster
spec:
  listenPort: 51820
  mtu: 1420
  privateKeyFile: /usr/local/etc/routerd/secrets/wg-cluster.key
---
apiVersion: net.routerd.net/v1alpha1
kind: WireGuardPeer
metadata:
  name: beta
spec:
  interface: wg-cluster
  publicKey: "<beta-public-key>"
  endpoint: "beta.cluster.example.net:51820"
  allowedIPs:
    - 10.250.0.2/32
  persistentKeepalive: 25
```

`mtu: 1420` 是 IPv4 underlay 的保守默认值（理论值为 1500 - 20 IP - 8 UDP - 32 WireGuard 额外开销 - 8 nonce/key = 1432，已留有余量）。

### Underlay 上的 L2 延伸区段

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: VXLANTunnel
metadata:
  name: vx-bridge1
spec:
  vni: 123001
  localAddress: 10.250.0.1
  peers:
    - 10.250.0.2
  underlayInterface: wg-cluster
  udpPort: 4789
  mtu: 1370
```

VXLAN 会增加 50 字节的标头（外侧 Ethernet 14 + 外侧 IPv4 20 + UDP 8 + VXLAN 8），
因此在 MTU 1420 的 WireGuard 内，内侧 MTU 为 1370。
进行多层封装时，请明确指定 MTU，不要依赖自动计算。

## 验证

```sh
# Underlay
routerctl describe WireGuardInterface/wg-cluster
ip -d link show wg-cluster
ping -M do -s 1392 <peer-underlay-address>   # 1420 - 20 IP - 8 ICMP

# Overlay
routerctl describe VXLANTunnel/vx-bridge1
ip -d link show vx-bridge1
ping -M do -s 1342 <peer-overlay-host>        # 1370 - 20 IP - 8 ICMP
```

`routerctl diagnose egress` 在需要将特定目的地的流量导向 WireGuard underlay 而非公共默认路由时也很有用。

## 操作建议

- **先从一对主机开始验证**。万一 WireGuard 或 VXLAN 无法收敛，请事先确保 Hypervisor 的控制台访问权限。
- **MTU 配置错误是「ping 快但大量传输慢」问题的最主要原因**。请先以 `ping -M do -s <size>` 确认 underlay 与 overlay 两者的 MTU，再投入正式环境。
- **请勿随意禁用 Linux NIC 的 offload**。TSO/GSO/GRO 本身通常不是问题。`mtu greater than device maximum` 的根本原因多半在于 Hypervisor 的 tap/veth offload，而非 guest 侧。
- **迁移期间，请勿将新旧 overlay 放在同一区段**。新的 WireGuard underlay 请与旧 SoftEther 放在不同区段，并有计划地进行切换（cutover）。

## 相关项目

- [Path MTU 与 MSS clamping](../concepts/path-mtu.md)
- [多 WAN 切换](./multi-wan.md)
