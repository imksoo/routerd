# DNSUpstream

[リソース別の設定](../resources.md#dnsupstream) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

DNSForwarderが参照するDNSサーバーの接続先です。宣言だけで待受を開始したり問い合わせを転送したりはしません。

## 指定条件と参照関係

- 内部URL生成にはudpの補完がありますが、設定ではprotocolが必須です。addressまたはaddressFromの1つ以上が必要で、併用時は接続先を結合します。動的addressFromは現行UDPのみです。
- 必須addressFromが未解決なら固定アドレスがあっても、それを含む転送規則全体を見送ります。optionalの未解決値は省略します。bootstrapは異なり、固定一覧があれば必須の動的bootstrapの未解決を補えます。
- sourceInterfaceは論理名・Kind/nameから変換せず、そのままソケットに渡します。Linuxではmetadata.nameとOSのifnameが一致する宣言を使ってください。FreeBSDのソケット処理はifname直接指定を拒否し、通常設定の検証もここで利用できるfib:<n>の代替指定を提供していません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: "<name>"
  spec:
    [ address: <string> ]
    [ addressFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ bootstrap:
        - <string>
    ]
    [ bootstrapFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ path: <string> ]
    [ port: <integer> ]
    protocol: ("udp" | "tcp" | "dot" | "doh")
    [ sourceInterface: <string> ]
    [ tlsName: <string> ]
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
| `spec.protocol` | **必須** | string; enum=["udp", "tcp", "dot", "doh"] | udp・tcp・dot・dohのいずれかが必須です。小文字の正式表記を使用します。UDP/TCPは平文DNS、DoTはTLS、DoHはHTTPSです。 |
| `spec.address` | **条件付き必須** | string | スキーム・ポート・パスを含まないサーバーIPまたはホスト名です。addressFromが空なら必須です。暗黙のサーバーはなく、bootstrap指定時は名前解決に使用します。 |
| `spec.addressFrom` | **条件付き必須** | array | UDP専用の状態値参照一覧です。各項目に設定内のresourceと提供フィールドの明示が必要で、optional既定falseです。動的な裸アドレスは本リソースのportとは独立してUDP 53へ正規化します。複数サーバーの返却も可能です。 |
| `spec.port` | *任意* | integer; minimum=1; maximum=65535 | 1～65535です。省略・0はudp/tcpで53、dotで853、dohで443です。固定addressに適用し、addressFromが返す裸値には適用しません。 |
| `spec.path` | *任意* | string | DoHのURLパスで、既定は/dns-queryです。dohで指定する場合は/始まりが必須です。他プロトコルでは使いません。 |
| `spec.tlsName` | *任意* | string | DoTの証明書検証・SNI用サーバー名です。省略時は接続先ホスト名を使用します。DoHでは明示指定が検証で拒否されます。UDP/TCPは検証で許しても使用しません。 |
| `spec.bootstrap` | *任意* | array | 上流ホスト名の解決用DNS一覧です。bootstrapFromも空・省略で必須依存待ちがなければOSのリゾルバーを使います。同じ転送規則内の上流で結合した設定を共有します。 |
| `spec.bootstrapFrom` | *任意* | array | field必須の参照一覧で、resource・fieldは非空必須、optional既定falseです。実在するKind/nameと提供状態フィールドを指定してください。ただし通常のStatusValueSourceSpecとは別型のため、現行検証は参照先の存在・提供リストへの所属まで検査しません。動的・固定サーバーを結合し、固定bootstrapがあれば必須動的値の未解決を補えます。 |
| `spec.sourceInterface` | *任意* | string | 宣言したInterfaceまたはWireGuardInterfaceの名前です。既定空ならOSの経路選択に任せます。転送プール単位で共有し、裸のOS名・FreeBSDに関する上記制限があります。 |
| `spec.when` | *任意* | object | 共通の状態条件です。既定は無条件で、除外されると転送規則に有効な接続先がなくなる場合があります。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/upstream.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream.go) · [cmd/routerd-dns-resolver/upstream_bind_linux.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_linux.go) · [cmd/routerd-dns-resolver/upstream_bind_freebsd.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_freebsd.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `address`, `url`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
