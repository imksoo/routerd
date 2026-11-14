# LocalServiceRedirect

[リソース別の設定](../resources.md#localserviceredirect) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

宣言したインターフェースで受信した対象通信を、nftablesのprerouting NATでルーター自身のサービスポートへ転送します。

## 指定条件と参照関係

- interfaceと1件以上のrulesが必須です。各ルールにはprotocols・destinationSetRef・両ポートが必要です。ルール内の条件はANDで、指定プロトコルの選択肢はORです。
- 受信インターフェース・宛先アドレス集合・宛先ポートで照合します。ローカル発信のOUTPUTではなく、受信パケットが対象です。転送先はルーター自身で、リモートのバックエンドへの転送ではありません。別用途にはPortForward・IngressServiceを使います。
- ローカルの待受サービスとファイアウォール許可は別途設定します。リダイレクトはDNSResolverなどのデーモンを起動せず、包括的な許可ルールも追加しません。参照集合のアドレスファミリーに応じてIPv4・IPv6ルールを出力し、DNS由来の集合はIPAddressSetが更新します。この実装はnftables用で、FreeBSDのPFリダイレクトは出力しません。
- ルールはYAMLの並び順ではなく、リソースID・ルール名の順にソートします。name省略時は0始まりのリスト番号を文字列にし、辞書順では10が2より先になります。重複する条件がある場合は順序を意図した名前を明示してください。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: LocalServiceRedirect
  metadata:
    name: "<name>"
  spec:
    interface: <string>
    rules:
      -
        destinationPort: <integer>
        destinationSetRef: <string>
        [ name: <string> ]
        protocols:
          - <string>
        redirectPort: <integer>
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
| `spec.interface` | **必須** | string | 同じRouterで宣言した論理インターフェースが必須です。OS上の受信インターフェース名に解決し、既定値・状態参照はありません。 |
| `spec.rules` | **必須** | array | 1件以上のリダイレクト定義が必須です。プロトコル・宛先集合・ポートは他ルールから継承しません。 |
| `spec.rules[].name` | *任意* | string | 任意の並び順・コメント用ラベルです。既定は0始まりの番号で、リソース参照ではありません。ここでは重複名を拒否しません。 |
| `spec.rules[].protocols` | **必須** | array | tcp・udpの1件以上が必須です。重複や別プロトコルは拒否し、両方を暗黙に指定する既定値はありません。 |
| `spec.rules[].destinationSetRef` | **必須** | string | 同じRouterで宣言したIPAddressSetの名前またはIPAddressSet/nameが必須です。field指定やインラインのアドレス一覧は扱いません。 |
| `spec.rules[].destinationPort` | **必須** | integer; minimum=1; maximum=65535 | 転送前の宛先ポート1～65535が必須です。範囲指定や既定値はありません。 |
| `spec.rules[].redirectPort` | **必須** | integer; minimum=1; maximum=65535 | 転送先のローカルポート1～65535が必須です。転送後に接続できる待受が必要ですが、YAML検証では待受を確認しません。 |
| `spec.when` | *任意* | object | 任意の共通有効化条件で、既定は無条件です。state・all・anyと参照制約は共通項を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/ipaddressset.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipaddressset.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)
