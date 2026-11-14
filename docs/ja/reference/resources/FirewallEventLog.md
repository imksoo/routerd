# FirewallEventLog

[リソース別の設定](../resources.md#firewalleventlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # 任意; boolean
    events: # 任意; array
      - "deny" # string; enum=["deny", "allow", "rateLimit", "connLimit"]
    fromZones: # 任意; array
      - "<string>" # string
    log: # 任意; object
      acceptSampleRate: 0 # 任意; integer; minimum=0
      copyRange: 0 # 任意; integer; minimum=0
      dropSampleRate: 0 # 任意; integer; minimum=0
      reject: true # 任意; boolean
    nflogGroup: 0 # 任意; integer; minimum=0; maximum=65535
    path: "<string>" # 任意; string
    retention: "<string>" # 任意; string
    rules: # 任意; array
      - "<string>" # string
    sampleRate: 0 # 任意; integer; minimum=0
    sinks: # 任意; array
      - "<string>" # string
    toZones: # 任意; array
      - "<string>" # string
```
