# DNSResolver

[リソース別の設定](../resources.md#dnsresolver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    cache: # 任意; object
      enabled: true # 任意; boolean
      maxEntries: 0 # 任意; integer; minimum=0
      maxTTL: "<string>" # 任意; string
      minTTL: "<string>" # 任意; string
      negativeTTL: "<string>" # 任意; string
    listen: # 必須; array
      - # object
        addressFrom: # 任意; array
          - # object
            field: "<string>" # 任意; string
            optional: true # 任意; boolean
            resource: "<string>" # 必須; string
        addresses: # 任意; array
          - "<string>" # string
        name: "<string>" # 任意; string
        port: 1 # 任意; integer; minimum=1; maximum=65535
        sources: # 任意; array
          - "<string>" # string
    metrics: # 任意; object
      perUpstream: true # 任意; boolean
    queryLog: # 任意; object
      enabled: true # 任意; boolean
      path: "<string>" # 任意; string
    when: # 任意; object
      state: # 任意; object
        "<key>": # 任意; object
          contains: "<string>" # 任意; string
          equals: "<string>" # 任意; string
          exists: true # 任意; boolean
          for: "<string>" # 任意; string
          in: # 任意; array
            - "<string>" # string
          maxAge: "<string>" # 任意; string
          status: "set" # 任意; string; enum=["set", "unset", "unknown"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
