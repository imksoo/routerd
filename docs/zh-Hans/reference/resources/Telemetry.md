# Telemetry

[各资源的配置](../resources.md#telemetry) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

为生成的受管服务设置OpenTelemetry环境变量，不定义LogSink或独立collector进程。

## 指定条件与引用关系

- 生成的遥测环境仅可选择一个Telemetry或OTLP endpoint非空的ObservabilityPipeline；同时选到多个会生成错误，不通过资源名互相引用。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: "<name>"
  spec:
    attributes: # 选填; object
      "<key>": "<string>" # 选填; string
    otlp: # 必填; object
      endpoint: "<string>" # 必填; string
      insecure: true # 选填; boolean
    serviceNamespace: "<string>" # 选填; string
    signals: # 选填; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.otlp` | 必填OTLP连接配置。 |
| `spec.otlp.endpoint` | 必填且不可空白的导出端点，写入OTEL_EXPORTER_OTLP_ENDPOINT，无默认值。验证器仅检查非空白，不确认可达性。 |
| `spec.otlp.insecure` | 默认false；true生成OTEL_EXPORTER_OTLP_INSECURE=true，false不生成此变量。 |
| `spec.serviceNamespace` | OpenTelemetry服务命名空间，去除首尾空白后为空时默认routerd。 |
| `spec.attributes` | 资源属性映射，默认空；按键排序并以逗号分隔key=value写入OTEL_RESOURCE_ATTRIBUTES，此生成器不转义值。 |
| `spec.signals` | 接受logs、metrics、traces，默认三者全部。非空列表会将省略信号的exporter变量设为none，不会创建接收进程。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/render/telemetry.go](https://github.com/imksoo/routerd/blob/main/pkg/render/telemetry.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `signals`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)
