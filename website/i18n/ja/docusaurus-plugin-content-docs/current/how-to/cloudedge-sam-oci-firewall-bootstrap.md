---
title: CloudEdge SAM - OCI Ubuntu イメージのファイアウォールブートストラップ
---

# CloudEdge SAM: OCI Ubuntu イメージのファイアウォールブートストラップ

![OCI Ubuntu ゲストファイアウォールのデフォルトが WireGuard と SAM 転送をブロックし、必要なブートストラップ許可と routerctl doctor チェックを示す図](/img/diagrams/how-to-cloudedge-sam-oci-firewall-bootstrap.png)

> 実験的機能（CloudEdge SAM）。これは SAM ルーターとして使用する OCI の Canonical Ubuntu イメージで観測された**プロバイダーイメージのホストファイアウォール動作**と、クリーンホスト上で収束すべき routerd 管理の許可を記録するものです。

## 症状

OCI では、Canonical Ubuntu 24.04 イメージが `iptables-nft` フィルタールールを有効な状態で起動し、**SSH と ICMP 以外のインバウンドトラフィックを reject し、すべての FORWARD トラフィックを reject** します。このデフォルトでは SAM ルーターは次の状態になります。

- OCI セキュリティリストが `UDP/51820` を許可し、VNIC が `skipSourceDestCheck=true` であっても、WireGuard ハンドシェイクを受信**できません**。ホストファイアウォールがインバウンドの WireGuard パケットを `wg-hybrid` リスナーに到達する前にドロップします。
- 捕捉やオーバーレイトラフィックを転送**できません**。デフォルトの `FORWARD` reject が VNIC インターフェースと `wg-hybrid` 間の SAM 配送パスをブロックします。

これはクラウドセキュリティリスト / VNIC の source-dest-check とは独立しています。それらはファブリック層で動作します。**ゲスト OS ファイアウォール**は別レイヤーであり、SAM パスを個別に許可する必要があります。

## 必要な許可（ゲスト OS）

各 OCI SAM ルーターで、ホストファイアウォールは以下を許可している必要があります:

- `wg-hybrid` WireGuard リスナーへの**インバウンド `UDP/51820`**。
- OCI VNIC インターフェース（例: `ens3`）と `wg-hybrid` の間の双方向 **`FORWARD`**。

`WireGuardInterface.spec.listenPort` は Linux 上では routerd の所有範囲です。`WireGuardInterface` controller はその UDP port への `INPUT` accept rule を保証し、結果を `WireGuardInterface.status.hostFirewall` に出します。

転送許可はパスごとに扱います。管理対象の capture path では、`RemoteAddressClaim` が必要な capture interface から tunnel への `FORWARD` 許可を所有します。CloudEdge SAM の全経路がクリーンな OCI ホストで green になるまでは、イメージ由来の reject-all `FORWARD` がサイレントな dataplane failure にならないよう、`routerctl doctor hybrid` を acceptance gate に残してください。

## 診断方法

`routerctl doctor hybrid` は、WireGuard や SAM パスをブロックするゲストファイアウォールの reject-all `FORWARD` と `INPUT` のパターンを検出します。許可漏れがサイレントな「ハンドシェイクなし」ではなくレポートとして表示されます。`routerctl describe WireGuardInterface/<name>` でも、listen port の許可が `status.hostFirewall` に反映されたか確認できます。デプロイ後に OCI ルーターで実行してください。

```
routerctl doctor hybrid
routerctl describe WireGuardInterface/wg-hybrid
```

WireGuard エンドポイントがハンドシェイクを示さないのにピアが keepalive を送信している場合は、まずゲストファイアウォール（この How-to）を確認し、次に OCI セキュリティリスト、次に VNIC の source-dest-check を確認してください。

## 関連項目

- [Selective Address Mobility](../reference/selective-address-mobility)
- OCI Ubuntu イメージはデフォルトの `iptables-nft` 設定が AWS や Azure のイメージと異なります。AWS と Azure の SAM スモークでこの問題が発生しなかったのは、それらのイメージがデフォルトで `FORWARD` を reject-all しないためです。
