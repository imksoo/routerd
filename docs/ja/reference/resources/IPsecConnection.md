# IPsecConnection

[リソース別の設定](../resources.md#ipsecconnection) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    certificateRef: "<string>" # 任意; string
    cloudProviderHint: "" # 任意; string; enum=["", "aws", "azure", "gcp"]
    leftSubnet: "<string>" # 必須; string
    localAddress: "<string>" # 必須; string
    phase1Proposals: # 任意; array
      - "<string>" # string
    phase2Proposals: # 任意; array
      - "<string>" # string
    preSharedKey: "<string>" # 任意; string
    remoteAddress: "<string>" # 必須; string
    rightSubnet: "<string>" # 必須; string
```
