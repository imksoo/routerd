# FirewallRule

[リソース別の設定](../resources.md#firewallrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

ゾーン間の許可・破棄・拒否ルールです。アドレス・プロトコル・ポート条件と、Linuxのレート・接続数条件を指定できます。

## 指定条件と参照関係

- fromZone・toZone・actionが必須です。ゾーンは既存名またはselfです。fromZone=selfも検証を通りますが、この描画処理は自発通信のチェーンを生成せず、ローカル発信はOS・outputの方針に従います。
- ルール順はYAML順でなくmetadata.name順です。既存状態・IPv6制御・生成サービスや公開通信の許可が先に成立する場合があります。PFは広い暗黙許可も先に出し、転送ルールのtoZoneから出力インターフェース・宛先ゾーン条件を作らないため、宛先制限にはdestinationCIDRsを明示してください。
- portとdestinationPortsは排他です。送信元・宛先ポートの指定にはtcpまたはudpが必要です。範囲は一覧内の唯一の項目とし、数値ポート・start-end文字列を指定できます。ICMP型には対応するプロトコルが必要です。
- Linuxでは送信元CIDRはOR、destinationCIDRsとdestinationSetRefsは和集合で、除外集合をその後で除きます。送信元・宛先・プロトコルのアドレスファミリーは整合が必要です。PFは検証を通してもアドレス集合参照・rateLimit・connLimitを使用しません。
- LinuxのrateLimitは指定レート超過、connLimitはmaxPerSource超過に一致し、超過分へactionを適用します。制限には適切なdrop・rejectを指定してください。両方を設定すると別々の選択肢を生成し、両閾値のANDにはなりません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: "<name>"
  spec:
    action: "accept" # 必須; string; enum=["accept", "drop", "reject"]
    connLimit: # 任意; object
      log: true # 任意; boolean
      maxPerSource: 0 # 任意; integer; minimum=0
    destinationCIDRs: # 任意; array
      - "<string>" # string
    destinationPorts: # 任意; array
      - "<string>" # string
    destinationSetRefs: # 任意; array
      - "<string>" # string
    excludeDestinationSetRefs: # 任意; array
      - "<string>" # string
    fromZone: "<string>" # 必須; string
    icmpType: "<string>" # 任意; string
    icmpv6Type: "<string>" # 任意; string
    log: true # 任意; boolean
    port: 0 # 任意; integer; minimum=0; maximum=65535
    protocol: "" # 任意; string; enum=["", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip"]
    rateLimit: # 任意; object
      burst: 0 # 任意; integer; minimum=0
      log: true # 任意; boolean
      packetsPerSecond: 0 # 任意; integer; minimum=0
      per: "" # 任意; string; enum=["", "second", "minute"]
      rate: 0 # 任意; integer; minimum=0
      unit: "" # 任意; string; enum=["", "packet", "byte", "kilobyte", "megabyte"]
    sourcePorts: # 任意; array
      - "<string>" # string
    srcCIDRs: # 任意; array
      - "<string>" # string
    toZone: "<string>" # 必須; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.fromZone` | 必須の裸のFirewallZone名またはselfです。selfは受理しますが、ここでローカル発信のフィルターにはなりません。 |
| `spec.toZone` | 必須の裸のFirewallZone名またはselfです。Linuxは入出力ゾーンのインターフェース、selfはローカル入力を選びます。PFのselfはルーターアドレスで、他のtoZoneは出力ゾーン条件になりません。 |
| `spec.srcCIDRs` | 任意の送信元IPv4・IPv6プレフィックスのOR一覧です。既定の空は送信元不問で、解析可能なCIDRが必要です。Linuxはホスト部をマスクして描画します。 |
| `spec.destinationCIDRs` | 任意の宛先CIDR一覧で既定は空です。LinuxはdestinationSetRefsと合わせてORにし、両方なければゾーン経路内の宛先不問です。PFはこの静的CIDRだけを使います。 |
| `spec.destinationSetRefs` | 任意の既存IPAddressSet/<name>参照で、既定は空です。Linuxは有効なIPv4・IPv6集合を参照しますが、現行PFは適用しません。 |
| `spec.excludeDestinationSetRefs` | Linuxの宛先条件から除外する既存IPAddressSet/<name>参照です。既定は空で、現行PFは使いません。 |
| `spec.protocol` | 既定の空はプロトコル不問です。tcp・udp・icmp・icmpv6・ipv6-icmp・ipipを指定できます。ここでipipはIPv6 next-header 4のIPv4カプセル化であり、IPv4下位網のトンネル指定ではありません。 |
| `spec.port` | 任意の単一宛先ポートで、既定0は指定なしです。1～65535にはtcp・udpが必要で、destinationPortsと排他です。 |
| `spec.sourcePorts` | 任意のtcp・udp送信元ポートOR一覧です。各値は1～65535か唯一のstart-end範囲とし、既定の空はポート不問です。 |
| `spec.destinationPorts` | 任意のtcp・udp宛先ポートOR一覧で、送信元と同じ数値・範囲制約です。0以外のportと排他で、既定の空は制限なしです。 |
| `spec.icmpType` | icmpが必要なIPv4 ICMP型です。echo-reply・destination-unreachable・source-quench・redirect・echo-request・router-advertisement・router-solicitation・time-exceeded・parameter-problem・timestamp-request・timestamp-replyに対応します。空は全型です。YAML別名icmp-typeより本キーを優先します。 |
| `spec.icmpv6Type` | icmpv6・ipv6-icmpが必要です。destination-unreachable・packet-too-big・time-exceeded・parameter-problem・echo-request/reply・router-solicit/advert・neighbor-solicit/advertと、そのnd-別名に対応します。空は全型で、YAML別名icmpv6-typeより本キーを優先します。 |
| `spec.action` | accept・drop・rejectが必須で既定値はありません。Linuxのレート・接続条件付きでは許容量でなく超過通信への判定です。 |
| `spec.log` | 既定falseです。trueで判定前にログを出し、Linuxはカーネルログまたは有効なFirewallEventLogのNFLOG、PFはlogを使います。 |
| `spec.rateLimit` | 任意のLinuxレート超過条件で、既定は無効です。現行PFは子項目を含め使いません。 |
| `spec.rateLimit.rate` | 0以上のレートで、既定0はpacketsPerSecondを使います。実効0は無効でunit・per・burst・logも空・0・falseが必要です。正のrateはpacketsPerSecondより優先します。 |
| `spec.rateLimit.unit` | 有効時の既定はpacketで、byte・kilobyte・megabyteも選べます。nftablesのレート・バースト単位で、正の実効レートが必要です。 |
| `spec.rateLimit.per` | 有効時の既定はsecondで、minuteも選べます。正の実効レートが必要です。 |
| `spec.rateLimit.log` | 既定falseです。trueでレート超過一致を判定前に記録し、正の実効レートが必要です。 |
| `spec.rateLimit.packetsPerSecond` | rate=0時に使う旧来の0以上のレートで、既定0です。名前にかかわらずunit・perを明示した場合はその単位に従います。 |
| `spec.rateLimit.burst` | 選択単位の0以上のバースト量です。既定0はburst句を省略しnftablesの挙動に従います。0以外には有効なレートが必要です。 |
| `spec.connLimit` | 任意のLinux送信元別接続数条件で、既定は無効です。rateLimitと独立し、現行PFは使いません。 |
| `spec.connLimit.maxPerSource` | 0以上の上限で既定0は無効です。正のNでは送信元アドレス別のct count>Nに一致しactionを適用します。IPv4・IPv6で別の計測を使います。 |
| `spec.connLimit.log` | 既定falseです。trueで接続数超過ログを加え、maxPerSource=0なら効果はありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `action`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)
