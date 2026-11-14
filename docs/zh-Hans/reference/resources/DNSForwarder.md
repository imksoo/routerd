# DNSForwarder

[各资源的配置](../resources.md#dnsforwarder) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    dnssecValidate: true # 选填; boolean
    healthcheck: # 选填; object
      failThreshold: 1 # 选填; integer; minimum=1
      interval: "<string>" # 选填; string
      passThreshold: 1 # 选填; integer; minimum=1
      timeout: "<string>" # 选填; string
    match: # 必填; array
      - "<string>" # string
    queryTimeout: "<string>" # 选填; string
    resolver: "<string>" # 必填; string
    upstreams: # 选填; array
      - "<string>" # string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
    zoneRefs: # 选填; array
      - "<string>" # string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
