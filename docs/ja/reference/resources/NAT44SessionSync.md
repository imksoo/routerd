# NAT44SessionSync

[リソース別の設定](../resources.md#nat44sessionsync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: "<name>"
  spec:
    conntrackCommand: "<string>" # 任意; string
    conntrackd: # 任意; object
      ignoreIPv4: # 任意; array
        - "<string>" # string
      interface: "<string>" # 必須; string
      localAddress: "<string>" # 必須; string
      peerAddress: "<string>" # 必須; string
      port: 1 # 任意; integer; minimum=1; maximum=65535
    excludeNatRules: # 任意; array
      - "<string>" # string
    interval: "<string>" # 任意; string
    mode: "event-stream" # 任意; string; enum=["event-stream", "conntrackd"]
    natRules: # 任意; array
      - "<string>" # string
    snatAddresses: # 任意; array
      - "<string>" # string
    targets: # 任意; array
      - # object
        host: "<string>" # 必須; string
        name: "<string>" # 任意; string
        restoreCommand: # 任意; array
          - "<string>" # string
        sshOptions: # 任意; array
          - "<string>" # string
        user: "<string>" # 任意; string
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
