# ADR 0012: BGP /32 アドレスモビリティ

![ADR 0012 の図。リースと epoch 所有権を BGP best-path /32 アドバタイズメントに置き換え、活性マーカー、Route Reflector パス、FIB インポート、バックグラウンドプロバイダー捕捉まで](/img/diagrams/adr-0012-bgp-address-mobility.png)

## ステータス

承認済み。Phase 1 の Clean Option B を B6/B7 まで実装 — 2026-06-03。

[ADR 0006](../adr/0006-event-federation.md)、
[ADR 0008](../adr/0008-capture-coordination-fencing.md)、
[ADR 0010](../adr/0010-capture-ownership-arbitration.md)、
[ADR 0011](../adr/0011-generalized-failover.md) が CloudEdge モビリティデータプレーンに
導入したカスタムオーバーレイ到達性の正本を置き換える。旧来のプロバイダーアクション、VRRP、
doctor の安全機構はバックグラウンド reconciliation およびローカル捕捉ガードとして
スコープ内に残る。

## 背景

CloudEdge の Selective Address Mobility は元々、routerd 固有の制御プレーンから
オーバーレイ到達性を構築していた：

- Event Federation が observed/expired/heartbeat ファクトを運ぶ；
- mobility コントローラーがそれらのイベントを `AddressLease` 行に射影する；
- プランナーがリースを `AddressMobilityDomain`、`RemoteAddressClaim`、
  プロバイダー `ActionPlan`、`captureEpoch`、`ownershipEpoch` 状態に下降させる；
- SAM が生成された claim をルート、proxy-ARP、プロバイダー secondary-IP アクションに
  下降させる；
- プロバイダーアクションコントローラーがクラウド mutation を承認/実行する。

これはプロダクトパスを証明したが、フェイルオーバーを長い routerd 固有チェーンに
依存させることにもなった。ライブ 4-site テストでは、オーバーレイ/クラウドフェイルオーバーは
reconcile tick、リース/epoch 射影、アクションインポート/自動実行、プロバイダー API 動作、
クラウドファブリック伝搬に制約されたままだった。最近のスモーク結果では
AWS/OCI でクラウドフェイルオーバーに約 120 秒かかったが、目標は 60 秒以下、
できればオーバーレイトラフィックでは秒単位。

routerd は既に GoBGP ベースの `routerd-bgp` daemon と BGP コントローラーを出荷している。
既存のサーフェスで GoBGP の起動、ピアとポリシーの設定、`AddPath` による
静的 IPv4/IPv6 unicast プレフィックスのアドバタイズ、`DeletePath` による withdraw、
best path の観測/Linux IPv4 FIB へのインポートが可能。GoBGP v3.37.0 は
EVPN Type-2/Type-5 と MAC モビリティシーケンス番号もサポートするが、routerd の
現在の BGP リソースモデルと FIB syncer は IPv4/IPv6 unicast のみを公開している。
最速の有用なカットはプレーンな IPv4 unicast `/32` モビリティであり、EVPN ではない。

クラウドプロバイダーファブリックは別の制約。AWS VPC ルートテーブル、Azure UDR/Route Server、
OCI VCN ルートテーブルは、明示的なクラウドルーティング統合が設定されない限り、
VM のプライベート GoBGP オーバーレイアドバタイズメントに自動追従しない。
プロバイダーの secondary-IP 割り当て、ルートテーブルターゲット変更、
Azure Route Server 等のプロバイダーサービスは、クラウドネイティブ ingress に依然として
必要な場合がある。BGP はプロバイダー API 呼び出しをオーバーレイ到達性のクリティカルパスから
除去できるが、プロバイダー ingress の問題を削除するわけではない。

## 決定

CloudEdge モビリティの**オーバーレイ到達性の正本**を BGP RIB に移す：

- `MobilityPool` 内の各所有アドレスは IPv4 unicast `/32` BGP アドバタイズメントとして
  表現される。
- アドレスのオーナーは、その `/32` の BGP best-path 選択で勝つノード。
- 非オーナーは BGP best path からリモートの所有アドレスを学習し、生成された SAM 配送
  ルートではなく BGP FIB importer を通じてオーバーレイ配送ルートをインストールする。
- モビリティの移動は BGP withdraw/advertise とパス優先度の変更で表現される。
  オペレーターの意図は `MobilityPool` で宣言的なまま。オペレーターがリース、claim、
  プロバイダーアクションを手動記述する必要はない。
