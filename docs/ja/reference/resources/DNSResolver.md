# DNSResolver

[リソース別の設定](../resources.md#dnsresolver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

DNSForwarderの規則に接続する、UDP・TCP待受を備えたローカルDNSサービスです。

## 指定条件と参照関係

- このリゾルバーを参照するDNSForwarderが1件以上必要です。転送規則は別リソースで設定します。旧インラインsourcesは対応するYAML入力ではありません。
- listenの省略・空リストは127.0.0.1:5053になります。各待受はUDPとTCPの両方で動作します。再帰問い合わせを許す待受は信頼するネットワークに限定してください。
- 固定値と動的な待受アドレスを結合して重複を除きます。必須参照の未解決は依存待ちとして記録しますが、他の有効なアドレスは利用します。待受が一つも解決しない場合はPendingです。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    [ cache:
        [ enabled: <boolean> ]
        [ maxEntries: <integer> ]
        [ maxTTL: <string> ]
        [ minTTL: <string> ]
        [ negativeTTL: <string> ]
    ]
    listen:
      -
        [ addressFrom:
            -
              field: <string>
              [ optional: <boolean> ]
              resource: <string>
        ]
        [ addresses:
            - <string>
        ]
        [ name: <string> ]
        [ port: <integer> ]
        [ sources:
            - <string>
        ]
    [ metrics:
        [ perUpstream: <boolean> ]
    ]
    [ queryLog:
        [ enabled: <boolean> ]
        [ path: <string> ]
    ]
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.listen` | **必須** | array | 待受一覧です。空なら正規化でループバック待受を補います。 |
| `spec.listen[].name` | *任意* | string | 待受の識別名です。省略時はリスト順にlisten-0、listen-1…となります。 |
| `spec.listen[].addresses` | *任意* | array | ポート・CIDRを含まないIPv4/IPv6アドレスです。addressesとaddressFromが両方空なら[127.0.0.1]です。 |
| `spec.listen[].addressFrom` | *任意* | array | 共通のresource・field・optionalによる参照一覧です。設定内の参照先と提供フィールドの明示が必要です。IPv4StaticAddress.addressは状態確定前でも宣言値を参照し、CIDRはアドレス部分を使用します。optional既定falseで、trueの未解決値は省略します。 |
| `spec.listen[].port` | *任意* | integer; minimum=1; maximum=65535 | 1～65535のポートです。省略・0は5053に正規化します。 |
| `spec.listen[].sources` | *任意* | array | このリゾルバーに属するDNSForwarderの名前一覧です。空なら全規則を選びます。metadata.nameの裸名を指定してください。検証は修飾名も許しますが、デーモンは接頭辞を除かず裸名と完全一致させます。このリスト順で規則順序は変わりません。 |
| `spec.cache` | *任意* | object | 応答キャッシュ設定です。対象は上流応答で、ローカルゾーンの応答は保存しません。既定は無効です。 |
| `spec.cache.enabled` | *任意* | boolean | 既定falseです。trueでメモリ内応答キャッシュを有効にします。 |
| `spec.cache.maxEntries` | *任意* | integer; minimum=0 | 省略・0は10000です。スキーマ上は0以上で、バイト数ではなくエントリー数の上限です。 |
| `spec.cache.minTTL` | *任意* | string | キャッシュ保持時間の下限をGo durationで指定します。空なら下限なしです。この処理では不正な期間は拒否せず無視します。応答内レコードのTTLは書き換えません。 |
| `spec.cache.maxTTL` | *任意* | string | Go durationによる上限で、minTTLの後に適用します。空なら上限なしで、不正な期間は無視します。 |
| `spec.cache.negativeTTL` | *任意* | string | 空の正常応答・NXDOMAINの保持時間をGo durationで上書きし、min/max制限より後に適用します。既定空では制限適用前60秒です。SERVFAILなど一時失敗は保存しません。 |
| `spec.metrics` | *任意* | object | メトリクス設定ですが、現行実行処理にperUpstreamの利用箇所はありません。 |
| `spec.metrics.perUpstream` | *任意* | boolean | 既定falseの真偽値として読み込みますが、現行実装では上流別メトリクスを切り替えません。 |
| `spec.queryLog` | *任意* | object | SQLiteデータベースへの問い合わせ記録です。既定は無効です。 |
| `spec.queryLog.enabled` | *任意* | boolean | 既定falseです。trueでqueryLog.pathへ問い合わせを記録します。 |
| `spec.queryLog.path` | *任意* | string | 有効時に省略・空白なら/var/lib/routerd/dns-queries.dbです。テキストログではなくデータベースのパスです。 |
| `spec.when` | *任意* | object | 共通の状態条件です。省略時は無条件です。state/all/anyとスキーマ制限は共通項を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/main.go) · [cmd/routerd-dns-resolver/zone.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/zone.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `listeners`, `listenAddresses`, `sources`, `updatedAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
