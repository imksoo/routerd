# DNSForwarder

[リソース別の設定](../resources.md#dnsforwarder) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

1つのDNSResolverに対し、問い合わせ名に応じてローカルゾーンまたは上流DNSを選ぶ規則です。

## 指定条件と参照関係

- resolverと空でないmatchに加え、zoneRefsとupstreamsの片方だけに1件以上必要です。参照先は同じRouter内に必要で、裸名または対応するKind/nameを使用します。
- 待受の絞り込み後、Router内のリソース順に評価します。最長サフィックス順ではないため、個別・内部向け規則を全体規則より前に置きます。一致した上流の失敗はSERVFAILとなり、別規則への転送はしません。
- 複数の上流参照は1つのプールに結合します。bootstrapは結合し、最初の空でないsourceInterfaceをプール全体で共有します。異なる上流ごとのインターフェース指定は個別に適用しません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    dnssecValidate: true # 任意; boolean
    healthcheck: # 任意; object
      failThreshold: 1 # 任意; integer; minimum=1
      interval: "<string>" # 任意; string
      passThreshold: 1 # 任意; integer; minimum=1
      timeout: "<string>" # 任意; string
    match: # 必須; array
      - "<string>" # string
    queryTimeout: "<string>" # 任意; string
    resolver: "<string>" # 必須; string
    upstreams: # 任意; array
      - "<string>" # string
    when: # 任意; object
      state: # 任意; object
        "<key>": # 任意; object
          contains: "<string>" # 任意; string
          equals: "<string>" # 任意; string
          exists: true # 任意; boolean
          for: "<string>" # 任意; string
          in: # 任意; array
            - "<string>" # string
          maxAge: "<string>" # 任意; string
          status: "set" # 任意; string; enum=["set", "unset", "unknown"]
    zoneRefs: # 任意; array
      - "<string>" # string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.resolver` | 既存DNSResolverの名前またはDNSResolver/nameが必須です。既定値はありません。 |
| `spec.match` | 1件以上のドメインサフィックスが必須です。大文字・小文字と末尾ドットを区別せず、example.netは同名とサブドメイン、"."は全問い合わせに一致します。全体一致の自動補完はありません。 |
| `spec.zoneRefs` | ローカル応答を返す既存DNSZoneの名前またはDNSZone/nameです。upstreamsと排他で、upstreamsが空なら必須です。 |
| `spec.upstreams` | 既存DNSUpstreamの名前またはDNSUpstream/nameで、IPやURLの直接指定ではありません。zoneRefsと排他で、参照したサーバーを1つの代替先プールにまとめます。 |
| `spec.dnssecValidate` | 既定falseです。trueは上流問い合わせのDOビットを設定し、応答のADを必須にします。ローカルで暗号学的DNSSEC検証を行うものではありません。ゾーン参照では使用しません。 |
| `spec.queryTimeout` | 上流への各問い合わせ試行に使う正のGo durationです。省略時は正のhealthcheck.timeoutを継承し、それ以外は3sです。背景監視の間隔とは別で、ローカルゾーン応答には使用しません。 |
| `spec.healthcheck` | 上流の背景監視設定です。既定はinterval 15s、timeout 3s、failThreshold 3、passThreshold 2です。ローカルゾーンでは使用しません。 |
| `spec.healthcheck.interval` | Go durationです。省略・空、または実行時に0以下なら15sです。設定検証は期間の構文のみで正値は強制しません。 |
| `spec.healthcheck.timeout` | 監視試行のGo durationです。省略・空・0以下なら3sです。正なら省略したqueryTimeoutの既定にもなります。 |
| `spec.healthcheck.failThreshold` | 上流を異常とする連続失敗回数です。既定3で、実行時の0以下も3になります。スキーマの下限は1です。 |
| `spec.healthcheck.passThreshold` | 回復に必要な連続成功回数です。既定2で、実行時の0以下も2になります。スキーマの下限は1です。 |
| `spec.when` | 共通の状態条件です。省略時は無条件で、条件により有効なリゾルバー設定から規則を除外できます。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/main.go) · [cmd/routerd-dns-resolver/upstream.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
