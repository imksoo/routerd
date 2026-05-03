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
      device: ${DSLiteTunnel/ds-lite.status.interface}
      gatewaySource: none
      weight: 80
      healthCheck: internet-tcp443
    - name: ix2215
      source: Interface/ix2215
      device: ${Interface/ix2215.status.ifname}
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

`IPv4Route` は、これらの status を参照できます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: default-route
spec:
  destination: 0.0.0.0/0
  device: ${EgressRoutePolicy/ipv4-default.status.selectedDevice}
  gateway: ${EgressRoutePolicy/ipv4-default.status.selectedGateway}
```

## HealthCheck

`HealthCheck` は、probe の送信経路を固定できます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: internet-tcp443
spec:
  daemon: routerd-healthcheck
  target: 1.1.1.1
  protocol: tcp
  port: 443
  sourceInterface: ds-routerd-test
```

Linux では、`routerd-healthcheck` が `sourceInterface` に
`SO_BINDTODEVICE` を使います。
routerd の設定では、`sourceInterface` に `Interface` や
`DSLiteTunnel` などのネットワークリソース名を書きます。
routerd は probe を実行する前に、リソース名を OS のインターフェース名へ解決します。
単体の `routerd-healthcheck` の引数では、OS のインターフェース名を直接指定します。
`sourceAddress` がある場合は、送信元アドレスを bind します。
`via` は probe 経路の gateway を記録します。
経路の作成は route policy リソースが担当します。
