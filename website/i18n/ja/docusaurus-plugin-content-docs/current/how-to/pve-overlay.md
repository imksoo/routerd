# PVE ノード間 VPN overlay の置き換え

routerd は、Proxmox ノード間の遅い VPN 経路を置き換える部品を扱えます。
たとえば SoftEther bridge の置き換えを想定できます。
現在の推奨構成は次の形です。

- `WireGuardInterface` で暗号化された L3 underlay を作ります。
- `WireGuardPeer` で相手ノードを表します。
- `VXLANTunnel` は、L2 セグメントをノード間に伸ばす場合だけ使います。
- `EgressRoutePolicy` と `HealthCheck` で L3 経路の準備状態を見ます。

可能なら、L2 延伸より L3 経路を優先してください。
VXLAN は特定のセグメントを伸ばす場合に有効です。
ただし MTU の誤りが起きやすくなります。
WireGuard の上で VXLAN を使う場合は、MTU を明示してください。
`ping -M do -s <size>` で断片化なしの疎通を確認します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: WireGuardInterface
metadata:
  name: wg-pve
spec:
  listenPort: 51820
  mtu: 1420
---
apiVersion: net.routerd.net/v1alpha1
kind: WireGuardPeer
metadata:
  name: pve06
spec:
  interface: wg-pve
  publicKey: "<peer-public-key>"
  endpoint: "pve06.example.net:51820"
  allowedIPs:
    - 10.250.6.0/24
  persistentKeepalive: 25
---
apiVersion: net.routerd.net/v1alpha1
kind: VXLANTunnel
metadata:
  name: vx-svnet1
spec:
  vni: 123001
  localAddress: 10.250.5.1
  peers:
    - 10.250.6.1
  underlayInterface: wg-pve
  udpPort: 4789
  mtu: 1370
```

vpn05 と vpn06 を置き換える場合は、まず管理セグメントを 1 つ選びます。
次に、1 組のノードだけで検証します。
PVE のコンソール経路は必ず残してください。
VXLAN を足す前に WireGuard の MTU を確認します。
その後、PVE bridge または SDN の vNIC が想定したインターフェースを通るか確認します。

確認に使うコマンドの例です。

```sh
routerctl diagnose egress --no-host
ip -d link show wg-pve
ip -d link show vx-svnet1
ping -M do -s 1340 <remote-underlay-address>
```

