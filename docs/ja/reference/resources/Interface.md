# Interface

[リソース別の設定](../resources.md#interface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

既存のOSインターフェースに論理名metadata.nameを付け、他のリソースから参照できるようにします。物理インターフェースを新規作成する設定ではありません。

## 指定条件と参照関係

- 依存するリソースのinterfaceには論理名metadata.name、ifnameには実デバイス名を指定します。managed=falseでも、adminUpやMTUを指定した場合のLinkControllerによる変更は抑止しません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    adminUp: true # 任意; boolean
    ifname: "<string>" # 必須; string
    managed: true # 必須; boolean
    mtu: 576 # 任意; integer; minimum=576; maximum=9216
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
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.ifname` | ens18などの既存カーネルデバイス名。必須で既定値はありません。存在しなければ新規作成せず、Down/InterfaceNotFoundとして観測します。 |
| `spec.managed` | 管理対象の宣言。読込み上の既定値はfalseですが、JSON Schemaでは明示を要求します。管理・観測の状態や周辺処理に使いますが、adminUpやMTUを観測だけに限定する保証ではありません。 |
| `spec.adminUp` | 省略時falseで、UPにする要求を出しません。trueなら存在するDOWNのlinkをUPにします。falseは強制的にDOWNにする指定ではありません。 |
| `spec.mtu` | 省略時0は現在のMTUを維持します。0以外は576〜9216で、観測値と異なれば反映します。 |
| `spec.when` | 有効化条件。省略時は条件を設けません。state/all/any、unknown、経過時間の条件は共通の条件指定を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `ifname`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `primaryIPv4`, `primaryIPv6`, `macAddress`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
