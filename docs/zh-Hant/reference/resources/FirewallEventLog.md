# FirewallEventLog

[各資源的設定](../resources.md#firewalleventlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

啟用防火牆封包紀錄收集器，設定資料庫、Linux NFLOG 群組與規則產生時的紀錄選項。部分接受的篩選與封存欄位目前沒有執行期使用者。

## 指定條件與參照關係

- enabled 預設 false。Linux 從 NFLOG 收集，FreeBSD 使用 pflog0。此設定不會讓所有允許或拒絕規則都記錄；FirewallRule.log 與 FirewallPolicy.logDeny 也決定哪些封包被記錄。
- 請只啟用一個資源：規則產生使用第一個啟用資源的選項，服務產生則共用 routerd-firewall-logger 名稱。多個資源名稱不會建立獨立收集器。
- fromZones/toZones 須為既有 FirewallZone 名稱或 self；rules 接受 FirewallRule/name 或單純規則名稱；sinks 接受 LogSink/name 或單純輸出名稱；retention 接受 LogRetention/name 或單純名稱。雖然目前不會用這些欄位篩選、匯出或選擇保留規則，仍會驗證參照。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # 選填; boolean
    events: # 選填; array
      - "deny" # string; enum=["deny", "allow", "rateLimit", "connLimit"]
    fromZones: # 選填; array
      - "<string>" # string
    log: # 選填; object
      acceptSampleRate: 0 # 選填; integer; minimum=0
      copyRange: 0 # 選填; integer; minimum=0
      dropSampleRate: 0 # 選填; integer; minimum=0
      reject: true # 選填; boolean
    nflogGroup: 0 # 選填; integer; minimum=0; maximum=65535
    path: "<string>" # 選填; string
    retention: "<string>" # 選填; string
    rules: # 選填; array
      - "<string>" # string
    sampleRate: 0 # 選填; integer; minimum=0
    sinks: # 選填; array
      - "<string>" # string
    toZones: # 選填; array
      - "<string>" # string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.enabled` | 預設 false。true 要求收集服務並啟用群組紀錄選項；目前服務產生也會要求 DPI 分類輔助程式。 |
| `spec.path` | 選用 SQLite 資料庫路徑。Linux 預設 /var/lib/routerd/firewall-logs.db，FreeBSD 為 /var/db/routerd/firewall-logs.db。這是資料庫，不是逐行 JSON 檔案。 |
| `spec.events` | 接受 deny、allow、rateLimit、connLimit，預設空。目前僅驗證，不篩選收集輸入或產生的防火牆紀錄。 |
| `spec.fromZones` | 預設空。來源區域須為 FirewallZone 名稱或 self，但執行時不作紀錄篩選。 |
| `spec.fromZones[]` | 既有 FirewallZone 的 metadata.name 或特殊 self 區域，不是 Kind/name。目前不影響篩選。 |
| `spec.toZones` | 預設空。目的區域須為 FirewallZone 名稱或 self，但執行時不作紀錄篩選。 |
| `spec.rules` | 預設空。指定時須參照既有 FirewallRule（單純名稱或 FirewallRule/name），目前不選擇記錄規則。請設定實際規則的 FirewallRule.log。 |
| `spec.sampleRate` | 非負整數，預設 0；收集器與規則產生器均未使用。支援的產生控制請用 log.acceptSampleRate/log.dropSampleRate。 |
| `spec.sinks` | 預設空。會驗證既有 LogSink 參照，但收集器不會匯出至這些來源，輸出為 path 資料庫。 |
| `spec.retention` | 選用的既有 LogRetention 參照，預設空；目前不讀取以選擇保留規則。請在 LogRetention 設定 firewallEvents 訊號以處理此資料庫。 |
| `spec.nflogGroup` | 整數 0–65535；Linux 省略或 0 時，nftables 與收集器都使用群組 1。FreeBSD 使用 pflog0，忽略此群組。 |
| `spec.log` | 選用的規則產生紀錄選項，省略欄位為 0/false。這些選項不會實作未使用的頂層事件、區域或規則篩選。 |
| `spec.log.acceptSampleRate` | 預設 0 停用額外的隱含允許紀錄。nftables 正 N 約記錄合格封包的 1/N（1 全部記錄）。PF 只把正值當啟用開關，不做 1/N 抽樣。明確的 FirewallRule.log 另行控制。 |
| `spec.log.dropSampleRate` | 預設 0。對已啟用的隱含拒絕紀錄，nftables 0/1 全部記錄，N>1 約抽樣 1/N。此值不會自行啟用 FirewallPolicy.logDeny。PF 不使用此抽樣值。 |
| `spec.log.copyRange` | 非負的 NFLOG snaplen 位元組數。預設 0 省略 nftables snaplen 參數；正值限制複製的封包長度。PF 不套用。 |
| `spec.log.reject` | 預設 false；目前規則產生器與收集器不讀取。不改變規則裁決，也不啟用 reject 紀錄。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/render/firewall_logger.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_logger.go) · [pkg/render/freebsd_rcd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd_rcd.go) · [pkg/render/dpi_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dpi_systemd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)
