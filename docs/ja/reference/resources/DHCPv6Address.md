# DHCPv6Address

[リソース別の設定](../resources.md#dhcpv6address) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

ホストのDHCPv6 clientで取得するIPv6アドレスを宣言します。DHCPv6PrefixDelegationとは別で、LAN向けプレフィックスの配布は行いません。

## 指定条件と参照関係

- 現行コードでは宣言の検証・索引化・観測・ホストのネットワーク引継ぎ判定に使います。DHCPv6Address専用daemonを生成する処理はありません。planの成功だけでホストのclient設定が完了したとは判断できません。
- 同じインターフェースで外部PD clientを使う場合、DHCPv6Addressのclient選択と一致する必要があります。設定索引はclient省略をnetworkdと扱い、現行observe/planの表示はdhcpcdと扱います。省略時の扱いが一貫しないため、利用するclientを明示してください。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Address
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    required: true # 任意; boolean
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.interface` | 必須の論理インターフェース名。Interface/は付けず、宣言済みRouterのインターフェース一覧に存在する必要があります。既定値はありません。 |
| `spec.client` | ホストDHCPv6 client・プログラムの選択。読込み上は省略時空です。networkdは引継ぎ判定で識別し、別名を明示した場合はそのIFのnetworkd DHCPv6を無効にします。検証時の既知の列挙値制限はなく、リソース名参照ではありません。 |
| `spec.required` | 省略時false。現行のアドレス観測・引継ぎ実装では使わず、アドレス取得を待つ条件は追加しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `interface`, `address`
