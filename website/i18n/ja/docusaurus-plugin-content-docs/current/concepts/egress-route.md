# EgressRoutePolicy

`EgressRoutePolicy` は、外向き通信で使う経路を選びます。
以前の WAN 経路ポリシー を置き換えます。
旧 Kind 名は受け付けません。

このポリシーは候補リソースと HealthCheck を見ます。
選んだ候補は status に保存します。
ほかのリソースは、その status を参照できます。

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

`destinationCIDRs` は、ポリシーの対象宛先です。
省略時は IPv4 で `0.0.0.0/0` を使います。
IPv6 では `::/0` を使います。

`gatewaySource` は gateway の決め方です。

- `none`: DS-Lite などのポイントツーポイントデバイスで使います。
- `static`: `gateway` に次ホップアドレスを書きます。
- `dhcpv4` と `dhcpv6`: DHCP クライアント由来の gateway 用です。

選択結果は次の status に入ります。

- `status.selectedCandidate`
- `status.selectedDevice`
- `status.selectedGateway`
- `status.destinationCIDRs`

起動直後は、まず準備完了の候補を選びます。
重みが最も高い経路を無期限に待ちません。
あとから重みが高い候補が準備完了になると、
routerd は `routerd.lan.route.changed` を発行します。
それを受けて `IPv4Route` と `NAT44Rule` が更新されます。
conntrack は消しません。
既存の通信はカーネルが持つ状態に従います。
新しい通信は、新しい経路と NAT の向きを使います。

`IPv4Route` は、これらの status を参照できます。

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

DS-Lite （あるいはどの tunnel）へ出してはいけない内部向け宛先は、通常の経路として表現します。
上流 gateway 側の private subnet は WAN 側へ、内部の `10.0.0.0/8` や `172.16.0.0/12` は専用経路へ、破棄したい範囲は `type: blackhole` を使います。

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

`HealthCheck` は、probe の intent を宣言します。

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

`HealthCheck` が `EgressRoutePolicy` の candidate や target から参照されている場合、
routerd は health-check daemon、socket mark、source binding をその route target から
自動導出します。config は probe intent だけを持ち、platform ごとの socket mechanics は
controller と renderer の内部に閉じ込めます。
