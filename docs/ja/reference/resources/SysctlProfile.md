# SysctlProfile

[リソース別の設定](../resources.md#sysctlprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

ルーター向けのsysctlセットを名前で選び、必要ならキー単位で上書きします。Sysctlリソースの名前を参照する設定ではありません。

## 指定条件と参照関係

- overridesで既存キーの値を変えても、そのキーの比較方式とoptional指定は維持します。新規キーはexact比較・optional=falseで追加します。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: SysctlProfile
  metadata:
    name: "<name>"
  spec:
    overrides: # 任意; object
      "<key>": "<string>" # 任意; string
    persistent: true # 任意; boolean
    profile: "router-linux" # 必須; string; enum=["router-linux", "router-freebsd"]
    runtime: true # 任意; boolean
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.profile` | 必須の選択値router-linuxまたはrouter-freebsd。OSからの自動選択はありません。FreeBSD用はIPv4/IPv6転送、Linux用は転送に加えてキュー・ソケット・TCP・optionalなconntrack調整を含みます。正確な全キーと値はリンク先の実装を参照してください。 |
| `spec.runtime` | セットの各項目を稼働中のカーネルへ反映します。省略時trueです。 |
| `spec.persistent` | 各項目の永続sysctl.dファイルを作ります。省略時false。runtimeとは独立です。 |
| `spec.overrides` | カーネルのsysctlキーから空白でない文字列値への対応表。省略時は空です。既存キーの値を置換し、新規キーは追加します。キーの空白や/、空白だけの値は拒否します。リソース参照ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/profile.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/profile.go)
