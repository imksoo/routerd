# Package

[各资源的配置](../resources.md#package) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

确保所选OS软件包已安装，选择第一个os与运行主机完全匹配的软件包集合。

## 指定条件与引用关系

- state仅支持present，不能移除软件包。找不到匹配OS集合时为Pending/NoMatchingOSPackageSet。集合的optional会被接受但controller不读取，安装失败仍返回错误。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Package
  metadata:
    name: "<name>"
  spec:
    packages: # 必填; array
      - # object
        manager: "" # 选填; string; enum=["", "apt", "dnf", "pkg"]
        names: # 必填; array
          - "<string>" # string
        optional: true # 选填; boolean
        os: "ubuntu" # 必填; string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"]
    state: "" # 选填; string; enum=["", "present"]
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.state` | 期望软件包状态，省略为present，拒绝其他值。 |
| `spec.packages` | 必填非空的OS软件包集合列表，不是资源引用。 |
| `spec.packages[].os` | 必填OS标识：ubuntu、debian、fedora、rhel、rocky、almalinux或freebsd。Linux读取/etc/os-release的ID，不按ID_LIKE匹配。 |
| `spec.packages[].manager` | ubuntu/debian默认apt，fedora/rhel/rocky/almalinux默认dnf，freebsd默认pkg；显式指定也必须符合此映射。 |
| `spec.packages[].names` | 必填非空软件包名列表，拒绝空白名称及含空白或/的名称；此字段不隐含新增其他名称。 |
| `spec.packages[].optional` | 默认false；可解码此字段，但当前不影响PackageController的错误处理。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/package.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/package.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)
