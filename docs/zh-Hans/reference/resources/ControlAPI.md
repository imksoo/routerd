# ControlAPI

[各资源的配置](../resources.md#controlapi) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

启用可读状态及变更配置的HTTP Control API，与Unix socket控制及WebConsole分开。

## 指定条件与引用关系

- 仅支持一个ControlAPI。显式HTTP CLI参数优先于对应资源配置；没有资源或启用参数时禁用HTTP控制。
- tls.certFile与tls.keyFile须一起指定；clientCAFile另须两者，并要求及验证客户端证书。tokenFrom.file与env互斥。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    allowCIDRs: # 选填; array
      - "<string>" # string
    enabled: true # 选填; boolean
    listenAddress: "<string>" # 选填; string
    port: 1 # 选填; integer; minimum=1; maximum=65535
    tls: # 选填; object
      certFile: "<string>" # 选填; string
      clientCAFile: "<string>" # 选填; string
      keyFile: "<string>" # 选填; string
    tokenFrom: # 选填; object
      base64: true # 选填; boolean
      env: "<string>" # 选填; string
      file: "<string>" # 选填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.enabled` | 资源存在时默认true，不存在时禁用；显式HTTP监听CLI参数会启用。 |
| `spec.listenAddress` | 绑定IP，默认127.0.0.1；非空白值须为IP，不会自行变更来源CIDR允许列表。 |
| `spec.port` | TCP端口，默认65432；解码值0也用默认，其余须为1–65535。 |
| `spec.allowCIDRs` | 来源IP允许列表，默认127.0.0.1/32与::1/128，非空列表会替换默认。须为有效CIDR，拒绝0.0.0.0/0与::/0，与防火墙规则不同。 |
| `spec.tokenFrom` | 选填Bearer token来源；省略不检查token，但仍限制来源CIDR。使用时仅指定file或env其中一项，不是routerd资源引用。 |
| `spec.tokenFrom.file` | token文件路径，默认空，与env互斥；HTTP API初始化时由routerd读取，无法读取则初始化失败。 |
| `spec.tokenFrom.env` | routerd进程环境变量名，默认空，与file互斥；变量不存在则HTTP API初始化失败。 |
| `spec.tokenFrom.base64` | 默认false。true时先修剪来源，再用标准Base64解码并再次修剪；格式错误或最终空token使初始化失败。 |
| `spec.tls` | 选填服务器TLS配置，未指定证书/密钥时监听为明文HTTP。 |
| `spec.tls.certFile` | 服务器证书文件，默认未指定，须搭配keyFile。启动时载入X.509密钥对，最低TLS 1.2。 |
| `spec.tls.keyFile` | 服务器私钥文件，默认未指定，须搭配certFile；为本地文件路径，不是资源引用。 |
| `spec.tls.clientCAFile` | 选填PEM客户端CA集合，须搭配certFile/keyFile并强制验证客户端证书；文件缺少或无效则启动失败。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/controlapi/server.go](https://github.com/imksoo/routerd/blob/main/pkg/controlapi/server.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

此 Kind 未登记常规状态引用的允许字段。

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)
