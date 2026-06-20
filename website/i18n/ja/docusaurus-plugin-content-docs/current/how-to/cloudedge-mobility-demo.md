---
title: CloudEdge Mobility デモ
---

# CloudEdge Mobility デモ

![4 サイト CloudEdge SAM モビリティデモの共有 /24 オーナー、MobilityPool、SAMTransportProfile IPIP トランスポート、BGP /32 配送、プロバイダーまたは proxy-ARP 捕捉の流れ](/img/diagrams/how-to-cloudedge-mobility-demo.png)

> 実験的機能（CloudEdge）。**Selective Address Mobility (SAM)** のラボデモです。on-prem、AWS、Azure、OCI が 1 つの論理 `/24` を共有し、各サイトが他のサイトが*所有する*アドレスを、NAT なしでクライアントのデフォルトゲートウェイも変更せずに提供できます。実行可能なパッケージは [`examples/cloudedge-mobility-demo/`](https://github.com/imksoo/routerd/tree/main/examples/cloudedge-mobility-demo) にあります。

![CloudEdge Mobility デモ: 4 サイトが 10.77.60.0/24 を共有し、各オーナーアドレスが生成された SAM トランスポートを介してすべての非オーナーサイトで捕捉され、NAT なしでソース IP が保持される](/img/diagrams/how-to-cloudedge-mobility-demo.png)

## このデモで示すこと

- **1 つの論理 `/24` を 4 サイトで共有。** on-prem、AWS、Azure、OCI のすべてが `10.77.60.0/24` を単一の論理アドレス空間として扱います。
- **非オーナーサイトがオーナーのアドレスを捕捉する。** 各オーナーアドレスが*他のすべてのサイト*で到達可能になります（クラウドではプロバイダーの**セカンダリ IP**、on-prem では**proxy ARP**）。単一オーナーへの調停が行われます。
- **12 方向の SSH フローがパス。** 4 つのデモクライアント間で全方向が通信可能です。
- **NAT なし、ソース IP 保持、ゲートウェイ変更なし。** 接続は実際のソース IP を維持し、NAT を行わず、クライアントのデフォルトゲートウェイに触れません。
- **クラウドメンテナンス捕捉マイグレーション（D5）。** 捕捉されたアドレスが同一プロバイダー内の別のルーターにスタンバイとして移動し、新しいホルダー経由でトラフィックが復旧します。

## アドレス設計

4 サイトすべてが 1 つの論理サブネットを共有し、各サイトはその中の `/32` を 1 つだけ所有します。

| サイト | routerd ノード | オーナーアドレス | 捕捉手段 |
| --- | --- | --- | --- |
| On-prem | `onprem-router` | `10.77.60.10/32` | LAN 上の Proxy ARP |
| AWS | `aws-router-a` | `10.77.60.11/32` | ENI セカンダリ IP |
| Azure | `azure-router` | `10.77.60.12/32` | NIC セカンダリ ipConfig |
| OCI | `oci-router` | `10.77.60.13/32` | VNIC セカンダリプライベート IP |

論理サブネットは **`10.77.60.0/24`** です。ルーター間トランスポートは別の RFC1918 エンドポイントと内部アドレス体系を使用します（リンクローカル `169.254/16` や CGNAT `100.64/10` とは分離）。アドレスの制約については [Selective Address Mobility](../reference/selective-address-mobility) を参照してください。

on-prem 側は `10.77.60.10/32` のみを `staticOwnedAddresses` で宣言します。
追加の on-prem クライアントを検知するには、on-prem メンバーで `ownershipDiscovery`
`mode: onprem-l2` と `ens21` の `arp-observer` ソースを設定してください。

## データプレーン

- **provider-secondary-ip 捕捉。** 各クラウドルーターで、*他の*サイトのオーナーアドレスがその ENI、NIC、VNIC にセカンダリ IP としてアタッチされ、クラウドファブリックがそのルーターにトラフィックを配送します。
- **proxy-ARP 捕捉。** on-prem では、ルーターが LAN 上で他サイトのオーナーアドレスに対する ARP に応答します。
- **BGP `/32` 配送。** 各オーナーが所有する `/32` を広告し、他のルーターがベストパスをインポートしてオーバーレイ経由で所有サイトのルーターに転送します。
- **生成された SAM トランスポート。** ルーターは `SAMTransportProfile` から導出された IPIP トンネルと BGP ピアで相互接続します。WireGuard が有効な場合はエンドポイント限定の暗号化アンダーレイです。その `AllowedIPs` にはトランスポートエンドポイントプレフィックスが含まれ、モバイル `/32` は含みません。

配送はルーティング（NAT ではない）のため、**ソース IP は保持**され、クライアントの**デフォルトゲートウェイは変更されません**。

## コントロールプレーン

運用者が宣言するのは意図のみで、それ以外はすべて導出されます。

- **MobilityPool。** 運用者が記述する唯一の意図（メンバー、捕捉モード、配送、配置、メンテナンスドレイン）。
- **目標メンバー構造。** 各レンダリング設定は `profiles.cloudCaptures`、`spec.values`、`targetFrom`、`subnetRefFrom` で自サイトを完全に宣言し、リモートサイトは識別情報のみのピアエントリです。BGP と同様に、ノードはピアを知る必要がありますが、ピアのプロバイダー NIC やサブネット実装の詳細は不要です。
- **SAMTransportProfile。** 共有トポロジと内部プレフィックスからピアごとの `TunnelInterface`、エンドポイント `/32` の `IPv4Route`、`BGPPeer` リソースを導出します。
- **BGP `/32` モビリティパス。** 各オーナーが所有するホストルートを広告し、他サイトが生成された SAM トランスポート上で現在のベストパスを学習します。
- **プロバイダー trap アクション。** クラウドルーターはリモート所有の `/32` をローカルトラッピング用にセカンダリ IP として最終的に assign または unassign します。これらのアクションはクリティカルな転送パス上にはもうありません。
- **Event Federation。** `routerd.client.ipv4.observed` ファクトがサイト間で伝播します（`EventGroup`、`EventPeer`、`EventSubscription` を使用。[Event Federation](../reference/event-federation.md) 参照）。
- **プロバイダーアクションエグゼキューター。** `ProviderActionPolicy` の下で、インスタンス自身のクラウドネイティブ識別情報を使用して、ゲート付きクラウド変更（セカンダリ IP の assign と unassign、forwarding）を実行します（[ADR 0007](../adr/0007-provider-action-execution.md) 参照）。
- **pathSig フェンシング。** プロバイダーアクションは現在の BGP 希望パスシグネチャとホルダーに対してフェンスされるため、古いアクションが他で再収束したルートを変更することはできません。

サンプル設定は、旧来の remote-full インラインスタイルを意図的に避けています。プレリリース期間中は旧スタイルもまだ受け付けますが、リモートの `MobilityPool` メンバーにローカルプロバイダーの捕捉やディスカバリーの詳細が含まれている場合、`routerctl validate`、plan、apply は警告を出します。将来のプレリリース設定では、リモートメンバーは識別情報のみが必須となる可能性があります。

## 実行方法

`examples/cloudedge-mobility-demo/` のパッケージを使用します。ラボインスタンス、NIC と VNIC、識別情報の権限、SSH、オプションの WireGuard エンドポイントキー、プロバイダー CLI がすでに準備されていることを前提とします。スクリプトはクラウドリソースをプロビジョニング**しません**。

```sh
cd examples/cloudedge-mobility-demo
cp env.example env
$EDITOR env            # すべてのプレースホルダーを記入。秘密情報は git に入れない

./run-demo.sh          # レンダー + デプロイ、イベント発行、D3 実行、その後 D5 マイグレーション
./collect-evidence.sh  # プロバイダー状態、ジャーナル、接続情報を収集
./reset-lab.sh         # ベストエフォートの teardown。アイドルコスト回避のためコンピュートを停止
```

失敗時も含め、毎回のラン後に `reset-lab.sh` を実行してください。

## 検証済みの結果

- **D1** ロケーション自動反映: on-prem に出現したオーナーアドレスを各クラウドルーターが認識。
- **D2** クラウド → on-prem 捕捉（proxy ARP）。
- **D3** 4 サイト **12 方向 ping と SSH が PASS**。ソース IP 保持、NAT なし、デフォルトゲートウェイ変更なし。
- **D4** on-prem HA と VRRP の捕捉フェイルオーバー。
- **D5** クラウドメンテナンスと**捕捉マイグレーション PASS**。`aws-router-a` をドレインすると、捕捉されたアドレスが `aws-router-b` に移動し、B 経由でトラフィックが復旧。古い pathSig アクションはフェンスされます（`skipped: stale mobility desired path`）。[D5 エビデンス](../releases/evidence/cloudedge-mobility-d5-aws-maintenance-20260531.md)を参照。

## 注意事項

- これは**ラボデモ**であり、本番用のターンキーソリューションではありません。
- 完全な L2 拡張や EVPN では**ありません**。ブロードキャストやマルチキャストのブリッジングはありません。
- **選択的な `/32` アドレスモビリティ**です。サブネット全体ではなく、選択されたアドレスがサイト間でモバイルになります。
- スクリプトは事前プロビジョニング済みのインスタンスを前提とし、プレースホルダーの非秘密論理アドレスを使用します。実際のアカウント、サブスクリプション、OCID、ENI や VNIC の ID、秘密鍵は決してコミットしないでください。
