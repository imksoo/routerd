# PPPoESession

[リソース別の設定](../resources.md#pppoesession) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    acName: "<string>" # 任意; string
    authMethod: "chap" # 任意; string; enum=["chap", "pap", "both"]
    defaultRoute: true # 任意; boolean
    enabled: true # 任意; boolean
    ifname: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    ipv6: true # 任意; boolean
    lcpEchoFailure: 0 # 任意; integer; minimum=0
    lcpEchoInterval: 0 # 任意; integer; minimum=0
    lcpFailure: 0 # 任意; integer; minimum=0
    lcpInterval: 0 # 任意; integer; minimum=0
    managed: true # 任意; boolean
    mru: 576 # 任意; integer; minimum=576; maximum=1500
    mtu: 576 # 任意; integer; minimum=576; maximum=1500
    password: "<string>" # 任意; string
    passwordFile: "<string>" # 任意; string
    persist: true # 任意; boolean
    secretEncoding: "plain" # 任意; string; enum=["plain"]
    serviceName: "<string>" # 任意; string
    usePeerDNS: true # 任意; boolean
    username: "<string>" # 必須; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)
