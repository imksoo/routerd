# Package

[各資源的設定](../resources.md#package) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

確保所選OS套件已安裝，選取第一個os與執行主機完全相符的套件集合。

## 指定條件與參照關係

- state只支援present，不能移除套件。找不到相符OS集合時為Pending/NoMatchingOSPackageSet。集合的optional會被接受但controller不讀取，安裝失敗仍回傳錯誤。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

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

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.state` | *選填* | string; enum=["", "present"] | 期望套件狀態，省略為present，拒絕其他值。 |
| `spec.packages` | **必填** | array | 必填非空的OS套件集合清單，不是資源參照。 |
| `spec.packages[].os` | **必填** | string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"] | 必填OS識別：ubuntu、debian、fedora、rhel、rocky、almalinux或freebsd。Linux讀取/etc/os-release的ID，不依ID_LIKE匹配。 |
| `spec.packages[].manager` | *選填* | string; enum=["", "apt", "dnf", "pkg"] | ubuntu/debian預設apt，fedora/rhel/rocky/almalinux預設dnf，freebsd預設pkg；明確指定也必須符合此對應。 |
| `spec.packages[].names` | **必填** | array | 必填非空套件名稱清單，拒絕空白名稱及含空白或/的名稱；此欄位不隱含新增其他名稱。 |
| `spec.packages[].optional` | *選填* | boolean | 預設false；可解碼此欄位，但目前不影響PackageController的錯誤處理。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/package.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/package.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `packages`, `dryRun`
