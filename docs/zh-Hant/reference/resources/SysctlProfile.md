# SysctlProfile

[各資源的設定](../resources.md#sysctlprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

依名稱套用路由器用sysctl集合，並可逐鍵覆寫；不是依名稱參照Sysctl資源。

## 指定條件與參照關係

- 覆寫既有鍵只改變值，保留比較方式與optional。新增鍵採exact比較及optional=false。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: SysctlProfile
  metadata:
    name: "<name>"
  spec:
    overrides: # 選填; object
      "<key>": "<string>" # 選填; string
    persistent: true # 選填; boolean
    profile: "router-linux" # 必填; string; enum=["router-linux", "router-freebsd"]
    runtime: true # 選填; boolean
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.profile` | 必填router-linux或router-freebsd，不依OS自動選擇。FreeBSD集合啟用IPv4/IPv6轉送；Linux還包含佇列、socket、TCP及可選conntrack調整。完整鍵值請見實作連結。 |
| `spec.runtime` | 將集合各項套用至目前核心；預設true。 |
| `spec.persistent` | 為各項寫入持久sysctl.d檔案；預設false，與runtime獨立。 |
| `spec.overrides` | 核心sysctl鍵至非空白字串值的對照，預設空；替換既有鍵值並新增其他鍵。拒絕含空白或/的鍵及空白值，不是資源參照。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/profile.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/profile.go)
