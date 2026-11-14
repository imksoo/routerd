# IngressService

[リソース別の設定](../resources.md#ingressservice) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends: # 必須; array
      - # object
        address: "<string>" # 任意; string
        addressFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        name: "<string>" # 任意; string
        port: 1 # 必須; integer; minimum=1; maximum=65535
        weight: 0 # 任意; integer; minimum=0
    externalDNS: true # 任意; boolean
    hairpin: # 任意; object
      enabled: true # 任意; boolean
      interfaces: # 任意; array
        - "<string>" # string
      mode: "" # 任意; string; enum=["", "auto", "manual", "off"]
    healthCheck: # 任意; object
      expectedBody: "<string>" # 任意; string
      expectedStatus: # 任意; array
        - 0 # integer
      healthyThreshold: 1 # 任意; integer; minimum=1
      host: "<string>" # 任意; string
      interval: "<string>" # 任意; string
      path: "<string>" # 任意; string
      protocol: "" # 任意; string; enum=["", "tcp", "http", "https"]
      timeout: "<string>" # 任意; string
      tlsSkipVerify: true # 任意; boolean
      unhealthyThreshold: 1 # 任意; integer; minimum=1
    hostname: "<string>" # 任意; string
    listen: # 必須; object
      address: "<string>" # 任意; string
      addressFrom: # 任意; object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
      interface: "<string>" # 必須; string
      port: 1 # 必須; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必須; string; enum=["tcp", "udp"]
    policy: # 任意; object
      onNoHealthyBackends: "" # 任意; string; enum=["", "drop", "reject"]
      selection: "" # 任意; string; enum=["", "failover", "sourceHash", "random"]
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)
