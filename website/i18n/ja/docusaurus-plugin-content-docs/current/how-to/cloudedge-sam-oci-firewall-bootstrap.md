---
title: CloudEdge SAM - OCI Ubuntu イメージのファイアウォールブートストラップ
---

# CloudEdge SAM: OCI Ubuntu イメージのファイアウォールブートストラップ

![OCI Ubuntu ゲストファイアウォールのデフォルトが WireGuard と SAM 転送をブロックし、必要なブートストラップ許可と routerctl doctor チェックを示す図](/img/diagrams/how-to-cloudedge-sam-oci-firewall-bootstrap.png)

> 実験的機能（CloudEdge SAM）。これは**ホストブートストラップ / プロバイダーイメージの動作**であり、routerd データプレーンの問題ではありません。SAM ルーターとして使用する OCI の Canonical Ubuntu イメージに適用されます。

## 症状

OCI では、Canonical Ubuntu 24.04 イメージが `iptables-nft` フィルタルールを有効な状態で起動し、**SSH/ICMP 以外のインバウンドトラフィックを reject し、すべての FORWARD トラフィックを reject** します。このデフォルトでは SAM ルーターは:

- OCI セキュリティリストが `UDP/51820` を許可し、VNIC が `skipSourceDestCheck=true` であっても、WireGuard ハンドシェイクを受信**できません** — ホストファイアウォールがインバウンドの WireGuard パケットを `wg-hybrid` リスナーに到達する前にドロップします。
- キャプチャ/オーバーレイトラフィックを転送**できません** — デフォルトの `FORWARD` reject が VNIC インターフェースと `wg-hybrid` 間の SAM デリバリーパスをブロックします。

これはクラウドセキュリティリスト / VNIC の source-dest-check とは独立しています。それらはファブリック層で動作します。**ゲスト OS ファイアウォール**は別レイヤーであり、SAM パスを個別に許可する必要があります。

## 必要な許可（ゲスト OS）

各 OCI SAM ルーターで、ホストファイアウォールが以下を許可していることを確認します:

- `wg-hybrid` WireGuard リスナーへの**インバウンド `UDP/51820`**。
- OCI VNIC インターフェース（例: `ens3`）と `wg-hybrid` の間の双方向 **`FORWARD`**。

これらはアドホックな `iptables` ルール（リビルドで失われる）に頼るのではなく、ルーター設定でホストブートストラップの一部として宣言的に記述してください（他の「ルーターの前提条件」と同様に、クリーンなホストで証明できるようにします）。

## 診断方法

`routerctl doctor hybrid` は、WireGuard / SAM パスをブロックするゲストファイアウォールの reject-all `FORWARD`/`INPUT` パターンを検出するため、許可漏れがサイレントな「ハンドシェイクなし」ではなくレポートとして表示されます。デプロイ後に OCI ルーターで実行してください:

```
routerctl doctor hybrid
```

WireGuard エンドポイントがハンドシェイクを示さないのにピアが keepalive を送信している場合は、まずゲストファイアウォール（この How-to）を確認し、次に OCI セキュリティリスト、次に VNIC の source-dest-check を確認してください。

## 関連項目

- [Selective Address Mobility](../reference/selective-address-mobility)
- OCI Ubuntu イメージはデフォルトの `iptables-nft` 設定が AWS/Azure イメージと異なります。AWS/Azure の SAM スモークでこの問題が発生しなかったのは、それらのイメージがデフォルトで `FORWARD` を reject-all しないためです。
