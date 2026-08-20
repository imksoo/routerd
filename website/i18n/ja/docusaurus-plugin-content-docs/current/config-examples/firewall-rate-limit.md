---
title: ファイアウォールのレート制限と ICMP ルール
---

# ファイアウォールのレート制限と ICMP ルール

![WAN トラフィッククラス、FirewallRule のレートと接続数制限、生成されるステートフルな nftables フィルタリングの構成](/img/diagrams/config-example-firewall-rate-limit.png)

この例は、小規模なルーター向けのステートフルな `FirewallRule` の書き方を示します。

- HTTP と HTTPS を 1 つの複数ポートのルールで許可する
- WAN からの ICMP echo request だけを許可する
- パケットのレート、または送信元ごとの接続数の上限を超えた SSH の試行を reject する

完全な YAML は `examples/firewall-rate-limit.yaml` にあります。

:::caution ファイアウォール機能の現在地

これらは routerd の現在のファイアウォール機能の土台を示すルール例であり、
インターネットに公開するルーターの完全なセキュリティポリシーではありません。
隔離したホストで試し、到達可能なルーターの唯一の防御としては使わないでください。

:::

## daemon の前に確認する

以下は `sudo` を実行できる通常ユーザーを想定します。まだサービスを起動していないため、
`routerctl` ではなく `routerd` を使い、dry-run の状態ファイルは一時ディレクトリへ隔離します。

```sh
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config examples/firewall-rate-limit.yaml
sudo routerd apply --config examples/firewall-rate-limit.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
sudo sed -n "1,160p" "$LAB_DIR/status.json"
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
