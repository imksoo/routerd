# SelfAddressPolicy

[リソース別の設定](../resources.md#selfaddresspolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

対応するDHCPv6静的生成で使用する、順序付き自己アドレス選択方針です。インターフェースへアドレスを割り当てるものではありません。

## 指定条件と参照関係

- 現行実行時のDHCPv6 LAN生成はDHCPv6Server.selfAddressPolicy・dnsSource: selfを参照しません。この方針は別の静的生成に作用しますが、宣言だけで稼働サーバーが自己DNSを配布することにはなりません。実行時はdnsServers・dnsServerFromを明示します。
- 参照は宣言したInterface・IPv6DelegatedAddressの裸名で、source分岐で使わなくても存在検証します。このリソースにwhen・dependsOnはありません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: SelfAddressPolicy
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # 必須; string; enum=["ipv4", "ipv6"]
    candidates: # 必須; array
      - # object
        address: "<string>" # 任意; string
        addressSuffix: "<string>" # 任意; string
        delegatedAddress: "<string>" # 任意; string
        interface: "<string>" # 任意; string
        matchSuffix: "<string>" # 任意; string
        ordinal: 1 # 任意; integer; minimum=1
        source: "delegatedAddress" # 必須; string; enum=["delegatedAddress", "interfaceAddress", "static"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.addressFamily` | ipv4・ipv6が必須です。検証は両方を許しますが、現行の選択利用箇所は旧DHCPv6静的生成で、ipv4の独立した実行時選択サービスにはなりません。 |
| `spec.candidates` | 1件以上の順序付き候補一覧が必須です。対応する静的生成は順に試し、最初の成功を選んで全滅ならエラーです。明示参照したポリシーへの自動候補補完はありません。 |
| `spec.candidates[].source` | delegatedAddress・interfaceAddress・staticが必須です。delegatedAddressはIPv6専用でaddress・interface禁止です。interfaceAddressはinterface、staticはaddressFamilyに合うaddress必須です。後二者の不要な他フィールドは一般には拒否しません。 |
| `spec.candidates[].interface` | interfaceAddressで必須の宣言済み論理インターフェースです。delegatedAddressでは禁止で、他sourceで使わなくても指定時は存在検証します。 |
| `spec.candidates[].delegatedAddress` | delegatedAddressで必須のIPv6DelegatedAddress裸名です。存在が必要で、対応生成はそのインターフェースの観測プレフィックスから生成し、汎用状態値fieldではありません。 |
| `spec.candidates[].address` | staticで必須の、addressFamilyと一致する裸アドレスです。delegatedAddressでは禁止で、OSへの自動割当は行いません。 |
| `spec.candidates[].addressSuffix` | delegatedAddressの任意IPv6サフィックスです。省略時は参照リソースのaddressSuffixで、他sourceの選択には使いません。 |
| `spec.candidates[].matchSuffix` | interfaceAddressの任意IPv6パターンです。0でない各バイトを比較し、0のバイトは制約なしとなるため通常の連続サフィックスマスクではありません。指定時は最初に一致するグローバルIPv6を選びordinalは無視します。 |
| `spec.candidates[].ordinal` | matchSuffixが空の場合のinterfaceAddressの1始まり番号です。省略・0は1で、スキーマ下限1、負値は検証拒否です。観測一覧の順序で、安定したアドレスIDではありません。範囲外なら次候補を試します。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)
