# CloudProviderProfile

[各资源的配置](../resources.md#cloudproviderprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

描述规划提供商操作所用的身份及目标元数据默认值。此声明本身不进行云端认证或创建资源。

## 指定条件与引用关系

- provider、非空 capabilities、auth.mode 与 auth.command 必填。验证会检查声明，但目前执行流程不执行 auth.command。提供商变更由受 ProviderActionPolicy 控制的独立执行 Plugin 处理。
- Mobility capture 等使用端以 metadata.name 选择配置，不是通用状态值来源。subscriptionID/resourceGroup 只补足未配置的操作目标值，不覆盖明确值。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: CloudProviderProfile
  metadata:
    name: "<name>"
  spec:
    auth:
      command: <string>
      mode: ("external-command")
    capabilities:
      - <string>
    provider: ("azure" | "aws" | "oci" | "gcp")
    [ resourceGroup: <string> ]
    [ subscriptionID: <string> ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.provider` | **必填** | string; enum=["azure", "aws", "oci", "gcp"] | 必填 azure/aws/oci/gcp，无默认值。选择规划／执行器标签，不会自动发现凭证。 |
| `spec.subscriptionID` | *选填* | string | 可选的目标订阅 ID，默认空。仅在目标缺少 subscriptionId 时补入，不检查存在或登录。 |
| `spec.resourceGroup` | *选填* | string | 可选的操作目标 resourceGroup 默认值，默认空。不创建或验证资源组。 |
| `spec.capabilities` | **必填** | array | 必填非空字符串列表，不可含空条目。目前验证不限制为已知能力词汇，执行策略也不将它视为操作允许列表。 |
| `spec.auth` | **必填** | object | 必填的认证描述。目前实现仅验证，不调用命令。 |
| `spec.auth.mode` | **必填** | string; enum=["external-command"] | 必填 external-command，唯一接受的模式。不会将 auth.command 启动为服务。 |
| `spec.auth.command` | **必填** | string | 必填非空声明，无默认值。目前执行流程不执行，也不传递由此取得的凭证。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/controller/mobility/planner.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/planner.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `provider`, `capabilities`
