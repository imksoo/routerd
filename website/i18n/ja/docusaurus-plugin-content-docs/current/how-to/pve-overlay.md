---
title: ハイパーバイザー間の overlay VPN を置き換える
---

# ハイパーバイザー間の overlay VPN を置き換える

## 想定するシーン

ハイパーバイザークラスター (Proxmox VE、KVM 等) で、ノード間ブリッジを既に何らかの heavyweight な overlay VPN (ベンダー製の SoftEther bridge、別 tap ベースのトンネル等) に乗せている。
症状として、別ホスト上のゲスト同士の通信が遅い、MTU がずれる、ハイパーバイザー本体やルーターと別プロダクトなので運用が脆い、といった問題がある。

これを次の性質で置き換えたい：

- 他のネットワーク設定と同じく宣言的に管理する
- L2 拡張ではなく原則 L3 経路にする
- MTU の挙動が予測できる
- 既に使っている `routerctl` と Web Console から観測できる

## routerd での解決方法

routerd は overlay を 4 つの primitive でモデル化します。

| リソース | 役割 |
| --- | --- |
| `WireGuardInterface` | ハイパーバイザー間の暗号化 L3 underlay |
| `WireGuardPeer` | ピア (リモートホスト) ごとに公開鍵、エンドポイント、許可 IP |
| `VXLANTunnel` | underlay 上に乗せる L2 セグメント (本当に L2 拡張が必要なときのみ) |
| `EgressRoutePolicy` + `HealthCheck` | underlay の readiness と L3 failover (任意) |

可能な限り L3 ルーティングを優先してください。L2 拡張は MTU 制約 (Ethernet header + WireGuard overhead + VXLAN header) を多重化するうえに、broadcast storm が複数ホスト規模に拡大します。
`VXLANTunnel` は本当にホスト間で広げる必要があるセグメントだけに使ってください。

## 最小構成

ハイパーバイザー 2 ホスト (`alpha` / `beta`) が既存 IP transport で繋がっている前提で、まず WireGuard underlay を上げ、必要なら VXLAN を 1 セグメント載せます。

### 2 ホスト間の underlay

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: WireGuardInterface
metadata:
  name: wg-cluster
spec:
  listenPort: 51820
  mtu: 1420
  privateKeyFromSecret: wg-cluster-key
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

`mtu: 1420` は IPv4 underlay 既定値の保守的な値です (1500 - 20 IP - 8 UDP - 32 WireGuard overhead - 8 nonce/key = 1432 が理論値、headroom 込み)。

### Underlay 上の L2 拡張セグメント

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

VXLAN は 50 バイトのヘッダ (14 outer Ethernet + 20 outer IPv4 + 8 UDP + 8 VXLAN) を加えるので、1420 の WireGuard 内では内側 MTU が 1370 になります。
カプセル化を重ねるときは MTU を明示し、自動計算に任せないでください。

## 動作確認

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

`routerctl diagnose egress` も役立ちます (例: 拠点向けトラフィックを公衆 default route ではなく WireGuard underlay に流したいとき)。

## 運用上のヒント

- **まず 1 ホストペアから試す**。WireGuard / VXLAN が収束しない場合に備えて、ハイパーバイザーの console アクセスを確保しておきます。
- **MTU ミスは「ping は速いが大容量転送だけ遅い」現象の最大要因**。`ping -M do -s <size>` で underlay と overlay 双方の MTU を確認してから本番投入してください。
- **Linux NIC の offload を無闇に切らないでください**。TSO/GSO/GRO 自体は通常問題ありません。`mtu greater than device maximum` の真因はゲスト側ではなくハイパーバイザーの tap/veth offload にあるケースが多いです。
- **移行期間中、新旧 overlay を同一セグメントに載せない**。新 WireGuard underlay は旧 SoftEther と別セグメントに置き、計画的にカットオーバーしてください。

## 関連項目

- [Path MTU と MSS clamping](../concepts/path-mtu.md)
- [マルチ WAN 切替](./multi-wan.md)
