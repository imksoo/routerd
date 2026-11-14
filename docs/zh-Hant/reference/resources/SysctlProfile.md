# SysctlProfile

[各資源的設定](../resources.md#sysctlprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

依名稱套用路由器用sysctl集合，並可逐鍵覆寫；不是依名稱參照Sysctl資源。

## 指定條件與參照關係

- 覆寫既有鍵只改變值，保留比較方式與optional。新增鍵採exact比較及optional=false。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: SysctlProfile
  metadata:
    name: "<name>"
  spec:
    [ overrides:
        [ <name>: <string> ]
    ]
    [ persistent: <boolean> ]
    profile: ("router-linux" | "router-freebsd")
    [ runtime: <boolean> ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.profile` | **必填** | string; enum=["router-linux", "router-freebsd"] | 必填router-linux或router-freebsd，不依OS自動選擇。FreeBSD集合啟用IPv4/IPv6轉送；Linux還包含佇列、socket、TCP及可選conntrack調整。完整鍵值請見實作連結。 |
| `spec.runtime` | *選填* | boolean | 將集合各項套用至目前核心；預設true。 |
| `spec.persistent` | *選填* | boolean | 為各項寫入持久sysctl.d檔案；預設false，與runtime獨立。 |
| `spec.overrides` | *選填* | object | 核心sysctl鍵至非空白字串值的對照，預設空；替換既有鍵值並新增其他鍵。拒絕含空白或/的鍵及空白值，不是資源參照。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/profile.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/profile.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `profile`, `dryRun`
