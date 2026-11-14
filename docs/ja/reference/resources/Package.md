# Package

[リソース別の設定](../resources.md#package) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

指定したOSパッケージの導入を確認します。稼働ホストのOSに完全一致する、最初のパッケージセットを選びます。

## 指定条件と参照関係

- stateはpresentだけを扱い、パッケージの削除はできません。OSに一致するセットがなければPending/NoMatchingOSPackageSetです。各セットのoptionalは入力として受理しますが、このcontrollerは読み取らず、導入失敗はエラーになります。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: Package
  metadata:
    name: "<name>"
  spec:
    packages:
      -
        [ manager: ("" | "apt" | "dnf" | "pkg") ]
        names:
          - <string>
        [ optional: <boolean> ]
        os: ("ubuntu" | "debian" | "fedora" | "rhel" | "rocky" | "almalinux" | "freebsd")
    [ state: ("" | "present") ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.state` | *任意* | string; enum=["", "present"] | 希望するパッケージの状態。省略時presentで、他の値は拒否します。 |
| `spec.packages` | **必須** | array | OS別パッケージセットの空でない配列が必須です。リソース参照ではありません。 |
| `spec.packages[].os` | **必須** | string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"] | OS識別子。ubuntu、debian、fedora、rhel、rocky、almalinux、freebsdのいずれかが必須です。Linuxでは/etc/os-releaseのIDを読み、ID_LIKEによる類似判定は行いません。 |
| `spec.packages[].manager` | *任意* | string; enum=["", "apt", "dnf", "pkg"] | 省略時はubuntu/debianならapt、fedora/rhel/rocky/almalinuxならdnf、freebsdならpkgです。明示する場合もこのOS対応と一致する必要があります。 |
| `spec.packages[].names` | **必須** | array | 必須の空でないパッケージ名一覧。空白の名前、空白文字や/を含む名前は拒否します。この項目から暗黙のパッケージ名を追加しません。 |
| `spec.packages[].optional` | *任意* | boolean | 省略時false。値は読み込みますが、現行のPackageControllerのエラー処理には影響しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/package.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/package.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `packages`, `dryRun`
