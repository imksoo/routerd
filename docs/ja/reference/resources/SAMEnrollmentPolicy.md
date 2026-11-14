# SAMEnrollmentPolicy

[リソース別の設定](../resources.md#samenrollmentpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentPolicy
  metadata:
    name: "<name>"
  spec:
    allowedLeafIDs: # 任意; object
      pattern: "<string>" # 任意; string
    directMesh: # 任意; object
      peerGroupRef: "<string>" # 任意; string
    endpointPrefixes: # 任意; array
      - "<string>" # string
    joinAudience: "<string>" # 任意; string
    joinTokenFrom: # 任意; object
      base64: true # 任意; boolean
      env: "<string>" # 任意; string
      file: "<string>" # 任意; string
    mobilityPoolRefs: # 任意; array
      - "<string>" # string
    mobilityPrefixes: # 任意; array
      - "<string>" # string
    rrNodeSetRef: "<string>" # 任意; string
    rrSetRef: "<string>" # 任意; string
    transportProfileRef: "<string>" # 必須; string
    ttl: "<string>" # 任意; string
    tunnelAddressPrefixes: # 必須; array
      - "<string>" # string
    wireGuard: # 任意; object
      allowedExtraIPPrefixes: # 任意; array
        - "<string>" # string
      endpointPrefixes: # 任意; array
        - "<string>" # string
      interface: "<string>" # 必須; string
      persistentKeepalive: 0 # 任意; integer; minimum=0; maximum=65535
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
