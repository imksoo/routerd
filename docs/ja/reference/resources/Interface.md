# Interface

[リソース別の設定](../resources.md#interface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

既存のOSインターフェースに論理名metadata.nameを付け、他のリソースから参照できるようにします。物理インターフェースを新規作成する設定ではありません。

## 指定条件と参照関係

- 依存するリソースのinterfaceには論理名metadata.name、ifnameには実デバイス名を指定します。managed=falseでも、adminUpやMTUを指定した場合のLinkControllerによる変更は抑止しません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    [ adminUp: <boolean> ]
    ifname: <string>
    managed: <boolean>
    [ mtu: <integer> ]
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
| `spec.ifname` | **必須** | string | ens18などの既存カーネルデバイス名。必須で既定値はありません。存在しなければ新規作成せず、Down/InterfaceNotFoundとして観測します。 |
| `spec.managed` | **必須** | boolean | 管理対象の宣言。読込み上の既定値はfalseですが、JSON Schemaでは明示を要求します。管理・観測の状態や周辺処理に使いますが、adminUpやMTUを観測だけに限定する保証ではありません。 |
| `spec.adminUp` | *任意* | boolean | 省略時falseで、UPにする要求を出しません。trueなら存在するDOWNのlinkをUPにします。falseは強制的にDOWNにする指定ではありません。 |
| `spec.mtu` | *任意* | integer; minimum=576; maximum=9216 | 省略時0は現在のMTUを維持します。0以外は576〜9216で、観測値と異なれば反映します。 |
| `spec.when` | *任意* | object | 有効化条件。省略時は条件を設けません。state/all/any、unknown、経過時間の条件は共通の条件指定を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `ifname`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `primaryIPv4`, `primaryIPv6`, `macAddress`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
