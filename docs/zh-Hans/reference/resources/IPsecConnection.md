# IPsecConnection

[各资源的配置](../resources.md#ipsecconnection) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

生成 strongSwan swanctl 连接，并统一同步所有 routerd IPsec 连接。CHILD_SA 设为流量触发的 trap；加载配置不代表 SA 已建立。

## 指定条件与引用关系

- preSharedKey 与 certificateRef 必须且只能指定一项。certificateRef 直接传入 swanctl local.certs，不解析为 routerd 资源。
- 汇总配置位于 Linux 的 /etc/routerd/swanctl 或 FreeBSD 的 /usr/local/etc/routerd/swanctl。swanctl --load-all --file 同步整个管理集合与密钥。证书、信任锚点及配对私钥须另行放入该配置使用的凭据存储区。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    [ certificateRef: <string> ]
    [ cloudProviderHint: ("" | "aws" | "azure" | "gcp") ]
    leftSubnet: <string>
    localAddress: <string>
    [ phase1Proposals:
        - <string>
    ]
    [ phase2Proposals:
        - <string>
    ]
    [ preSharedKey: <string> ]
    remoteAddress: <string>
    rightSubnet: <string>
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.localAddress` | **必填** | string | 必填的本机端点 IPv4/IPv6 地址，不接受主机名或接口引用。 |
| `spec.remoteAddress` | **必填** | string | 必填的远端 IPv4/IPv6 地址。验证不强制两端与流量选择器的地址族一致。 |
| `spec.preSharedKey` | **条件必填** | string | 直接指定 IKE 预共享密钥；未使用 certificateRef 时必填。无默认值、文件或环境来源，会写入管理的 swanctl secrets 段。 |
| `spec.certificateRef` | **条件必填** | string | strongSwan local.certs 使用的证书名称或路径；未使用 preSharedKey 时必填，远端认证设为 pubkey。不创建证书或引用 routerd Secret。 |
| `spec.phase1Proposals` | *选填* | array | IKE 提案列表，默认 [aes256-sha256-modp2048]。非空列表直接以逗号连接，routerd 不验证算法支持。 |
| `spec.phase2Proposals` | *选填* | array | ESP 提案列表，默认 [aes256-sha256]，应用至名为 net 的子连接；算法有效性由 strongSwan 加载时检查。 |
| `spec.leftSubnet` | **必填** | string | 必填的本机流量选择器 IPv4/IPv6 CIDR，输出为 local_ts。 |
| `spec.rightSubnet` | **必填** | string | 必填的远端流量选择器 IPv4/IPv6 CIDR，输出为 remote_ts。 |
| `spec.cloudProviderHint` | *选填* | string; enum=["", "aws", "azure", "gcp"] | 可选的 aws、azure 或 gcp 标签，默认为空。仅供观察报告，不更改提案、凭据或 swanctl 生成内容，不创建云端资源。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/ipsec/ipsec.go](https://github.com/imksoo/routerd/blob/main/pkg/ipsec/ipsec.go) · [cmd/routerd/ipsec_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/ipsec_apply.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`
