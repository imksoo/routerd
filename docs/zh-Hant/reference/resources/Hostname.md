# Hostname

[各資源的設定](../resources.md#hostname) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

管理OS主機名稱，與Router.metadata.name及資源名稱不同。

## 指定條件與參照關係

- 多個受管Hostname在協調時失敗；managed=false項目被controller忽略。Linux使用hostnamectl，FreeBSD使用sysrc與hostname。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: Hostname
  metadata:
    name: "<name>"
  spec:
    hostname: <string>
    [ managed: <boolean> ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.hostname` | **必填** | string | 必填非空OS主機名稱，無預設值，拒絕空格、tab、換行與/；不是DNSZone或Interface參照。 |
| `spec.managed` | *選填* | boolean | 預設false，不變更主機名稱。true觀察目前/靜態名稱並按需變更；dry-run不呼叫變更命令。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/hostname.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hostname.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `hostname`
