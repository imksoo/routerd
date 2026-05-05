# 複数 WAN の経路選択

1 台のルーターに複数の外向き経路がある場合は、`EgressRoutePolicy` を使います。
この方針は、準備ができていて健康な候補のうち、重みが最も高いものを選びます。

この動作は収束型です。
起動直後は、低い重みの fallback 経路でも準備ができれば通信を始めます。
優先経路が後からヘルスチェックに通ると、routerd は選択デバイスを変えます。
その後、経路と NAT リソースを再適用します。
controller は conntrack を消しません。
既存通信はカーネル状態を維持します。
新しい通信は新しく選ばれた経路へ流れます。

## 経路ポリシー

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
    - name: ds-lite-a
      source: DSLiteTunnel/ds-lite-a
      deviceFrom:
        resource: DSLiteTunnel/ds-lite-a
        field: interface
      gatewaySource: none
      weight: 90
      healthCheck: internet-tcp443-dslite-a

    - name: ds-lite-slaac
      source: DSLiteTunnel/ds-lite
      deviceFrom:
        resource: DSLiteTunnel/ds-lite
        field: interface
      gatewaySource: none
      weight: 70
      healthCheck: internet-tcp443-dslite

    - name: hgw-fallback
      source: Interface/wan
      deviceFrom:
        resource: Interface/wan
        field: ifname
      gatewaySource: static
      gateway: 192.168.1.254
      weight: 50
```

インターネット到達性を確認したい経路ごとに、ヘルスチェックを追加します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: internet-tcp443-dslite-a
spec:
  daemon: routerd-healthcheck
  target: 1.1.1.1
  protocol: tcp
  port: 443
  sourceInterface: ds-lite-a
  interval: 30s
  timeout: 3s
```

## NAT を選択経路に追従させる

経路と NAT は、選択された出口ポリシーに追従できます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-egress
spec:
  type: masquerade
  egressPolicyRef: ipv4-default
  sourceRanges:
    - 172.18.0.0/16
```

上流の HGW に LAN サブネットへの静的経路を設定できる場合は、
プライベート宛先を NAT 対象から外せます。
インターネット向け通信だけ NAT します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-wan-hgw
spec:
  type: masquerade
  egressInterface: wan
  sourceRanges:
    - 172.18.0.0/16
  excludeDestinationCIDRs:
    - 192.168.0.0/16
    - 172.16.0.0/12
    - 10.0.0.0/8
```

HGW LAN への静的経路リソースも合わせて書きます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: hgw-lan-via-wan
spec:
  destination: 192.168.0.0/16
  device: wan
```

この形では、RFC 1918 宛ての通信はローカルネットワーク設計の内側に残します。
公開インターネット向けの通信は、選択された出口ポリシーに従います。

## 運用メモ

- SSH は管理インターフェースで維持します。
- ファイアウォールや経路変更中に、untrust WAN 経由でルーターへ SSH しないでください。
- ヘルスチェックは候補インターフェースへ束縛することを推奨します。
- 明確な障害対応でない限り、経路選択の変更時に conntrack を消さないでください。
