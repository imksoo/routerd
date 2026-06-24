---
title: ハイブリッドクラウドエッジ設計
---

# ハイブリッドクラウドエッジ設計

routerd の CloudEdge は、人が管理する startup YAML をクラウド自動化が直接編集せずに、
ローカルエッジとクラウド側の事実を同じ宣言的ルーターモデルに載せる設計です。
現在の実装には、動的設定、プラグイン I/O、BGP モードの選択的アドレス移動性、
SAM トランスポートリソースの自動生成、ゲート付きプロバイダーアクション実行が含まれます。

この MVP では、CloudEdge に 2 つの宣言的な柱があります。

- L3 ハイブリッドルーティング。`HybridRoute` はリモートの IPv4 プレフィクスを `OverlayPeer` 経由で lowering します。
- 選択的アドレス移動性。選択した `/32` IPv4 アドレスをローカルで捕捉し、L2 を延伸せずに routerd 間のオーバーレイで所有側に配送します。

このモデルは [アーキテクチャ概要](./design) の既存アーキテクチャを拡張します。コントローラーは引き続き 1 つの desired 設定をリコンサイルし、リソースは同じ `apiVersion`、`kind`、`metadata.name`、`spec`、`status` の形を使い、状態データベースが生成された状態のランタイム記録になります。

![MobilityPool と SAMTransportProfile から DynamicConfigPart、IPIP TunnelInterface 配送、BGP ピア、ECMP 対応 FIB パス、エンドポイント専用 WireGuard アンダーレイへつながる CloudEdge SAM 図](/img/diagrams/cloudedge-sam-ipip.png)

## 目標

CloudEdge は、ルーターが信頼できるローカルソースからランタイムの意図を受け取る必要がある、クラウドとオンプレミスの混在環境を対象としています。

- startup YAML の変更よりも速く変化するクラウドインベントリ
- 選択的なアドレス要求、ルートヒント、VPN 接続の観測
- 運用者がゲート付きエグゼキューターパスでプロバイダー側の変更をインポート・実行するか判断する前に、plan/dry-run で確認できるプロバイダーアクション
- 動的なクラウドリソースが正常な間、静的フォールバックリソースを選択的に抑制すること

姿勢は保守的です。動的な入力はリソースと `mask` ディレクティブを実効設定に寄与できますが、startup ファイルを変更することはできません。プロバイダーアクションの実行は、デフォルト無効のジャーナル付き別経路で、明示的なポリシーゲートを必要とします。

## 設定レイヤー

CloudEdge は 3 つの明示的な設定レイヤーを導入します。

### startup-config

startup-config は、通常の routerd 設定パス（多くの場合 `/usr/local/etc/routerd/router.yaml`）から読み込む、人が管理する YAML です。運用者はこれを git で管理し、ソースコードと同様にレビューし、既存の validate、plan、dry-run、apply のフローで適用してください。

プラグインは startup-config を編集してはいけません。プラグインは startup のハッシュを観測して動的な意図を発行できますが、ソースファイル内のリソースの書き換え・並べ替え・削除はできません。静的フォールバックルート、緊急管理アクセス、その他の運用者所有の安全リソースはここに置きます。

### dynamic-config

dynamic-config は、信頼できるローカルソースが生成するランタイムの意図です。MVP では主なソースは、プラットフォームのプラグインディレクトリにインストールされたローカルプラグインです。例:

```text
/usr/local/libexec/routerd/plugins/<name>/
```

各プラグインの結果は検証され、`DynamicConfigPart` として状態データベースに保存されます。パートにはソース、世代番号、observedAt タイムスタンプ、expiresAt タイムスタンプ、ダイジェスト、リソース/ディレクティブがあります。プラグインは `PluginResult` で TTL を返し、routerd はその期間をパートの `expiresAt` に解決します。期限切れのパートは effective-config の導出時に無視されます。

dynamic-config はホストの状態ではなく、命令的なコマンドキューでもありません。startup 設定と同じリソースモデルに参加する、ランタイムの desired な意図です。

### effective-config

effective-config が唯一のリコンサイル対象です。

```text
effective-config = startup-config + 有効な動的パート - 有効なマスク
```

コントローラー、レンダラー、dry-run、plan、status、将来の CloudEdge 画面はすべて effective-config を基準にします。有効なマスクで抑制された startup リソースは startup ファイルから削除されず、有効なリコンサイル対象から除外され、理由を説明するメタデータ付きで「抑制中」と報告されます。

## 設計原則

- startup-config は routerd プラグインから不変。運用者が所有する。
- dynamic-config はランタイムの意図であり、OS への直接変更パスではない。
- effective-config が唯一のリコンサイル対象。
- 動的リソースは startup リソースをその場で上書きしない。
- マスクは一致する startup リソースを抑制するが、削除はしない。
- すべての動的変更は、ソース、世代番号、ダイジェスト、観測時刻、有効期限、ディレクティブの理由で説明できなければならない。
- プラグイン出力は dynamic-config になる前に必ず検証される。
- プロバイダーアクションプランは dynamic-config の中では不活性。アクションジャーナルにインポートし、明示的な `ProviderActionPolicy`、承認、許可リスト、エグゼキュータープラグインのゲートを通って初めて実行できる。

## 現在のスコープ

CloudEdge の基盤には現在、次が含まれます。

