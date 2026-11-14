# PortForward

[リソース別の設定](../resources.md#portforward) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    hairpin: # 任意; object
      enabled: true # 任意; boolean
      interfaces: # 任意; array
        - "<string>" # string
      mode: "" # 任意; string; enum=["", "auto", "manual", "off"]
    listen: # 必須; object
      address: "<string>" # 任意; string
      addressFrom: # 任意; object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
      interface: "<string>" # 必須; string
      port: 1 # 必須; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必須; string; enum=["tcp", "udp"]
    target: # 必須; object
      address: "<string>" # 任意; string
      addressFrom: # 任意; object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
      port: 1 # 必須; integer; minimum=1; maximum=65535
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)
