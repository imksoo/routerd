# CloudProviderProfile

[リソース別の設定](../resources.md#cloudproviderprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: CloudProviderProfile
  metadata:
    name: "<name>"
  spec:
    auth: # 必須; object
      command: "<string>" # 任意; string
      mode: "external-command" # 必須; string; enum=["external-command"]
    capabilities: # 必須; array
      - "<string>" # string
    provider: "azure" # 必須; string; enum=["azure", "aws", "oci", "gcp"]
    resourceGroup: "<string>" # 任意; string
    subscriptionID: "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
