# FirewallEventLog

[リソース別の設定](../resources.md#firewalleventlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

ファイアウォールのパケットログ収集と、保存DB・Linux NFLOGグループ・描画時のログ設定を指定します。受理される項目の一部は、現在の実行処理で使われません。

## 指定条件と参照関係

- enabledの既定はfalseです。LinuxはNFLOG、FreeBSDはpflog0から収集します。これだけで全許可・拒否ルールを記録するわけではなく、FirewallRule.logとFirewallPolicy.logDenyも記録対象を決めます。
- 有効なリソースは1件にしてください。描画設定は最初の有効リソースを使い、サービス生成は共通のrouterd-firewall-logger名を使います。名前を増やしても独立した収集プロセスにはなりません。
- fromZones・toZonesは既存FirewallZone名またはself、rulesはFirewallRule/nameまたは裸のルール名、sinksはLogSink/nameまたは裸の出力先名、retentionはLogRetention/nameまたは裸の名前です。これらは実行時の絞り込み・転送・保持ポリシー選択に使われませんが、参照検証は行います。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # 任意; boolean
    events: # 任意; array
      - "deny" # string; enum=["deny", "allow", "rateLimit", "connLimit"]
    fromZones: # 任意; array
      - "<string>" # string
    log: # 任意; object
      acceptSampleRate: 0 # 任意; integer; minimum=0
      copyRange: 0 # 任意; integer; minimum=0
      dropSampleRate: 0 # 任意; integer; minimum=0
      reject: true # 任意; boolean
    nflogGroup: 0 # 任意; integer; minimum=0; maximum=65535
    path: "<string>" # 任意; string
    retention: "<string>" # 任意; string
    rules: # 任意; array
      - "<string>" # string
    sampleRate: 0 # 任意; integer; minimum=0
    sinks: # 任意; array
      - "<string>" # string
    toZones: # 任意; array
      - "<string>" # string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.enabled` | 既定falseです。trueで収集サービスとグループログ設定を有効にし、現行のサービス描画ではDPI分類ヘルパーも要求します。 |
| `spec.path` | SQLite DBのパスです。既定はLinuxで/var/lib/routerd/firewall-logs.db、FreeBSDで/var/db/routerd/firewall-logs.dbです。JSON行形式の出力ファイルではありません。 |
| `spec.events` | deny・allow・rateLimit・connLimitを受け付け、既定は空です。検証のみで、現在の収集入力・生成ログの絞り込みには使いません。 |
| `spec.fromZones` | 既定は空です。送信元としてFirewallZone名またはselfを検証しますが、実行時のログ絞り込みには使いません。 |
| `spec.fromZones[]` | 既存FirewallZoneのmetadata.nameまたは特別なselfです。Kind/name形式ではなく、現在の絞り込み効果はありません。 |
| `spec.toZones` | 既定は空です。宛先としてFirewallZone名またはselfを検証しますが、実行時のログ絞り込みには使いません。 |
| `spec.rules` | 既定は空です。指定時は既存FirewallRuleの裸の名前またはFirewallRule/nameが必要ですが、現在の記録ルール選択には使いません。対象ルールのFirewallRule.logを設定してください。 |
| `spec.sampleRate` | 0以上の整数で既定は0ですが、収集・描画は使用しません。対応する描画制御にはlog.acceptSampleRate・log.dropSampleRateを使います。 |
| `spec.sinks` | 既定は空です。既存LogSinkへの参照は検証しますが、収集処理は転送しません。出力先はpathのDBです。 |
| `spec.retention` | 任意の既存LogRetention参照で、既定は空です。現在の保持ポリシー選択には使いません。本DBの保持処理にはLogRetention側でfirewallEventsを指定してください。 |
| `spec.nflogGroup` | 0～65535の整数です。Linuxでは省略・0をグループ1にし、nftablesと収集で合わせます。FreeBSDはpflog0を使い、このグループを使いません。 |
| `spec.log` | 任意の描画ログ設定です。省略項目は0・falseになります。未使用のトップレベルのイベント・ゾーン・ルール絞り込みを実現する項目ではありません。 |
| `spec.log.acceptSampleRate` | 既定0は暗黙許可の追加ログを無効にします。nftablesは正のNで対象パケットの約1/Nを記録し、1はすべてです。PFは正数を有効化の切替としてのみ使い、1/Nの間引きはしません。明示的なFirewallRule.logとは別です。 |
| `spec.log.dropSampleRate` | 既定0です。nftablesは既に有効な暗黙拒否ログに対し0・1で全記録、N>1で約1/Nを記録します。この値自体ではFirewallPolicy.logDenyを有効にしません。PFは使いません。 |
| `spec.log.copyRange` | 0以上のNFLOGコピー長（バイト）です。既定0はnftablesのsnaplen引数を省略し、正数でコピー長を制限します。PF描画では使いません。 |
| `spec.log.reject` | 既定falseですが、現行の描画・収集は読みません。ルールの判定変更やrejectログの有効化にはなりません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/render/firewall_logger.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_logger.go) · [pkg/render/freebsd_rcd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd_rcd.go) · [pkg/render/dpi_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dpi_systemd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)
