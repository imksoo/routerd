# FirewallFlowPinhole

[リソース別の設定](../resources.md#firewallflowpinhole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallFlowPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # 任意; object
      key: "" # 任意; string; enum=["", "remoteAddress", "localEndpoint"]
    fromZone: "<string>" # 必須; string
    inbound: # 必須; object
      sourcePorts: # 必須; array
        - "<string>" # string
    outbound: # 必須; object
      destinationCIDRs: # 任意; array
        - "<string>" # string
      destinationPorts: # 必須; array
        - "<string>" # string
      destinationSetRefs: # 任意; array
        - "<string>" # string
      protocol: "udp" # 必須; string; enum=["udp"]
      sourceCIDRs: # 任意; array
        - "<string>" # string
      sourceSetRef: "<string>" # 任意; string
    timeout: "<string>" # 任意; string
    toZone: "<string>" # 必須; string
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
