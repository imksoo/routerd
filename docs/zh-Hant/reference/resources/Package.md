# Package

[各資源的設定](../resources.md#package) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

確保所選OS套件已安裝，選取第一個os與執行主機完全相符的套件集合。

## 指定條件與參照關係

- state只支援present，不能移除套件。找不到相符OS集合時為Pending/NoMatchingOSPackageSet。集合的optional會被接受但controller不讀取，安裝失敗仍回傳錯誤。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Package
  metadata:
    name: "<name>"
  spec:
    packages: # 必填; array
      - # object
        manager: "" # 選填; string; enum=["", "apt", "dnf", "pkg"]
        names: # 必填; array
          - "<string>" # string
        optional: true # 選填; boolean
        os: "ubuntu" # 必填; string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"]
    state: "" # 選填; string; enum=["", "present"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.state` | 期望套件狀態，省略為present，拒絕其他值。 |
| `spec.packages` | 必填非空的OS套件集合清單，不是資源參照。 |
| `spec.packages[].os` | 必填OS識別：ubuntu、debian、fedora、rhel、rocky、almalinux或freebsd。Linux讀取/etc/os-release的ID，不依ID_LIKE匹配。 |
| `spec.packages[].manager` | ubuntu/debian預設apt，fedora/rhel/rocky/almalinux預設dnf，freebsd預設pkg；明確指定也必須符合此對應。 |
| `spec.packages[].names` | 必填非空套件名稱清單，拒絕空白名稱及含空白或/的名稱；此欄位不隱含新增其他名稱。 |
| `spec.packages[].optional` | 預設false；可解碼此欄位，但目前不影響PackageController的錯誤處理。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/package.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/package.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `packages`, `dryRun`