- `DynamicConfigPart` / `DynamicOverridePolicy` によるランタイムの意図とマスク。
- 信頼できるローカルプラグインによる観測、動的リソース、プロバイダーアクション提案、エグゼキュータープラグインの実行。
- `MobilityPool` を中心とした選択的アドレス移動性の意図。
- `SAMTransportProfile` を主な transport 記述面とした、IPIP/GRE の `TunnelInterface`、エンドポイント `/32` の `IPv4Route`、`BGPPeer` リソースの自動生成。
- BGP モード SAM 配送。所有者は IPv4 `/32` パスを広告し、非所有者は BGP の最良経路をローカル FIB にインポートします。マルチパスは BGP が複数のネクストホップを提供する場合に維持されます。
- Linux SAM 捕捉。プロバイダー secondary IP と proxy-ARP のケースに対応し、オンプレミスの VRRP/単一ルーターゲート、active 遷移時の GARP、保守的なオンデマンド ARP 探索を含みます。
- 実験的・デフォルト無効のプロバイダーアクション実行。アクションプランはインポートされ、ポリシー、承認、エグゼキュータープラグインでゲートされるまではレビュー用の成果物にとどまります。

スコープ外:

- リモートプラグインのインストールや公開プラグインレジストリ
- 合意ベースのグローバル所有権や split-brain 防止
- CloudEdge SAM を完全な L2 延伸として扱うこと
- 任意のプロバイダーや OS の変更の自動ロールバック

## L3 ハイブリッドルーティング

`HybridRoute` は保守的な L3 の柱です。`OverlayPeer` 経由で lowering すべきリモートの非デフォルト IPv4 プレフィクスを表します。lowering と status のパスは意図的に明示的です。デフォルトルートは拒否され、運用者は通常の routerctl plan と dry-run のフローで生成されたルートの意図をレビューできます。

## 選択的アドレス移動性

選択的アドレス移動性は CloudEdge の 2 つ目の柱です。完全な L2 延伸ではありません。パブリッククラウドのファブリックは運用者が制御する Ethernet セグメントを公開せず、プロバイダーのアドレス所有モデルも異なります。そのため routerd は選択した移動可能な `/32` IPv4 アドレスだけをモデル化します。

現在の主な記述モデル:

- `MobilityPool` は、移動性プレフィクス、フェデレーショングループ、メンバーの識別情報、サイトのロール、捕捉ポリシー、プロバイダートラップの配置、BGP 配送ポリシーを宣言します。
- `SAMTransportProfile` は、ルーター間の transport を宣言します。自ノード、共有トポロジのノードリスト、内部プレフィクス、IPIP/GRE モード、オプションの WireGuard 暗号化アンダーレイ、BGP ルーター、ピアを含みます。
- `CloudProviderProfile` は、プロバイダーの能力と外部認証の形を記述します。
- `ProviderActionPolicy` は、インポートされたプロバイダーアクションプランをエグゼキュータープラグインに渡せるかどうかを制御します。

低レベルの `AddressMobilityDomain` と `RemoteAddressClaim` リソースは互換性と実験のために残っていますが、CloudEdge SAM の主な記述面ではなくなりました。

選択的アドレス移動性は通常のスイッチング/フォワーディングプレーンに存在し、ファイアウォールや NAT の概念を含みません。送信元と宛先の透過性は固有であり、設定可能なフィールドではありません。運用者はファイアウォールと NAT ポリシーを、既存の `FirewallZone`、`FirewallRule`、`NAT44Rule` リソースでリテラルアドレスを参照して別途構成します。

詳細は [選択的アドレス移動性](./reference/selective-address-mobility) を参照してください。

## クラウドインベントリとプロバイダーアクション

クラウドインベントリプラグインは、プロバイダーの状態を観測し、プロバイダーを変更せずに動的リソースを返せます。プロバイダー捕捉プランナーは `assign-secondary-ip`、`unassign-secondary-ip`、`ensure-forwarding-enabled`、`ensure-forwarding-disabled` などの `actionPlans` も発行できます。

`actionPlan` は effective-config にマージされず、dynamic-config コントローラーからも実行されません。レビュー可能な状態として永続化されます。運用者はこれをプロバイダーアクションジャーナルにインポートし、`routerctl action` コマンドを実行できます。実際の変更は引き続きデフォルト無効で、すべてのハードゲートが必要です。具体的には、`ProviderActionPolicy.enabled`、`dryRunOnly` でないこと、承認または明示的な自動承認ポリシー、プロバイダー/アクション/CIDR の許可リスト、正の `maxActionsPerRun`、`execute.providerAction` を持つエグゼキュータープラグインです。

routerd コアはクラウドの認証情報を保持しません。エグゼキュータープラグインは独自のプロセスとして動作し、クラウドネイティブの識別情報か独自の環境で認証します。

## ロードマップ

今後の CloudEdge の作業も、検証後は通常の effective-config リソースを使い続けるべきです。これにより、既存のルート、ファイアウォール、NAT、所有権、GC、観測性のフローが、別のクラウド制御パスなしでそれらを消費できます。残りの設計領域には、より広いプロバイダー対応、運用証跡の自動化、transport 導出のユーザビリティ向上、split-brain 観測性の本番強化があります。

完全な L2 延伸は引き続きスコープ外です。VXLAN、EVPN、VRF、WireGuard、IPsec の基盤はすでにありますが、CloudEdge はリモートの障害ドメインをブリッジする前に、L3 到達性と明示的なアドレス単位の移動性を優先すべきです。完全な L2 の設計には、ループ回避、障害分離、MTU、ブロードキャスト封じ込め、運用ロールバックに関する別途の安全性検討が必要です。

[動的設定リファレンス](./reference/dynamic-config.md) と [プラグインプロトコル](/docs/reference/plugin-protocol) も参照してください。
