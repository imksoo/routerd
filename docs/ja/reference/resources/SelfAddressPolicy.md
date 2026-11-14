# SelfAddressPolicy

[リソース別の設定](../resources.md#selfaddresspolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

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
