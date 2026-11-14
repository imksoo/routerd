# WebConsole

[リソース別の設定](../resources.md#webconsole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

routerdから読取り用のWebコンソールを公開します。設定変更を行うControlAPIとは別のHTTP待受です。

## 指定条件と参照関係

- runtimeは最初のWebConsoleを選び、リソースごとに待受を起動しません。listenAddressFromの解決値をlistenAddressより優先し、listenAddressは代替値として使えます。排他指定ではありません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    [ basePath: <string> ]
    [ enabled: <boolean> ]
    [ links:
        -
          [ description: <string> ]
          label: <string>
          url: <string>
    ]
    [ listenAddress: <string> ]
    [ listenAddressFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ port: <integer> ]
    [ title: <string> ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.enabled` | *任意* | boolean | リソースがある場合の省略時はtrueです。falseなら選択されたコンソールを無効にします。リソース自体がなければ待受は起動しません。 |
| `spec.listenAddress` | *任意* | string | 代替の待受IP。省略時127.0.0.1で、空白でない指定はIPとして解析可能である必要があります。listenAddressFromの解決値を優先します。 |
| `spec.listenAddressFrom` | *任意* | object | 待受IPの取得元。省略時は未指定です。状態を先に読み、その後、対応する設定上のインターフェース・アドレスを解決します。resourceとfieldが必要です。未解決でlistenAddressもなければ、optional=trueでもコンソール起動は失敗します。 |
| `spec.port` | *任意* | integer; minimum=1; maximum=65535 | TCPポート。省略時8080。読込み値0も既定値を選び、0以外は1〜65535を指定します。 |
| `spec.basePath` | *任意* | string | HTTPのパス接頭辞。省略時/です。空でない値は/で始め、NULや改行を含めません。 |
| `spec.title` | *任意* | string | コンソールのページタイトル。省略時routerdで、ルーターやリソース自体の名前は変更しません。 |
| `spec.links` | *任意* | array | 追加リンクの一覧。省略時は空です。labelかURLが空白の要素は表示時に省きます。 |
| `spec.links[].label` | **条件付き必須** | string | リンクの表示文字。既定値はなく、空白なら表示しません。 |
| `spec.links[].url` | **条件付き必須** | string | リンク先URL。既定値はありません。Kind/名前の参照ではなく、前後の空白は除去します。 |
| `spec.links[].description` | *任意* | string | 任意の説明文。省略時は空で、表示時に前後の空白を除去します。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/webconsole/console.go](https://github.com/imksoo/routerd/blob/main/pkg/webconsole/console.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `listenAddress`, `port`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
