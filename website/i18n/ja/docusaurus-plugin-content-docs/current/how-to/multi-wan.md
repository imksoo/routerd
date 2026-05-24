---
title: ヘルスチェック付きマルチ WAN 切替
---

# ヘルスチェック付きマルチ WAN 切替

## 想定するシーン

ルーターから外向きの経路が複数あり、routerd に次のように振る舞ってほしい場合です。

- 利用可能な経路の中から最良のものを自動で選ぶ。
- 主回線が不調になったらフォールバック回線に切り替える。
- 既存の接続を切らずに、なめらかに切り替える。

代表的な例を挙げます。

- 家庭ルーターで、DS-Lite トンネルを主回線、上流 HGW の NAT 越しをフォールバックにしたい。
- SOHO ルーターで、2 系統の ISP 回線（光 + LTE など）を冗長化したい。
- 拠点ルーターで、社内 VPN 回線を優先し、つながらないときだけ公衆インターネットへ落としたい。

## routerd での解決方法

`EgressRoutePolicy` で候補の経路と選択方法を宣言します。
routerd は常に、「ready（上流リソースが落ち着いている）かつ healthy（`HealthCheck` が通っている）」候補のうち、weight が最も高いものを選びます。
切り替え時は OS の経路表を更新し、ポリシーに追従する `NAT44Rule` も適用し直しますが、conntrack はあえて消しません。
既存のフローはそのまま継続し、新規のフローだけが新しい経路を使います。

これにより、weight の低いフォールバックが起動直後から働き、あとから確認できた主回線へなめらかに移れます。

試験用 PPPoE など、セッション枠を消費するフォールバックは、YAML に残したまま無効化できます。
`PPPoESession`、対応する `HealthCheck`、`EgressRoutePolicy` の候補に `enabled: false` を指定します。
通常の適用では生成済みのサービスを停止・無効化し、必要なときだけ手動で試験できます。

## 最小構成

構成要素は 3 つです。候補ごとの `HealthCheck`、それらをまとめる `EgressRoutePolicy`、ポリシーに追従する `NAT44Rule` です。

### Health check

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: internet-via-primary
spec:
  target: 1.1.1.1
  protocol: tcp
  port: 443
  interval: 30s
  timeout: 3s
```

各チェックは、対応する `EgressRoutePolicy` の候補から参照します。routerd はその参照からプローブの送信元バインドと socket mark を導出するため、設定にホスト固有の仕組みを書く必要はありません。
ICMP は途中のフィルターで落ちやすいので、安定した宛先（1.1.1.1 など）に対して TCP/443 を使うのが確実です。

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

`hysteresis` はチャタリングを防ぐためのものです。候補が unhealthy になっても、この時間は降格しません。

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

masquerade の送信元アドレスは、その時点で routerd が選んでいる候補のインターフェースから取ります。切り替わると、次のパケットは新しい経路のアドレスで NAT します。

## RFC1918 宛は NAT しない

上流ゲートウェイが LAN への戻り経路を持っている場合、公衆インターネット向けは NAT しつつ、ほかの社内ネットワーク宛は NAT を避けられます。

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

これで RFC1918 宛は経路で配り、公衆インターネット向けは選択された egress を経由します。

## 運用上のヒント

- 必ず帯域外の管理経路（mgmt インターフェース、コンソール、専用 SSH NIC など）を確保してください。WAN 経由の SSH で経路やファイアウォールを変えるのは危険です。
- 1 つの `HealthCheck` は 1 つの候補から参照する形にしてください。routerd が単一のプローブ経路を導出でき、「プローブ失敗 = その経路が壊れている」と解釈しやすくなります。
- 切り替え時に conntrack をフラッシュしないでください。routerd は意図的にフラッシュしません。すでにハンドシェイク済みの TCP は自然に終わらせます。
- 現在選択中の候補は、`routerctl describe EgressRoutePolicy/<name>` の `status.selectedCandidate` で確認できます。

## 関連項目

- [Path MTU と MSS clamping](../concepts/path-mtu.md)
- [ファイアウォールルールの基本](./firewall-rule.md)
- [DS-Lite 設定](./flets-ipv6-setup.md)
