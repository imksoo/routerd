# WebConsole

[各资源的配置](../resources.md#webconsole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

由routerd提供只读Web控制台，HTTP监听与可变更配置的ControlAPI不同。

## 指定条件与引用关系

- runtime选择第一个WebConsole，不为每项分别开启监听。listenAddressFrom解析值优先，listenAddress可作备用，两者不互斥。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    [ basePath: <string> ]
    [ enabled: <boolean> ]
    [ links:
        -
          [ description: <string> ]
          label: <string>
          url: <string>
    ]
    [ listenAddress: <string> ]
    [ listenAddressFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ port: <integer> ]
    [ title: <string> ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.enabled` | *选填* | boolean | 存在WebConsole资源时默认true；false禁用所选控制台，没有资源则不开启监听。 |
| `spec.listenAddress` | *选填* | string | 备用绑定地址，默认127.0.0.1；非空白值须为IP，listenAddressFrom解析值优先。 |
| `spec.listenAddressFrom` | *选填* | object | 绑定IP来源，默认未指定；先读状态，再尝试支持的配置接口/地址解析。须有resource与field；无法解析且无listenAddress时，即使optional=true仍无法启动。 |
| `spec.port` | *选填* | integer; minimum=1; maximum=65535 | TCP端口，默认8080；解码后0也选默认，非零值须为1–65535。 |
| `spec.basePath` | *选填* | string | HTTP路径前缀，默认/；非空值须以/开头，不可含NUL或换行。 |
| `spec.title` | *选填* | string | 控制台页面标题，默认routerd，不会重命名路由器或资源。 |
| `spec.links` | *选填* | array | 额外控制台链接，默认空；label或URL空白时显示阶段跳过。 |
| `spec.links[].label` | **条件必填** | string | 链接文字，无默认值，空白不显示。 |
| `spec.links[].url` | **条件必填** | string | 链接目标URL，无默认值，不是Kind/名称引用，会去除首尾空白。 |
| `spec.links[].description` | *选填* | string | 选填说明，默认空，显示时去除首尾空白。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/webconsole/console.go](https://github.com/imksoo/routerd/blob/main/pkg/webconsole/console.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `listenAddress`, `port`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
