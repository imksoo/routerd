# IPv6RAAddress

[リソース別の設定](../resources.md#ipv6raaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

ホスト側でIPv6 RA/SLAACを受信する宣言です。RA clientであり、LANへRAを送るIPv6RouterAdvertisementとは別です。

## 指定条件と参照関係

- managedはホストのRA設定を制御します。FreeBSDはaccept_rtadv/rtsoldを生成します。Linuxのnetworkd引継ぎではRAを維持・有効化し、DHCPv6をnetworkdから切り離す場合はdrop-inでRA起点のDHCPv6起動も抑止します。上流から有効なRAが届く保証ではありません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RAAddress
  metadata:
    name: "<name>"
  spec:
    interface: <string>
    [ managed: <boolean> ]
    [ required: <boolean> ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.interface` | **必須** | string | 必須の論理インターフェース名。Interface/は付けず、宣言済みRouterのインターフェース一覧に存在する必要があります。既定値はありません。 |
| `spec.managed` | *任意* | boolean | 省略時true。falseならホストのRA設定を要求せず観測する意図にします。Interfaceのmanagedとは独立しています。 |
| `spec.required` | *任意* | boolean | 省略時false。現行の観測・生成経路では読み取らず、applyが戻る前にSLAACアドレス取得を完了させる条件にはなりません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `interface`, `address`
