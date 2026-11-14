# Package

[リソース別の設定](../resources.md#package) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

指定したOSパッケージの導入を確認します。稼働ホストのOSに完全一致する、最初のパッケージセットを選びます。

## 指定条件と参照関係

- stateはpresentだけを扱い、パッケージの削除はできません。OSに一致するセットがなければPending/NoMatchingOSPackageSetです。各セットのoptionalは入力として受理しますが、このcontrollerは読み取らず、導入失敗はエラーになります。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Package
  metadata:
    name: "<name>"
  spec:
    packages: # 必須; array
      - # object
        manager: "" # 任意; string; enum=["", "apt", "dnf", "pkg"]
        names: # 必須; array
          - "<string>" # string
        optional: true # 任意; boolean
        os: "ubuntu" # 必須; string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"]
    state: "" # 任意; string; enum=["", "present"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.state` | 希望するパッケージの状態。省略時presentで、他の値は拒否します。 |
| `spec.packages` | OS別パッケージセットの空でない配列が必須です。リソース参照ではありません。 |
| `spec.packages[].os` | OS識別子。ubuntu、debian、fedora、rhel、rocky、almalinux、freebsdのいずれかが必須です。Linuxでは/etc/os-releaseのIDを読み、ID_LIKEによる類似判定は行いません。 |
| `spec.packages[].manager` | 省略時はubuntu/debianならapt、fedora/rhel/rocky/almalinuxならdnf、freebsdならpkgです。明示する場合もこのOS対応と一致する必要があります。 |
| `spec.packages[].names` | 必須の空でないパッケージ名一覧。空白の名前、空白文字や/を含む名前は拒否します。この項目から暗黙のパッケージ名を追加しません。 |
| `spec.packages[].optional` | 省略時false。値は読み込みますが、現行のPackageControllerのエラー処理には影響しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/package.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/package.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `packages`, `dryRun`
