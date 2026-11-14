# DHCPv6Server

[各資源的設定](../resources.md#dhcpv6server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

使用共用 dnsmasq 程序提供 LAN DHCPv6 位址或資訊服務及相關 RA 行為。

## 指定條件與參照關係

- 目前即時寫入與舊靜態轉譯不同：需要 interface，僅 delegatedAddress 不建立即時範圍，dnsSource/selfAddressPolicy 不提供自身 DNS，ra-only 亦走 stateless 分支。請用下列明確欄位並確認實際 DHCP/RA 流量。
- 一般驗證限制 delegatedAddress 的 mode 為 stateless/ra-only。沒有 delegatedAddress 時，所有非 stateless 模式（含 ra-only）均須有效 IPv6 起訖值。domainSearch/domainSearchFrom 互斥，DNS/SNTP 固定與動態來源可並用。
- 資源共用一個 dnsmasq，constructor 使用實際介面的 /64。Applied 僅記錄調和成功，不代表客戶端取得租約、收到 RA 或安裝預設路由；已移除 ready_when 會被拒絕。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 選填; object
      end: "<string>" # 選填; string
      leaseTime: "<string>" # 選填; string
      start: "<string>" # 選填; string
    configPath: "<string>" # 選填; string
    defaultRoute: true # 選填; boolean
    delegatedAddress: "<string>" # 選填; string
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    dnsServerFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    dnsServers: # 選填; array
      - "<string>" # string
    dnsSource: "self" # 選填; string; enum=["self", "static", "none"]
    domainSearch: # 選填; array
      - "<string>" # string
    domainSearchFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    interface: "<string>" # 選填; string
    leaseFile: "<string>" # 選填; string
    leaseTime: "<string>" # 選填; string
    listenInterfaces: # 選填; array
      - "<string>" # string
    logDHCP: true # 選填; boolean
    managed: true # 選填; boolean
    mode: "stateless" # 選填; string; enum=["stateless", "stateful", "both", "ra-only"]
    pidFile: "<string>" # 選填; string
    rapidCommit: true # 選填; boolean
    role: "server" # 選填; string; enum=["server", "transit"]
    selfAddressPolicy: "<string>" # 選填; string
    server: "dnsmasq" # 選填; string; enum=["dnsmasq"]
    sntpServerFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    sntpServers: # 選填; array
      - "<string>" # string
    stickyHoldDays: 0 # 選填; integer; minimum=0
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.server` | 選用 dnsmasq，預設亦為 dnsmasq，不接受其他後端；即時服務與 DHCPv4/RA 共用。 |
| `spec.managed` | 布林值預設 false，目前即時服務寫入器與控制器不以此停用變更。 |
| `spec.role` | server/transit，驗證與靜態轉譯預設 server；目前即時寫入不以 transit 抑制服務。 |
| `spec.listenInterfaces` | 選用已宣告邏輯介面，皆驗證存在。提供 delegatedAddress 且清單非空時，須包含該位址資源介面。用於位址族及防火牆規劃，但目前即時集區仍需 interface 本身。 |
| `spec.logDHCP` | 預設 false，靜態轉譯可輸出 log-dhcp，但目前即時寫入不使用。 |
| `spec.stickyHoldDays` | 非負天數，預設 0 停用。dnsmasq 正值將分配保留於 dhcp-sticky.db 供重用；執行時對全部 IPv6 事件採 IPv6 伺服器最大值，不依介面。DHCPv6ServerLeaseSync 不複製此資料庫。 |
| `spec.interface` | 已宣告的邏輯 LAN 介面，即使驗證允許僅有 delegatedAddress，目前即時寫入仍需要，省略則略過集區產生。 |
| `spec.delegatedAddress` | 選用既有 IPv6DelegatedAddress 裸名。靜態轉譯可衍生介面、前綴與自身 DNS，但目前即時寫入直接用 interface/constructor，不以此替代。設定後驗證限制 mode 為 stateless 或 ra-only。 |
| `spec.mode` | 預設 stateless；stateful 輸出 constructor 範圍，both 加 slaac。目前即時寫入將 ra-only 同樣送入 ra-stateless 預設分支，不能實際停用 DHCPv6 資訊服務。無 delegatedAddress 時驗證亦要求 ra-only 提供集區。 |
| `spec.addressPool` | IPv6 有狀態分配範圍，未有 delegatedAddress 且非 stateless 時必填；stateless 不作分配範圍，端點不自動補入。 |
| `spec.addressPool.start` | IPv6 起始位址，包含端點且 <= end；即時 stateful 範圍使用 constructor:<OS名稱> 與前綴長 64。 |
| `spec.addressPool.end` | IPv6 結束位址，包含端點且 >= start，與 start 配對。 |
| `spec.addressPool.leaseTime` | 後端期間，優先於 leaseTime；空值依 leaseTime、12h 補足，此處不按 Go duration 解析。 |
| `spec.defaultRoute` | 布林值預設 false，靜態轉譯可控制預設路由器期限，但目前即時寫入不讀取。DHCPv6 本身不發送 IPv6 預設路由。 |
| `spec.dnsSource` | 驗證 self/static/none，靜態預設 self，static 需 dnsServers。目前即時寫入忽略選擇器，僅用明確 dnsServers/dnsServerFrom，none 也不抑制這些明確選項。 |
| `spec.selfAddressPolicy` | 選用既有 SelfAddressPolicy 裸名，即使目前即時 LAN 產生不使用仍驗證存在；供舊靜態自身 DNS 選擇。 |
| `spec.dnsServers` | 選用固定 IPv6 DNS 清單，預設空，與 dnsServerFrom 合併輸出標籤式 option6:dns-server，不啟動 DNS 接聽。 |
| `spec.dnsServerFrom` | 選用共用 resource/field 來源清單，optional 預設 false；必要缺值即使有固定 DNS 仍為 Pending，此讀取器不完整驗證所有狀態值位址族便傳入。 |
| `spec.sntpServers` | 選用固定 IPv6 SNTP 清單，預設空，與 sntpServerFrom 合併輸出標籤式 option6:sntp-server，不啟動 NTP 常駐程式。 |
| `spec.sntpServerFrom` | 選用共用狀態來源清單，必要或選用阻擋行為同 dnsServerFrom，預設無參照。 |
| `spec.domainSearch` | 選用單一網域字串清單，預設空，不可含空白、斜線或逗號，與 domainSearchFrom 互斥，輸出標籤式 option6:domain-search。 |
| `spec.domainSearchFrom` | 選用來源限 DNSZone/<name>、field: zone，須存在，可先讀宣告值。必要缺值阻擋伺服器，optional true 略過缺值，與 domainSearch 互斥。 |
| `spec.leaseTime` | addressPool.leaseTime 之後的後端期間替代值，預設 12h。 |
| `spec.rapidCommit` | 布林值預設 false，目前輸出標籤式 option6:rapid-commit，不另輸出 dnsmasq dhcp-rapid-commit 指令，不能僅據此證明協商成兩訊息 Rapid Commit。 |
| `spec.configPath` | 選用解碼字串，預設空，目前控制器使用共用 dnsmasq 選項而非此資源路徑。 |
| `spec.pidFile` | 選用解碼字串，預設空，目前控制器使用共用 dnsmasq 選項而非此資源路徑。 |
| `spec.leaseFile` | 選用租約檔路徑，預設 <StateDir>/dnsmasq/dnsmasq.leases；單一服務共用，所有 DHCPv4/6 明確 leaseFile 須一致，否則即時產生失敗。 |
| `spec.dependsOn` | 接受共用相依清單並驗證參照，預設空。目前即時 DHCPv6 伺服器寫入與狀態路徑不評估此清單，不能作可靠服務閘門；動態值要求及 when 另行評估。 |
| `spec.when` | 選用共用條件，預設無條件；為假時自有效設定排除並為 Pending/WhenFalse。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [cmd/routerd/runtime_services.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/runtime_services.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
