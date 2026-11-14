# SAMEnrollmentClient

[リソース別の設定](../resources.md#samenrollmentclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClient
  metadata:
    name: "<name>"
  spec:
    bootstrapEndpoints: # 任意; array
      - "<string>" # string
    claimRef: "<string>" # 必須; string
    controlAPITLS: # 任意; object
      caFile: "<string>" # 任意; string
      certFile: "<string>" # 任意; string
      insecureSkipVerify: true # 任意; boolean
      keyFile: "<string>" # 任意; string
      serverName: "<string>" # 任意; string
    controlAPITokenFrom: # 任意; object
      base64: true # 任意; boolean
      env: "<string>" # 任意; string
      file: "<string>" # 任意; string
    retryBackoff: # 任意; object
      max: "<string>" # 任意; string
      min: "<string>" # 任意; string
    rrSocket: "<string>" # 任意; string
    stateTTLRefreshBefore: "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
