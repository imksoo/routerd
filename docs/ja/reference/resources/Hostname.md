# Hostname

[リソース別の設定](../resources.md#hostname) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

OSのホスト名を管理します。Router.metadata.nameや各リソース名とは別です。

## 指定条件と参照関係

- 管理するHostnameを複数指定するとreconcile時に失敗します。managed=falseの項目はcontrollerが無視します。Linuxではhostnamectl、FreeBSDではsysrcとhostnameを使います。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Hostname
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # 必須; string
    managed: true # 任意; boolean
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.hostname` | 必須の空でないOSホスト名。既定値はなく、空白・タブ・改行・/を拒否します。DNSZoneやInterfaceの参照ではありません。 |
| `spec.managed` | 省略時falseでホスト名を変更しません。trueなら現在値と永続ホスト名を観測し、必要なら変更します。dry-runでは変更コマンドを実行しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/hostname.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hostname.go)
