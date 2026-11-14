# HealthCheck

[リソース別の設定](../resources.md#healthcheck) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # 任意; string; enum=["ipv4", "ipv6"]
    enabled: true # 任意; boolean
    healthyThreshold: 1 # 任意; integer; minimum=1
    interface: "<string>" # 任意; string
    interval: "<string>" # 任意; string
    port: 0 # 任意; integer; minimum=0; maximum=65535
    protocol: "" # 任意; string; enum=["", "icmp", "tcp", "dns", "http"]
    role: "link" # 任意; string; enum=["link", "next-hop", "internet", "service", "policy"]
    target: "<string>" # 任意; string
    targetSource: "auto" # 任意; string; enum=["auto", "static", "defaultGateway", "dsliteRemote"]
    timeout: "<string>" # 任意; string
    type: "ping" # 任意; string; enum=["ping"]
    unhealthyThreshold: 1 # 任意; integer; minimum=1
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
