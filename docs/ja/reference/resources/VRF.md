# VRF

[リソース別の設定](../resources.md#vrf) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

LinuxのVRFデバイス・経路表・参加インターフェースの意図を宣言します。現行実装は計画とartifact識別子の生成、経路表の保護を行いますが、VRF作成・メンバー接続のcontrollerはありません。plan表示をVRF作成済みの証拠とは扱わないでください。

## 指定条件と参照関係

- membersにはRouterに宣言済みの論理インターフェース名を指定します。自分自身への参加は拒否します。routeTableはポリシー経路の掃除でその表を維持するためにも使い、BGPのVRF設定からも参照します。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VRF
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # 任意; string
    members: # 任意; array
      - "<string>" # string
    routeTable: 1 # 必須; integer; minimum=1; maximum=4294967295
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.ifname` | カーネルのVRF名。省略時metadata.name。15文字以下で、空白や/は使えません。 |
| `spec.routeTable` | 必須の正の経路表IDで既定値はありません。スキーマ上は1〜4294967295で、現行Go検証は下限を確認します。 |
| `spec.members` | 論理インターフェース名の一覧。省略時は空でKind/は付けません。各要素が存在し、このVRFのmetadata.nameとは異なる必要があります。実際の接続処理は未実装です。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/apply/artifacts.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/artifacts.go) · [pkg/controller/chain/policyroute.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/policyroute.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `ifname`, `routeTable`, `members`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/multi-instance-bgp.yaml)
