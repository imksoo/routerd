# ManagementAccess

[リソース別の設定](../resources.md#managementaccess) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

反映前の管理経路確認で検査するインターフェースを宣言します。管理用の待受やファイアウォール許可ルールを生成するリソースではありません。

## 指定条件と参照関係

- 反映前とrouterctl doctorで確認します。fail判定は実反映を止めますが、明示的な--allow-mgmt-lockout指定時は続行します。dry-runは報告のみです。検査対象は設定であり、実際の管理疎通を測定するものではありません。
- 各参照先はInterfaceとして宣言する必要があります。FirewallZoneが1件でもある場合、各管理インターフェースはroleがtrustまたはmgmtのゾーンに所属する必要があります。Bridge・WireGuardのリソース名だけではこの検査を満たしません。
- ManagementAccess自体がなければ、この検査は指摘を追加しません。requireWebConsoleBoundは明示された全アドレス待受を検査するもので、WebConsoleの存在や待受アドレスが管理インターフェースに属することまでは要求・証明しません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: ManagementAccess
  metadata:
    name: "<name>"
  spec:
    [ allowSourceCIDRs:
        - <string>
    ]
    interfaces:
      - <string>
    [ requireWebConsoleBound: <boolean> ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.interfaces` | **必須** | array | 1件以上のInterface名またはInterface/nameが必須です。正規化後の重複は拒否されます。任意のOS名ではなく、宣言済みリソースの名前です。 |
| `spec.allowSourceCIDRs` | *任意* | array | IPv4・IPv6のCIDR一覧で、既定は空です。書式は検証しますが、現行の管理経路検査・ファイアウォール出力では使用しません。送信元アドレスのアクセス制限にはなりません。 |
| `spec.requireWebConsoleBound` | *任意* | boolean | 既定はtrueです。有効なWebConsoleが0.0.0.0または::を明示して待ち受けるとfail、falseならwarnです。複数のManagementAccessがある場合、いずれか1件の省略・trueでこの条件を有効にします。動的な待受アドレスは警告だけで、参照もないlistenAddress省略にはここでは指摘しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/management.go](https://github.com/imksoo/routerd/blob/main/pkg/config/management.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go) · [cmd/routerctl/doctor.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `interfaces`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
