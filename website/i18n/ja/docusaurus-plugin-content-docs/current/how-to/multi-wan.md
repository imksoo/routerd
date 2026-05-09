---
title: ヘルスチェック付きマルチ WAN 切替
---

# ヘルスチェック付きマルチ WAN 切替

## 想定するシーン

ルーターから外向きの経路が複数あり、routerd に次のように振る舞ってほしい場合：

- 利用可能な経路の中から最良のものを自動選択する。
- 主回線が不調になったら fallback 回線に切り替える。
- 既存接続を切らないソフトな切替を行う。

代表的な例：

- 家庭ルーターで、DS-Lite トンネルを主回線、上流 HGW の NAT 越しを fallback にしたい。
- SOHO ルーターで 2 系統の ISP 回線 (光 + LTE 等) を冗長化したい。
- 拠点ルーターで、社内 VPN 回線を優先し、駄目なときだけ公衆インターネットに落としたい。

## routerd での解決方法

`EgressRoutePolicy` で候補経路と選択方法を宣言します。
routerd は常に「ready (上流リソースが落ち着いている) かつ healthy (`HealthCheck` が通っている)」候補のうち、最も weight が高いものを選びます。
切り替え時は OS の経路表を更新し、ポリシーに追従する `NAT44Rule` も再適用しますが、conntrack はあえて消しません。
既存フローはそのまま継続し、新規フローのみ新しい経路を使います。

これにより、低 weight の fallback が起動直後から仕事をして、後から確認できた主回線にスムーズに移れます。

試験用 PPPoE など、セッション枠を消費する fallback は YAML に残したまま無効化できます。
`PPPoEInterface`、対応する `HealthCheck`、`EgressRoutePolicy` の候補に `disabled: true` を指定します。
通常の適用では生成済みサービスを停止・無効化し、必要なときだけ手動で試験できます。

## 最小構成

3 つの構成要素：候補ごとの `HealthCheck`、それらをまとめる `EgressRoutePolicy`、ポリシーに追従する `NAT44Rule`。

### Health check

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: internet-via-primary
spec:
  daemon: routerd-healthcheck
  target: 1.1.1.1
  protocol: tcp
  port: 443
  sourceInterface: ds-lite-primary
  interval: 30s
  timeout: 3s
```

各 check は候補インターフェースに bind します (`sourceInterface`)。これで本当にその経路で probe が出ます。
ICMP は途中フィルタで落ちやすいので、TCP/443 を安定先 (1.1.1.1 等) に当てるのが安定します。

### Egress policy

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
    - name: ds-lite-primary
      source: DSLiteTunnel/ds-lite-primary
      deviceFrom:
        resource: DSLiteTunnel/ds-lite-primary
        field: interface
      gatewaySource: none
      weight: 90
      healthCheck: internet-via-primary

    - name: hgw-fallback
      source: Interface/wan
      deviceFrom:
        resource: Interface/wan
        field: ifname
      gatewaySource: static
      gateway: 192.0.2.1
      weight: 50
      healthCheck: internet-via-hgw
```

`hysteresis` はチャタリング防止用。候補が unhealthy になってもこの時間は降格しません。

### ポリシーに追従する NAT

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-egress
spec:
  type: masquerade
  egressPolicyRef: ipv4-default
  sourceRanges:
    - 192.0.2.0/24
```

masquerade の送信元アドレスは、その瞬間 routerd が選んでいる候補のインターフェースから取られます。切り替わると、次のパケットは新しい経路のアドレスで NAT されます。

## RFC1918 宛は NAT しない

上流ゲートウェイが LAN への戻り経路を持っている場合、公衆インターネット向けは NAT しつつ、他の社内ネットワーク宛は NAT を回避できます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-wan-hgw
spec:
  type: masquerade
  egressInterface: wan
  sourceRanges:
    - 192.0.2.0/24
  excludeDestinationCIDRs:
    - 192.168.0.0/16
    - 172.16.0.0/12
    - 10.0.0.0/8

---

apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: hgw-lan
spec:
  destination: 192.168.0.0/16
  device: wan
```

これで RFC1918 宛は経路で配り、公衆インターネット向けは選択された egress を経由する形になります。

## 運用上のヒント

- 必ず帯域外の管理経路 (mgmt インターフェース、コンソール、専用 SSH NIC 等) を確保してください。WAN 経由の SSH で経路や firewall を変えるのは危険です。
- Health check は候補インターフェースに bind してください (`sourceInterface: <ifname>`)。これで「probe 失敗 = その経路が壊れている」と一意に解釈できます。
- 切り替え時に conntrack を flush しないでください。routerd は意図的に flush しません。すでに handshake 済みの TCP は自然に終わらせます。
- 現在選択中の候補は `routerctl describe EgressRoutePolicy/<name>` の `status.selectedCandidate` で確認できます。

## 関連項目

- [Path MTU と MSS clamping](../concepts/path-mtu.md)
- [Firewall ルールの基本](./firewall-rule.md)
- [DS-Lite 設定](./flets-ipv6-setup.md)
