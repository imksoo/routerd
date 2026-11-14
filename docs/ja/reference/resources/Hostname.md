# Hostname

[リソース別の設定](../resources.md#hostname) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

OSのホスト名を管理します。Router.metadata.nameや各リソース名とは別です。

## 指定条件と参照関係

- 管理するHostnameを複数指定するとreconcile時に失敗します。managed=falseの項目はcontrollerが無視します。Linuxではhostnamectl、FreeBSDではsysrcとhostnameを使います。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: Hostname
  metadata:
    name: "<name>"
  spec:
    hostname: <string>
    [ managed: <boolean> ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.hostname` | **必須** | string | 必須の空でないOSホスト名。既定値はなく、空白・タブ・改行・/を拒否します。DNSZoneやInterfaceの参照ではありません。 |
| `spec.managed` | *任意* | boolean | 省略時falseでホスト名を変更しません。trueなら現在値と永続ホスト名を観測し、必要なら変更します。dry-runでは変更コマンドを実行しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/hostname.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hostname.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `hostname`
