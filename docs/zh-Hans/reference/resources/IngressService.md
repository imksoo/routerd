# IngressService

[各资源的配置](../resources.md#ingressservice) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends: # 必填; array
      - # object
        address: "<string>" # 选填; string
        addressFrom: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        name: "<string>" # 选填; string
        port: 1 # 必填; integer; minimum=1; maximum=65535
        weight: 0 # 选填; integer; minimum=0
    externalDNS: true # 选填; boolean
    hairpin: # 选填; object
      enabled: true # 选填; boolean
      interfaces: # 选填; array
        - "<string>" # string
      mode: "" # 选填; string; enum=["", "auto", "manual", "off"]
    healthCheck: # 选填; object
      expectedBody: "<string>" # 选填; string
      expectedStatus: # 选填; array
        - 0 # integer
      healthyThreshold: 1 # 选填; integer; minimum=1
      host: "<string>" # 选填; string
      interval: "<string>" # 选填; string
      path: "<string>" # 选填; string
      protocol: "" # 选填; string; enum=["", "tcp", "http", "https"]
      timeout: "<string>" # 选填; string
      tlsSkipVerify: true # 选填; boolean
      unhealthyThreshold: 1 # 选填; integer; minimum=1
    hostname: "<string>" # 选填; string
    listen: # 必填; object
      address: "<string>" # 选填; string
      addressFrom: # 选填; object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
      interface: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必填; string; enum=["tcp", "udp"]
    policy: # 选填; object
      onNoHealthyBackends: "" # 选填; string; enum=["", "drop", "reject"]
      selection: "" # 选填; string; enum=["", "failover", "sourceHash", "random"]
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
```

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `hostname`, `listenAddress`, `activeBackend`, `activeBackends`, `healthyBackends`, `totalBackends`, `backends`, `observedAt`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)
