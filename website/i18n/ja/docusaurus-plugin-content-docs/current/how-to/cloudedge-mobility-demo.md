---
title: CloudEdge Mobility デモ
---

# CloudEdge Mobility デモ

![4 サイト CloudEdge SAM mobility デモの共有 /24 オーナー、MobilityPool、SAMTransportProfile IPIP トランスポート、BGP /32 デリバリー、プロバイダーまたは proxy-ARP キャプチャの流れ](/img/diagrams/how-to-cloudedge-mobility-demo.png)

> 実験的機能（CloudEdge）。**Selective Address Mobility (SAM)** のラボデモです。on-prem、AWS、Azure、OCI が 1 つの論理 `/24` を共有し、各サイトが他のサイトが*所有する*アドレスを — NAT なしでクライアントのデフォルトゲートウェイも変更せずに — 提供できます。実行可能なパッケージは [`examples/cloudedge-mobility-demo/`](https://github.com/imksoo/routerd/tree/main/examples/cloudedge-mobility-demo) にあります。

![CloudEdge Mobility デモ: 4 サイトが 10.77.60.0/24 を共有し、各オーナーアドレスが生成された SAM トランスポートを介してすべての非オーナーサイトでキャプチャされ、NAT なしでソース IP が保持される](../images/cloudedge-mobility-demo.png)

## このデモで示すこと

- **1 つの論理 `/24` を 4 サイトで共有** — on-prem / AWS / Azure / OCI のすべてが `10.77.60.0/24` を単一の論理アドレス空間として扱います。
- **非オーナーサイトがオーナーのアドレスをキャプチャする** — 各オーナーアドレスが*他のすべてのサイト*で到達可能になります（クラウド: プロバイダーの**セカンダリ IP**、on-prem: **proxy ARP**）。単一オーナーへのアービトレーションが行われます。
- **12 方向の SSH フローがパス** — 4 つのデモクライアント間で全方向が通信可能です。
- **NAT なし、ソース IP 保持、ゲートウェイ変更なし** — 接続は実際のソース IP を維持し、NAT を行わず、クライアントのデフォルトゲートウェイに触れません。
- **クラウドメンテナンスキャプチャマイグレーション（D5）** — キャプチャされたアドレスが同一プロバイダー内の別のルーターにスタンバイとして移動し、新しいホルダー経由でトラフィックが復旧します。

## アドレス設計

4 サイトすべてが 1 つの論理サブネットを共有し、各サイトはその中の `/32` を 1 つだけ所有します。

| サイト | routerd ノード | オーナーアドレス | キャプチャメカニズム |
| --- | --- | --- | --- |
| On-prem | `onprem-router` | `10.77.60.10/32` | LAN 上の Proxy ARP |
| AWS | `aws-router-a` | `10.77.60.11/32` | ENI セカンダリ IP |
| Azure | `azure-router` | `10.77.60.12/32` | NIC セカンダリ ipConfig |
| OCI | `oci-router` | `10.77.60.13/32` | VNIC セカンダリプライベート IP |

論理サブネット: **`10.77.60.0/24`**。ルーター間トランスポートは別の RFC1918 エンドポイント/内部アドレス体系を使用します（リンクローカル `169.254/16` や CGNAT `100.64/10` とは分離）。アドレスの制約については [Selective Address Mobility](../reference/selective-address-mobility) を参照してください。

## データプレーン

- **provider-secondary-ip キャプチャ** — 各クラウドルーターで、*他の*サイトのオーナーアドレスがその ENI / NIC / VNIC にセカンダリ IP としてアタッチされ、クラウドファブリックがそのルーターにトラフィックを配送します。
- **proxy-ARP キャプチャ** — on-prem では、ルーターが LAN 上で他サイトのオーナーアドレスに対する ARP に応答します。
- **BGP `/32` デリバリー** — 各オーナーが所有する `/32` を広告し、他のルーターがベストパスをインポートしてオーバーレイ経由で所有サイトのルーターに転送します。
- **生成された SAM トランスポート** — ルーターは `SAMTransportProfile` から導出された IPIP トンネルと BGP ピアで相互接続します。WireGuard が有効な場合はエンドポイント限定の暗号化アンダーレイです。その `AllowedIPs` にはトランスポートエンドポイントプレフィックスが含まれ、モバイル `/32` は含みません。

デリバリーはルーティング（NAT ではない）のため、**ソース IP は保持**され、クライアントの**デフォルトゲートウェイは変更されません**。

## コントロールプレーン

オペレーターが宣言するのはインテントのみで、それ以外はすべて導出されます。

- **MobilityPool** — オペレーターが記述する唯一のインテント（メンバー、キャプチャモード、デリバリー、配置、メンテナンスドレイン）。
- **ノーススター メンバー構造** — 各レンダリング設定は `profiles.cloudCaptures`、`spec.values`、`targetFrom`、`subnetRefFrom` で自サイトを完全に宣言し、リモートサイトは ID のみのピアエントリです。BGP と同様に、ノードはピアを知る必要がありますが、ピアのプロバイダー NIC/サブネット実装の詳細は不要です。
- **SAMTransportProfile** — 共有トポロジと内部プレフィックスからピアごとの `TunnelInterface`、エンドポイント `/32` `IPv4Route`、`BGPPeer` リソースを導出します。
- **BGP `/32` mobility パス** — 各オーナーが所有するホストルートを広告し、他サイトが生成された SAM トランスポート上で現在のベストパスを学習します。
- **プロバイダー trap アクション** — クラウドルーターはリモート所有の `/32` をローカルトラッピング用にセカンダリ IP として最終的に assign/unassign します。これらのアクションはクリティカルな転送パス上にはもうありません。
- **Event Federation** — `routerd.client.ipv4.observed` ファクトがサイト間で伝播します（`EventGroup` / `EventPeer` / `EventSubscription`、[Event Federation](../reference/event-federation.md) 参照）。
- **プロバイダーアクション executor** — `ProviderActionPolicy` の下で、インスタンス自身のクラウドネイティブ ID を使用して、ゲート付きクラウドミューテーション（セカンダリ IP の assign / unassign、forwarding）を実行します（[ADR 0007](../adr/0007-provider-action-execution.md) 参照）。
- **pathSig フェンシング** — プロバイダーアクションは現在の BGP 希望パスシグネチャとホルダーに対してフェンスされるため、古いアクションが他で再収束したルートを変更することはできません。

サンプル設定は、旧来の remote-full インラインスタイルを意図的に避けています。プレリリース期間中は旧スタイルもまだ受け付けますが、リモートの `MobilityPool` メンバーにローカルプロバイダーのキャプチャやディスカバリの詳細が含まれている場合、`routerctl validate`、plan、apply は警告を出します。将来のプレリリース設定では、リモートメンバーは ID のみが必須となる可能性があります。

## 実行方法

`examples/cloudedge-mobility-demo/` のパッケージを使用します。ラボインスタンス、NIC/VNIC、ID 権限、SSH、オプションの WireGuard エンドポイントキー、プロバイダー CLI がすでに準備されていることを前提とします — スクリプトはクラウドリソースをプロビジョニング**しません**。

```sh
cd examples/cloudedge-mobility-demo
cp env.example env
$EDITOR env            # すべてのプレースホルダーを記入。シークレットは git に入れない

./run-demo.sh          # レンダー + デプロイ、イベント発行、D3 実行、その後 D5 マイグレーション
./collect-evidence.sh  # プロバイダー状態、ジャーナル、接続情報を収集
./reset-lab.sh         # ベストエフォートの teardown。アイドルコスト回避のためコンピュートを停止
```

失敗時も含め、毎回のラン後に `reset-lab.sh` を実行してください。

## 検証済みの結果

- **D1** ロケーション自動反映: on-prem に出現したオーナーアドレスを各クラウドルーターが認識。
- **D2** クラウド → on-prem キャプチャ（proxy ARP）。
- **D3** 4 サイト **12 方向 ping + SSH PASS** — ソース IP 保持、NAT なし、デフォルトゲートウェイ変更なし。
- **D4** on-prem HA / VRRP キャプチャフェイルオーバー。
- **D5** クラウドメンテナンス / **キャプチャマイグレーション PASS** — `aws-router-a` をドレインすると、キャプチャされたアドレスが `aws-router-b` に移動し、B 経由でトラフィックが復旧。古い pathSig アクションはフェンスされます（`skipped: stale mobility desired path`）。[D5 エビデンス](../releases/evidence/cloudedge-mobility-d5-aws-maintenance-20260531.md)を参照。

## 注意事項

- これは**ラボデモ**であり、本番用のターンキーソリューションではありません。
- 完全な L2 拡張 / EVPN では**ありません** — ブロードキャスト/マルチキャストブリッジングはありません。
- **選択的な `/32` アドレスモビリティ**です: サブネット全体ではなく、選択されたアドレスがサイト間でモバイルになります。
- スクリプトは事前プロビジョニング済みのインスタンスを前提とし、プレースホルダーの非秘密論理アドレスを使用します。実際のアカウント/サブスクリプション/OCID/ENI/VNIC ID や秘密鍵は決してコミットしないでください。
