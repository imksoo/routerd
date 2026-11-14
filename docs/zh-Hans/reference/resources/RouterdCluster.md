# RouterdCluster

[各资源的配置](../resources.md#routerdcluster) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

通过文件租约与OS文件锁协调哪个routerd可以变更状态，不是VRRP选举或对端网络发现。

## 指定条件与引用关系

- 参与者需要共享且锁定语义可靠的leasePath。peers是标识而非端点；租约实现仅用非空列表启用协调，不连接对端或验证成员资格。待机controller以dry-run变更路径运行。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    identity: "<string>" # 选填; string
    leasePath: "<string>" # 选填; string
    leaseTTL: "<string>" # 选填; string
    peers: # 必填; array
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
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.peers` | 去除空白与重复后至少两项的必填列表，无默认值；不是Kind/名称引用或实际连接地址。 |
| `spec.leaseTTL` | 租约期限默认30s，显式值须为5s–10m的Go duration。互斥变更仍由锁控制，期限本身不是网络故障检测器。 |
| `spec.leasePath` | 必填租约文件绝对路径，不可含换行，无默认值；各主机独立文件不会协调主机间状态。 |
| `spec.identity` | 本机租约持有者标识，省略使用修剪后os.Hostname()，不是Router.metadata.name。 |
| `spec.when` | 启用条件，省略不设限制，请见共用条件参考。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/ha/lease.go](https://github.com/imksoo/routerd/blob/main/pkg/ha/lease.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `leader`, `leaseExpiresAt`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)