- best-path アービトレーションは標準の unicast 属性を優先使用：
  `LOCAL_PREF`/`MED`/communities + 決定的ルーターポリシー。観測性のために
  ルートシーケンスコミュニティを追加する可能性があるが、プレーン BGP は
  「新しいシーケンスが勝つ」をネイティブルールとして扱わない。
- EVPN は明示的に延期。EVPN Type-2 MAC/IP モビリティは将来の interop オプションであり、
  Phase 1 のメカニズムではない。

プロバイダーの secondary-IP とフォワーディングアクションは**バックグラウンド
reconciliation に降格**：

- VPC/VNet/VCN 経由でパケットが入るクラウドファブリック ingress パスには依然として必要。
  確立された routerd オーバーレイパスの代わりとして。
- 同じ BGP モビリティビューとプロバイダーインベントリ/アクションジャーナルから
  eventually reconcile される。
- オーバーレイ到達性の正本であってはならない。

オンプレミス LAN 捕捉はローカルのまま：

- VRRP マスターゲーティング、proxy-ARP、GARP、非マスターの fail-closed 動作、
  重複ホルダー doctor チェックは維持。
- BGP はリモートのオーバーレイ到達性を決定する。ローカル L2/ARP 権限ガードを
  置き換えるわけではない。

## Clean Option B の最終状態

プレリリース実装は BGP をモビリティの正本として直接使用する：

- **所有権:** モバイル `/32` のオーナーはそのプレフィックスの現在の BGP best path。
  別の `AddressLease`、ownership epoch、捕捉 epoch レジストリはない。
- **配送:** 非オーナーは BGP best path をローカル FIB にインポートし、
  `/32` をオーバーレイ next hop 経由でルーティングする。MobilityPool の
  route-mode プランニングと生成された SAM 配送 claim はメインラインの一部ではない。
- **捕捉/trap:** クラウドプロバイダー secondary-IP アクションは BGP best-path ビューと
  ローカルプレースメントから導出される。オーバーレイ到達性の前提条件ではなく、
  バックグラウンドのファブリック ingress reconciliation。
- **フェンシング:** プロバイダーアクションは現在のモビリティパスシグネチャ
  （`mobilityPathSig`）+ desired ホルダーと observed プロバイダー/ジャーナル遷移を持つ。
  stale アクションは desired BGP パスが一致しなくなったときにスキップされる。
  旧来の ownership/捕捉 epoch テーブルは削除済み。
- **活性:** モビリティフェイルオーバーは BGP withdrawal と best-path 収束に依存する。
  高速障害検出は FRR `bfdd` にレンダリングされる `BFD` リソースで提供。
  BGP hold タイマーは BFD が不安定なときのルート withdrawal の非破壊的権限として残る。
  カスタムモビリティハートビート/staleness 射影は削除済み。
- **オンプレミス LAN 権限:** VRRP マスターゲーティング、proxy-ARP、GARP、
  非マスター fail-closed 動作、重複 proxy-ARP doctor チェックはローカル安全機構として維持。
- **削除された状態:** B6 でモビリティリース、ownership epoch、捕捉 epoch、
  deprovision マーカーのテーブルと API を物理的に削除。そのステージで約 6,200 行を
  純減。

## 非ゴール

- Phase 1 で EVPN を実装しない。
- Phase 1 でプロバイダーエグゼキューターを削除しない。
- BGP だけでクラウドネイティブ ingress が解決されるとは主張しない。
- コンセンサス、etcd、Raft、単一ライターリースデータベースを追加しない。
- オペレーターに各アドレスの動的 BGP パスリソースの記述を要求しない。
- Event Federation をグローバルに削除しない。BGP パスが証明されてから
  モビリティ固有の使用のみを退役させる。

## モデル

意図する定常状態のマッピング：

