---
title: ハイパーバイザー間のオーバーレイ VPN を置き換える
---

# ハイパーバイザー間のオーバーレイ VPN を置き換える

![hypervisor overlay VPN を WireGuard underlay、optional VXLAN、health check、MTU verification、routerctl visibility で置き換える流れ](/img/diagrams/how-to-pve-overlay.png)

## 想定するシーン

ハイパーバイザークラスター (Proxmox VE, KVM など) で、ノード間のブリッジを、すでに何らかの重量級のオーバーレイ VPN (ベンダー製の SoftEther bridge, 別の tap ベースのトンネルなど) に乗せている構成です。
症状としては、別ホスト上のゲスト同士の通信が遅い、MTU がずれる、ハイパーバイザー本体やルーターとは別の製品なので運用が脆い、といった問題があります。

これを、次の性質を備えた仕組みに置き換えたいとします。

- 他のネットワーク設定と同じく、宣言型で管理する。
- L2 拡張ではなく、原則として L3 経路にする。
- MTU の挙動が予測できる。
- すでに使っている `routerctl` と Web 管理画面から観測できる。

## routerd での解決方法

routerd は、オーバーレイを 4 つのプリミティブでモデル化します。

| リソース | 役割 |
| --- | --- |
| `WireGuardInterface` | ハイパーバイザー間の暗号化 L3 underlay |
| `WireGuardPeer` | ピア (リモートホスト) ごとの公開鍵, エンドポイント, 許可 IP |
| `VXLANTunnel` | underlay 上に乗せる L2 セグメント (本当に L2 拡張が必要なときのみ) |
| `EgressRoutePolicy` + `HealthCheck` | underlay の準備完了確認と L3 フェイルオーバー (任意) |

可能な限り L3 ルーティングを優先してください。
L2 拡張は MTU の制約 (Ethernet ヘッダー + WireGuard のオーバーヘッド + VXLAN ヘッダー) を重ねるうえに、ブロードキャストストームが複数ホスト規模に広がります。
`VXLANTunnel` は、本当にホスト間で広げる必要があるセグメントだけに使ってください。

## 最小構成

ハイパーバイザー 2 ホスト (`alpha` / `beta`) が既存の IP トランスポートでつながっている前提で、まず WireGuard underlay を立ち上げ、必要なら VXLAN を 1 セグメント載せます。

### 2 ホスト間の underlay

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

`mtu: 1420` は、IPv4 underlay の既定値として保守的な値です (1500 - 20 IP - 8 UDP - 32 WireGuard オーバーヘッド - 8 nonce/key = 1432 が理論値で、余裕を込めています)。

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

VXLAN は 50 バイトのヘッダー (外側 Ethernet 14 + 外側 IPv4 20 + UDP 8 + VXLAN 8) を加えるため、1420 の WireGuard 内では内側の MTU が 1370 になります。
カプセル化を重ねるときは、MTU を明示し、自動計算に任せないでください。

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

`routerctl diagnose egress` も役立ちます (例: 拠点向けのトラフィックを、公衆のデフォルト経路ではなく WireGuard underlay へ流したいとき)。

## 運用上のヒント

- **まず 1 ホストのペアから試す。** WireGuard や VXLAN が収束しない場合に備えて、ハイパーバイザーのコンソールアクセスを確保しておきます。
- **MTU の誤りは「ping は速いが大容量転送だけ遅い」という現象の最大の要因です。** `ping -M do -s <size>` で underlay と overlay の両方の MTU を確認してから本番に投入してください。
- **Linux NIC の offload をむやみに切らないでください。** TSO/GSO/GRO そのものは、通常は問題ありません。`mtu greater than device maximum` の真因は、ゲスト側ではなくハイパーバイザーの tap/veth の offload にあることが多いです。
- **移行期間中は、新旧のオーバーレイを同一セグメントに載せないでください。** 新しい WireGuard underlay は旧 SoftEther とは別のセグメントに置き、計画的にカットオーバーします。

## 関連項目

- [Path MTU と MSS clamping](../concepts/path-mtu.md)
- [マルチ WAN 切替](./multi-wan.md)
