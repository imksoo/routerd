# Hostname

[各資源的設定](../resources.md#hostname) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

管理OS主機名稱，與Router.metadata.name及資源名稱不同。

## 指定條件與參照關係

- 多個受管Hostname在協調時失敗；managed=false項目被controller忽略。Linux使用hostnamectl，FreeBSD使用sysrc與hostname。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Hostname
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # 必填; string
    managed: true # 選填; boolean
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.hostname` | 必填非空OS主機名稱，無預設值，拒絕空格、tab、換行與/；不是DNSZone或Interface參照。 |
| `spec.managed` | 預設false，不變更主機名稱。true觀察目前/靜態名稱並按需變更；dry-run不呼叫變更命令。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/hostname.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hostname.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `hostname`