| 既存の概念 | BGP モビリティの概念 |
| --- | --- |
| `AddressLease` アクティブオーナー | `pool/address/32` の BGP best path |
| observed オーナーイベント | ローカル `/32` advertise |
| expired/released イベント | ローカル `/32` withdraw |
| `staticOwnedAddresses` | 所有メンバーによる静的ローカル `/32` advertise |
| F3 ハンドオーバー | release/withdraw バリア、その後新オーナーが advertise |
| `RemoteAddressClaim` 配送ルート | インポートされた BGP `/32` FIB ルート |
| 捕捉プレースメントのアクティブメンバー | パス優先度 / origin 適格性 |
| オーバーレイルーティングの `ownershipEpoch`/`captureEpoch` | best-path ビューとオプションのルートメタデータ |
| プロバイダー secondary-IP アクション | バックグラウンドファブリック ingress reconciliation |
| オンプレミス proxy-ARP 権限 | 変更なしの VRRP マスターゲート |

## Phase 1 のスコープ

Phase 1 は BGP unicast パスを構築し、リリース前に置き換え済みのカスタム
モビリティプランナー/状態パスを削除した。

1. routerd が生成する `/32` アドバタイズメントにソース認識の動的 BGP パス管理を追加。
2. `MobilityPool` のオーナー状態を BGP アドバタイズメントに射影。
3. BGP best path をリモートアドレス配送ビューとして消費。
4. フェイルオーバーと静的ハンドオーバーのオーバーレイ到達性を BGP withdraw/advertise に移行。
5. プロバイダー secondary-IP 処理をバックグラウンド reconciliation に変換。
6. パリティ証明後、旧リース/プランナー/epoch パスを削除。

## 結論

正の影響：

- オーバーレイフェイルオーバーが routerd 固有のリース/アクション/プロバイダー直列ワークフローではなく、
  ルーティング収束の問題になる。
- 設計は BGP サービス VIP やポッド/サービスルートアドバタイズメント等の
  Kubernetes エッジパターンと整合する。
- 最も複雑なカスタム状態（`AddressLease` 射影、捕捉プレースメント、
  捕捉/ownership epoch プランニング、deprovision マーカー）を
  マイグレーション後に大幅に削減できる。
- D3/D5/D6/D7 のオーバーレイ到達性は、クラウドプロバイダー secondary-IP reconciliation が
  まだ保留中でも収束できる。

負の影響 / リスク：

- プレーン BGP は曖昧な同一プレフィックスアドバタイズメントを避けるために明示的なポリシーが必要。
  シーケンスコミュニティはネイティブフェンシングトークンではない。
- デプロイメントがクラウドルーティング統合も設定しない限り、バックグラウンドの
  プロバイダー状態が追いつくまでプロバイダーファブリック ingress は利用不可の場合がある。
- 既存のライブデモと acceptance プローブは、オーバーレイ到達性と
  クラウドネイティブ ingress を区別する必要がある。
- routerd の GoBGP 観測は現在ポールベース。Phase 1 でイベント駆動の `WatchEvent`
  パスを追加するか、BGP ルートインストールループにポールレイテンシが残る。
- スプリットブレイン防止は依然として VRRP/プロバイダーフェンシング/doctor チェックに依存。
  BGP best path は 1 つの転送パスを選ぶが、それだけでは stale なローカル proxy-ARP や
  stale なプロバイダー割り当てを除去しない。

## マイグレーションルール

- `MobilityPool` をオペレーターが記述する唯一のモビリティ意図として維持する。
- MobilityPool のデフォルト配送を BGP にする。旧 MobilityPool route-mode プランナーは
  マイグレーション支援であり、クリーンなプレリリース API では受け入れない。
- 決定的な優先順位ルールなしに、同一 `(pool, address)` に 2 つのルート下降ソースを
  同時に実行しない。
- 生成された BGP パスにソースメタデータをマークし、静的 BGP アドバタイズメントが
  モビリティ reconciliation によって誤って withdraw されないようにする。
- プロバイダー reconciliation が残っている間、プロバイダーアクションの冪等性と
  パスシグネチャフェンシングを維持する。

## 終了条件

- 4-site デモが BGP 学習済み `/32` オーバーレイルートを使って directed SSH マトリクスを通過。
- 協調ドレインと stale オーナーフェイルオーバーが、オーバーレイパスでの手動プロバイダーアクション
  承認/実行なしに BGP を通じて収束。
- プロバイダー secondary-IP アクションの遅延や失敗がオーバーレイ到達性を壊さない。
- VRRP/proxy-ARP オンプレミスの fail-closed セマンティクスが変更されていない。
- テストとライブエビデンスが BGP パスをカバーした後、旧モビリティリース/プランナーパスが削除済み。
