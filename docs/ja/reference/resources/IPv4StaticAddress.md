# IPv4StaticAddress

[リソース別の設定](../resources.md#ipv4staticaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

論理インターフェースへ固定IPv4アドレスを宣言します。controllerが設定に収束させるアドレスであり、DHCPリースではありません。

## 指定条件と参照関係

- interface参照とaddress/prefixが必須です。同じ論理インターフェースとマスク後のネットワークprefixを持つ固定アドレスは、ホスト部が異なっても重複として拒否します。同じ組合せのVirtualAddressとの重複も拒否します。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必須; string
    allowOverlap: true # 任意; boolean
    allowOverlapReason: "<string>" # 任意; string
    exclusive: true # 任意; boolean
    interface: "<string>" # 必須; string
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
| `spec.interface` | 必須の論理インターフェース名。Interface/は付けず、宣言済みRouterのインターフェース一覧に存在する必要があります。既定値はありません。 |
| `spec.address` | 192.0.2.1/24などのプレフィックス長付きIPv4ホストアドレス。必須で既定値や暗黙の/32はありません。入力はprefix構文を確認し、実行時の操作はIPv4を使います。 |
| `spec.exclusive` | 省略時false。現行observe処理はexclusiveによる掃除が未実装と警告し、実行時のアドレスcontrollerもこの値を読みません。余分なアドレスの全体reconcileは別なので、falseでも手作業のアドレスを維持する保証はありません。 |
| `spec.allowOverlap` | 省略時false。読込み可能な項目として残りますが、現行の検証・アドレスreconcileでは使わず、重複・競合の検証も回避しません。 |
| `spec.allowOverlapReason` | 省略時は空です。宣言として保持するだけで、重複の許可やallowOverlapの効果を変えません。 |
| `spec.when` | 有効化条件。省略時は条件を設けません。state/all/any、unknown、経過時間の条件は共通の条件指定を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `interface`, `ifname`, `address`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/basic-static.yaml)
