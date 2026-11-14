# TailscaleNode

[リソース別の設定](../resources.md#tailscalenode) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TailscaleNode
  metadata:
    name: "<name>"
  spec:
    acceptDNS: true # 任意; boolean
    acceptRoutes: true # 任意; boolean
    advertiseExitNode: true # 任意; boolean
    advertiseRoutes: # 任意; array
      - "<string>" # string
    advertiseTags: # 任意; array
      - "<string>" # string
    authKey: "<string>" # 任意; string
    authKeyEnv: "<string>" # 任意; string
    authKeyFile: "<string>" # 任意; string
    hostname: "<string>" # 任意; string
    loginServer: "<string>" # 任意; string
    shieldsUp: true # 任意; boolean
    ssh: true # 任意; boolean
    state: "" # 任意; string; enum=["", "present", "absent"]
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/tailscale-exit-subnet.yaml)
