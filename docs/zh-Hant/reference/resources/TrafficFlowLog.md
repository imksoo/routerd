# TrafficFlowLog

[各資源的設定](../resources.md#trafficflowlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

將連線快照記錄至 SQLite 流量資料庫，並可從 DPI 快取補充資訊。不是封包擷取，也不記錄每個封包。

## 指定條件與參照關係

- 觀察器選擇第一個可解碼的 TrafficFlowLog，即使它已停用；後續啟用的資源不會覆蓋。請設定單一資源以確定記錄行為。
- 成功觀察 conntrack 後才記錄；觀察不可用時該輪不記錄。連線查詢上限為 10000 筆。每次觀察器協調時取樣，通常每 30 秒一次，可由執行選項調整。
- DPI 補充會讀取第一個啟用的 FirewallEventLog 資料庫，使用一小時內符合連線組合的分類快取。include 旗標不保證成功補充；兩者皆 false 時仍可使用可用快取。保留期間由 LogRetention 的 trafficFlows 管理。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: TrafficFlowLog
  metadata:
    name: "<name>"
  spec:
    [ enabled: <boolean> ]
    [ includeApplicationLayer: <boolean> ]
    [ includeTLSSNI: <boolean> ]
    [ path: <string> ]
    [ source: ("" | "conntrack") ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.enabled` | *選填* | boolean | 預設 false。true 啟用所選資源的記錄，不會使不可用的連線來源變為可用。 |
| `spec.path` | **條件必填** | string | 路由器上的 SQLite 資料庫路徑，正常驗證在 enabled 時必填。讀取器有內部 /var/lib/routerd/traffic-flows.db 後備值，但不免除啟用時的路徑要求。 |
| `spec.source` | *選填* | string; enum=["", "conntrack"] | 僅接受省略或 conntrack，實際標籤預設 conntrack。不用於選擇其他收集器或任意後端名稱。 |
| `spec.includeApplicationLayer` | *選填* | boolean | 預設 false。與 enabled 同為 true 時要求 DPI 輔助服務及 Linux DPI 觀察規則，並在資源狀態的 applicationLayer 記錄 nDPI 可用性。不保證 libndpi 載入或分類存在。 |
| `spec.includeTLSSNI` | *選填* | boolean | 預設 false。與 enabled 同為 true 時要求相同 DPI 輔助程式及觀察流程。不是 TLS 解密、SNI 擷取保證或獨立儲存遮蔽／篩選開關；加密或未觀察到的名稱可能不可用。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/controller/conntrackobserver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/conntrackobserver/controller.go) · [pkg/render/dpi_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dpi_systemd.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `path`, `sinks`
