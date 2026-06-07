---
title: 取代 Hypervisor 間的 Overlay VPN
---

# 取代 Hypervisor 間的 Overlay VPN

![以 WireGuard underlay、optional VXLAN、health check、MTU verification 與 routerctl visibility 取代 hypervisor overlay VPN 的流程](/img/diagrams/how-to-pve-overlay.png)

## 適用情境

在 Hypervisor 叢集（Proxmox VE、KVM 等）中，節點間的 bridge 已架設於某種重量級的 overlay VPN 之上（廠商製 SoftEther bridge、其他 tap 式隧道等）。
常見症狀包括：不同主機上的 guest 之間通訊緩慢、MTU 不一致、由於是 Hypervisor 本體與路由器以外的獨立產品而導致運維脆弱。

希望將其替換為具備以下特性的方案：

- 與其他網路設定一樣，以宣告式方式管理。
- 原則上採用 L3 路由，而非 L2 延伸。
- MTU 行為可預測。
- 可從既有的 `routerctl` 與 Web 管理介面進行觀測。

## routerd 的解決方式

routerd 以四個原語對 overlay 進行建模：

| 資源 | 角色 |
| --- | --- |
| `WireGuardInterface` | Hypervisor 間的加密 L3 underlay |
| `WireGuardPeer` | 各對等節點（遠端主機）的公鑰、端點、允許 IP |
| `VXLANTunnel` | 架設於 underlay 上的 L2 區段（僅在確實需要 L2 延伸時使用） |
| `EgressRoutePolicy` + `HealthCheck` | underlay 就緒確認與 L3 故障切換（選用） |

請盡可能優先採用 L3 路由。L2 延伸不僅疊加 MTU 限制（Ethernet 標頭 + WireGuard 額外負荷 + VXLAN 標頭），廣播風暴也會擴散至多台主機的規模。
`VXLANTunnel` 請僅用於確實需要跨主機延伸的區段。

## 最小配置

以兩台 Hypervisor（`alpha` / `beta`）已透過現有 IP 傳輸連接為前提，先建立 WireGuard underlay，若有需要再載入一個 VXLAN 區段。

### 兩主機間的 underlay

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

`mtu: 1420` 是 IPv4 underlay 的保守預設值（理論值為 1500 - 20 IP - 8 UDP - 32 WireGuard 額外負荷 - 8 nonce/key = 1432，已留有餘裕）。

### Underlay 上的 L2 延伸區段

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

VXLAN 會增加 50 位元組的標頭（外側 Ethernet 14 + 外側 IPv4 20 + UDP 8 + VXLAN 8），
因此在 MTU 1420 的 WireGuard 內，內側 MTU 為 1370。
進行多層封裝時，請明確指定 MTU，不要仰賴自動計算。

## 驗證

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

`routerctl diagnose egress` 在需要將特定目的地的流量導向 WireGuard underlay 而非公共預設路由時也很有用。

## 操作建議

- **先從一對主機開始驗證**。萬一 WireGuard 或 VXLAN 無法收斂，請事先確保 Hypervisor 的控制台存取權限。
- **MTU 設定錯誤是「ping 快但大量傳輸慢」問題的最主要原因**。請先以 `ping -M do -s <size>` 確認 underlay 與 overlay 兩者的 MTU，再投入正式環境。
- **請勿隨意停用 Linux NIC 的 offload**。TSO/GSO/GRO 本身通常不是問題。`mtu greater than device maximum` 的根本原因多半在於 Hypervisor 的 tap/veth offload，而非 guest 側。
- **移轉期間，請勿將新舊 overlay 放在同一區段**。新的 WireGuard underlay 請與舊 SoftEther 放在不同區段，並有計劃地進行切換（cutover）。

## 相關項目

- [Path MTU 與 MSS clamping](../concepts/path-mtu.md)
- [多 WAN 切換](./multi-wan.md)
