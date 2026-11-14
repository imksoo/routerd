# FirewallEventLog

[各资源的配置](../resources.md#firewalleventlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # 选填; boolean
    events: # 选填; array
      - "deny" # string; enum=["deny", "allow", "rateLimit", "connLimit"]
    fromZones: # 选填; array
      - "<string>" # string
    log: # 选填; object
      acceptSampleRate: 0 # 选填; integer; minimum=0
      copyRange: 0 # 选填; integer; minimum=0
      dropSampleRate: 0 # 选填; integer; minimum=0
      reject: true # 选填; boolean
    nflogGroup: 0 # 选填; integer; minimum=0; maximum=65535
    path: "<string>" # 选填; string
    retention: "<string>" # 选填; string
    rules: # 选填; array
      - "<string>" # string
    sampleRate: 0 # 选填; integer; minimum=0
    sinks: # 选填; array
      - "<string>" # string
    toZones: # 选填; array
      - "<string>" # string
```
