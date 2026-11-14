# SAMEnrollmentClient

[各资源的配置](../resources.md#samenrollmentclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

将本机注册请求提交至引导 RR ControlAPI，取得允许的拓扑，保存为动态 SAMRRSet 及可选 direct 对等组供已有控制器使用，不创建云端基础设施。

## 指定条件与引用关系

- claimRef 须解析为本机 SAMEnrollmentClaim，运行时该请求的 rrSetRef 须为 SAMRRSet/name。leaf 请求可引用远端策略，不需在本机复制 SAMEnrollmentPolicy。
- 至少需一个 bootstrapEndpoints 或 rrSocket，两者不互斥。先使用 Unix 套接字，再依次使用 HTTP(S) 端点。普通接纳可使用成功的 RR 响应；direct 拓扑要求配置端点间一致，无法安全接纳时保留 RR 路径。
- 禁用 direct-mesh 时，会先移除缓存 direct 拓扑，再联系可能无法连接的 RR。客户端重试/更新状态会保存，拓扑更新不代表隧道或 BGP 已运行。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClient
  metadata:
    name: "<name>"
  spec:
    [ bootstrapEndpoints:
        - <string>
    ]
    claimRef: <string>
    [ controlAPITLS:
        [ caFile: <string> ]
        [ certFile: <string> ]
        [ insecureSkipVerify: <boolean> ]
        [ keyFile: <string> ]
        [ serverName: <string> ]
    ]
    [ controlAPITokenFrom:
        [ base64: <boolean> ]
        [ env: <string> ]
        [ file: <string> ]
    ]
    [ retryBackoff:
        [ max: <string> ]
        [ min: <string> ]
    ]
    [ rrSocket: <string> ]
    [ stateTTLRefreshBefore: <string> ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.claimRef` | **必填** | string | 必填同一 Router 声明的 SAMEnrollmentClaim/name，不使用状态字段选择器。 |
| `spec.bootstrapEndpoints` | **条件必填** | array | 可选且有顺序的绝对 HTTP(S) ControlAPI URL，须含主机。未提供 rrSocket 时至少一个，不自动发现。每个注册请求目前使用 10 秒请求期限。 |
| `spec.controlAPITokenFrom` | *选填* | object | 仅供 HTTP(S) 客户端使用的可选 Bearer 令牌来源。省略不传 Bearer 标头，提供时 file/env 只能指定一项，与请求的 join HMAC 凭据不同。 |
| `spec.controlAPITokenFrom.file` | **条件必填** | string | 路由器本机原始令牌文件，与 env 互斥。会移除首尾空白；配置 HTTP 端点时，缺失、无法读取或为空会失败。 |
| `spec.controlAPITokenFrom.env` | **条件必填** | string | 存放令牌的 routerd 进程环境变量名称，与 file 互斥，不是环境文件路径。 |
| `spec.controlAPITokenFrom.base64` | *选填* | boolean | 默认 false，true 执行标准 Base64 解码并移除解码后首尾空白，不是加密。 |
| `spec.controlAPITLS` | *选填* | object | 可选 HTTPS 客户端配置，不用于 Unix 套接字，不会将 http URL 自动转成 https。 |
| `spec.controlAPITLS.caFile` | *选填* | string | 可选 PEM CA 文件，提供时以文件内证书替换客户端根证书集；省略使用普通信任配置。 |
| `spec.controlAPITLS.certFile` | *选填* | string | 可选客户端证书文件，须与 keyFile 同时配置，没有默认证书或自动生成的凭据。 |
| `spec.controlAPITLS.keyFile` | *选填* | string | 可选客户端私钥文件，须与 certFile 同时配置。 |
| `spec.controlAPITLS.serverName` | *选填* | string | 可选 TLS SNI/验证名称覆盖，空值依 URL 主机行为，不更改端点路由。 |
| `spec.controlAPITLS.insecureSkipVerify` | *选填* | boolean | 默认 false。true 禁用服务器证书链及主机名验证，对纯 HTTP URL 无效。 |
| `spec.rrSocket` | **条件必填** | string | 可选本机 RR Unix 套接字绝对路径，可与 HTTP(S) 端点并用。Unix 客户端不使用 Bearer 令牌或 TLS。 |
| `spec.stateTTLRefreshBefore` | *选填* | string | 提前更新的 Go duration，默认 10 分钟。若大于等于观察到的正 RR 租约长度，运行时改用一半租约以避免立即重复更新。验证仅检查可解析性，不强制正值，请用正值。 |
| `spec.retryBackoff` | *选填* | object | 可选指数重试配置，失败时由 min 倍增至 max。direct 拓扑恢复另有较短的收敛重试路径。 |
| `spec.retryBackoff.min` | *选填* | string | Go duration，默认 10 秒。仅验证可解析性，接受零或负值；请用正值避免立即或排在过去的重试。 |
| `spec.retryBackoff.max` | *选填* | string | Go duration，默认 15 分钟。min/max 都明确指定时要求 max>=min，请用正值。验证比较不纳入省略字段的默认值。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go) · [pkg/controller/mobility/core_helpers.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/core_helpers.go) · [pkg/controlapi/client.go](https://github.com/imksoo/routerd/blob/main/pkg/controlapi/client.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `claimRef`, `observedRRSet`, `observedDirectPeerGroup`, `lastAttempt`, `lastSuccess`, `nextAttempt`, `backoff`, `failureCount`, `directTopologyPending`, `reason`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
