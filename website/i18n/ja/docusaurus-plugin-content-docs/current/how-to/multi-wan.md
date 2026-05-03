# 複数 WAN の経路選択

1 台のルーターに複数の外向き経路がある場合は、
`EgressRoutePolicy` を使います。
このポリシーは、準備完了の候補から重みが最も高いものを選びます。

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
  hysteresis: 30s
  candidates:
    - name: ds-lite
      source: DSLiteTunnel/ds-lite
      device: ${DSLiteTunnel/ds-lite.status.interface}
      gatewaySource: none
      weight: 80
      healthCheck: internet-tcp443
    - name: fallback
      source: Interface/fallback
      device: ${Interface/fallback.status.ifname}
      gatewaySource: static
      gateway: 172.17.0.1
      weight: 50
```

外部到達性を確認したい経路には、HealthCheck を追加します。

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
  interval: 30s
  timeout: 3s
```

経路や NAT のリソースは、選ばれたデバイスを参照できます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-wan
spec:
  type: masquerade
  egressPolicyRef: ipv4-default
  sourceRanges:
    - 192.168.0.0/16
```

管理通信は管理インターフェースで扱います。
firewall 変更の適用時に、WAN 側の SSH 経路を前提にしないでください。
