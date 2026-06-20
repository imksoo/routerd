---
title: ファイアウォールのレート制限と ICMP ルール
---

# ファイアウォールのレート制限と ICMP ルール

![WAN トラフィッククラス、FirewallRule のレートと接続数制限、生成されるステートフルな nftables フィルタリングの構成](/img/diagrams/config-example-firewall-rate-limit.png)

この例は、小規模なルーター向けのステートフルな `FirewallRule` の書き方を示します。

- HTTP と HTTPS を 1 つの複数ポートルールで許可する
- WAN からの ICMP echo request だけを許可する
- パケットレート、または送信元ごとの接続数の上限を超えた SSH の試行を reject する

完全な YAML は `examples/firewall-rate-limit.yaml` にあります。

## 適用手順

```bash
routerctl validate -f examples/firewall-rate-limit.yaml --replace
routerctl plan -f examples/firewall-rate-limit.yaml --replace
```

## ルールの抜粋

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: ssh-bruteforce-over-limit
  spec:
    fromZone: wan
    toZone: self
    protocol: tcp
    destinationPorts:
      - "22"
    action: reject
    rateLimit:
      rate: 8
      burst: 16
      unit: packet
      per: minute
      log: true
    connLimit:
      maxPerSource: 4
      log: true
```
