# WebConsole

[リソース別の設定](../resources.md#webconsole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

routerdから読取り用のWebコンソールを公開します。設定変更を行うControlAPIとは別のHTTP待受です。

## 指定条件と参照関係

- runtimeは最初のWebConsoleを選び、リソースごとに待受を起動しません。listenAddressFromの解決値をlistenAddressより優先し、listenAddressは代替値として使えます。排他指定ではありません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    basePath: "<string>" # 任意; string
    enabled: true # 任意; boolean
    links: # 任意; array
      - # object
        description: "<string>" # 任意; string
        label: "<string>" # 必須; string
        url: "<string>" # 必須; string
    listenAddress: "<string>" # 任意; string
    listenAddressFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    port: 1 # 任意; integer; minimum=1; maximum=65535
    title: "<string>" # 任意; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.enabled` | リソースがある場合の省略時はtrueです。falseなら選択されたコンソールを無効にします。リソース自体がなければ待受は起動しません。 |
| `spec.listenAddress` | 代替の待受IP。省略時127.0.0.1で、空白でない指定はIPとして解析可能である必要があります。listenAddressFromの解決値を優先します。 |
| `spec.listenAddressFrom` | 待受IPの取得元。省略時は未指定です。状態を先に読み、その後、対応する設定上のインターフェース・アドレスを解決します。resourceとfieldが必要です。未解決でlistenAddressもなければ、optional=trueでもコンソール起動は失敗します。 |
| `spec.port` | TCPポート。省略時8080。読込み値0も既定値を選び、0以外は1〜65535を指定します。 |
| `spec.basePath` | HTTPのパス接頭辞。省略時/です。空でない値は/で始め、NULや改行を含めません。 |
| `spec.title` | コンソールのページタイトル。省略時routerdで、ルーターやリソース自体の名前は変更しません。 |
| `spec.links` | 追加リンクの一覧。省略時は空です。labelかURLが空白の要素は表示時に省きます。 |
| `spec.links[].label` | リンクの表示文字。既定値はなく、空白なら表示しません。 |
| `spec.links[].url` | リンク先URL。既定値はありません。Kind/名前の参照ではなく、前後の空白は除去します。 |
| `spec.links[].description` | 任意の説明文。省略時は空で、表示時に前後の空白を除去します。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/webconsole/console.go](https://github.com/imksoo/routerd/blob/main/pkg/webconsole/console.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
