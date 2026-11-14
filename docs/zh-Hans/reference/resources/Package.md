# Package

[各资源的配置](../resources.md#package) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

确保所选OS软件包已安装，选择第一个os与运行主机完全匹配的软件包集合。

## 指定条件与引用关系

- state仅支持present，不能移除软件包。找不到匹配OS集合时为Pending/NoMatchingOSPackageSet。集合的optional会被接受但controller不读取，安装失败仍返回错误。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: Package
  metadata:
    name: "<name>"
  spec:
    packages:
      -
        [ manager: ("" | "apt" | "dnf" | "pkg") ]
        names:
          - <string>
        [ optional: <boolean> ]
        os: ("ubuntu" | "debian" | "fedora" | "rhel" | "rocky" | "almalinux" | "freebsd")
    [ state: ("" | "present") ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.state` | *选填* | string; enum=["", "present"] | 期望软件包状态，省略为present，拒绝其他值。 |
| `spec.packages` | **必填** | array | 必填非空的OS软件包集合列表，不是资源引用。 |
| `spec.packages[].os` | **必填** | string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"] | 必填OS标识：ubuntu、debian、fedora、rhel、rocky、almalinux或freebsd。Linux读取/etc/os-release的ID，不按ID_LIKE匹配。 |
| `spec.packages[].manager` | *选填* | string; enum=["", "apt", "dnf", "pkg"] | ubuntu/debian默认apt，fedora/rhel/rocky/almalinux默认dnf，freebsd默认pkg；显式指定也必须符合此映射。 |
| `spec.packages[].names` | **必填** | array | 必填非空软件包名列表，拒绝空白名称及含空白或/的名称；此字段不隐含新增其他名称。 |
| `spec.packages[].optional` | *选填* | boolean | 默认false；可解码此字段，但当前不影响PackageController的错误处理。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/package.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/package.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `packages`, `dryRun`
