# FirewallRule

[各資源的設定](../resources.md#firewallrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

宣告區域路徑的具名允許、丟棄或拒絕規則，支援位址、協定、連接埠比對及 Linux 速率/連線條件。

## 指定條件與參照關係

- fromZone、toZone、action 必填。區域須存在或為 self。雖然 fromZone=self 可通過驗證，這些產生器不會為它產生本機來源鏈；本機送出流量依 OS/output 規則處理。
- 規則依 metadata.name 排序，不依 YAML 順序。既有狀態、IPv6 控制流量與產生的服務/入口允許可能先符合。PF 也先產生廣泛隱含允許；轉送規則的 toZone 不形成輸出介面或目的區域條件，需要目的範圍時請指定 destinationCIDRs。
- port 與 destinationPorts 互斥。指定來源或目的連接埠需要 tcp/udp。範圍須為清單中唯一項目；接受數值連接埠與 start-end 字串。ICMP 型別需要對應協定。
- Linux 的來源 CIDR 為 OR；destinationCIDRs 與 destinationSetRefs 取聯集，再排除指定集合。來源、目的與協定位址族須相容。PF 目前忽略位址集合參照、rateLimit、connLimit，即使驗證接受。
- Linux rateLimit 比對超過設定速率的流量，connLimit 比對超過 maxPerSource 的數量，將 action 套用於超額流量。限制時請使用適當 drop/reject。兩者同時啟用會產生獨立替代規則，不是兩個門檻的 AND。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: "<name>"
  spec:
    action: "accept" # 必填; string; enum=["accept", "drop", "reject"]
    connLimit: # 選填; object
      log: true # 選填; boolean
      maxPerSource: 0 # 選填; integer; minimum=0
    destinationCIDRs: # 選填; array
      - "<string>" # string
    destinationPorts: # 選填; array
      - "<string>" # string
    destinationSetRefs: # 選填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 選填; array
      - "<string>" # string
    fromZone: "<string>" # 必填; string
    icmpType: "<string>" # 選填; string
    icmpv6Type: "<string>" # 選填; string
    log: true # 選填; boolean
    port: 0 # 選填; integer; minimum=0; maximum=65535
    protocol: "" # 選填; string; enum=["", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip"]
    rateLimit: # 選填; object
      burst: 0 # 選填; integer; minimum=0
      log: true # 選填; boolean
      packetsPerSecond: 0 # 選填; integer; minimum=0
      per: "" # 選填; string; enum=["", "second", "minute"]
      rate: 0 # 選填; integer; minimum=0
      unit: "" # 選填; string; enum=["", "packet", "byte", "kilobyte", "megabyte"]
    sourcePorts: # 選填; array
      - "<string>" # string
    srcCIDRs: # 選填; array
      - "<string>" # string
    toZone: "<string>" # 必填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.fromZone` | 必填單純 FirewallZone 名稱或 self。接受 self，但此處不產生本機送出篩選。 |
| `spec.toZone` | 必填單純 FirewallZone 名稱或 self。Linux 比對區域配對輸入/輸出介面；self 選擇本機輸入。PF self 使用路由器位址，其他 toZone 不形成輸出區域條件。 |
| `spec.srcCIDRs` | 選用 IPv4/IPv6 來源前綴 OR 清單；預設空為任意來源。須為有效 CIDR；Linux 產生前會遮罩主機位元。 |
| `spec.destinationCIDRs` | 選用目的前綴清單，預設空。Linux 與 destinationSetRefs 取聯集；兩者皆無時區域路徑內不限制目的。PF 僅使用這些靜態目的前綴。 |
| `spec.destinationSetRefs` | 選用既有 IPAddressSet/<name> 參照，預設空。Linux 使用啟用的 IPv4/IPv6 nftables 集合；目前 PF 不執行。 |
| `spec.excludeDestinationSetRefs` | 選用既有 IPAddressSet/<name> 參照，從 Linux 目的比對中排除。預設空；目前 PF 忽略。 |
| `spec.protocol` | 預設空為任意協定。接受 tcp、udp、icmp、icmpv6、ipv6-icmp、ipip。此處 ipip 是 IPv6 內封裝 IPv4（next-header 4），不是 IPv4 底層通道協定規則。 |
| `spec.port` | 選用單一目的連接埠，預設 0 為未指定。正值 1–65535 需要 tcp/udp，且與 destinationPorts 互斥。 |
| `spec.sourcePorts` | 選用 tcp/udp 來源連接埠 OR 清單，各值為 1–65535 或唯一 start-end 範圍。預設空為任意來源連接埠。 |
| `spec.destinationPorts` | 選用 tcp/udp 目的連接埠 OR 清單，規則與來源相同。與非零 port 互斥，預設空為不限制。 |
| `spec.icmpType` | 選用 IPv4 ICMP 名稱，須為 icmp。支援 echo-reply、destination-unreachable、source-quench、redirect、echo-request、router-advertisement、router-solicitation、time-exceeded、parameter-problem、timestamp-request、timestamp-reply。空表示全部型別；此鍵優先於 YAML 別名 icmp-type。 |
| `spec.icmpv6Type` | 選用 IPv6 ICMP 名稱，須為 icmpv6/ipv6-icmp。支援 destination-unreachable、packet-too-big、time-exceeded、parameter-problem、echo-request/reply、router-solicit/advert、neighbor-solicit/advert 及其 nd- 別名。空為全部型別，優先於 icmpv6-type 別名。 |
| `spec.action` | 必填 accept、drop 或 reject，無預設值。Linux 搭配速率/連線條件時，動作用於超額流量，不是允許配額。 |
| `spec.log` | 預設 false。true 在動作前加入紀錄；Linux 使用核心紀錄或已啟用 FirewallEventLog 的 NFLOG，PF 使用 log。 |
| `spec.rateLimit` | 選用 Linux 超速率條件，預設停用。目前 PF 忽略所有內層速率選項。 |
| `spec.rateLimit.rate` | 非負速率，預設 0 改用 packetsPerSecond。有效值 0 停用條件，且 unit/per/burst/log 須為空/0/false。正 rate 優先於 packetsPerSecond。 |
| `spec.rateLimit.unit` | 啟用時預設 packet，也接受 byte、kilobyte、megabyte。選擇 nftables 速率與突發單位，需要正有效速率。 |
| `spec.rateLimit.per` | 啟用時預設 second，或 minute，需要正有效速率。 |
| `spec.rateLimit.log` | 預設 false。true 在動作前記錄超速率比對，需要正有效速率。 |
| `spec.rateLimit.packetsPerSecond` | rate=0 時使用的舊式非負速率，預設 0。雖有此名稱，明確 unit/per 仍決定有效單位。 |
| `spec.rateLimit.burst` | 所選單位的非負突發量。預設 0 省略 burst 子句，依 nftables 行為。非零需要啟用速率。 |
| `spec.connLimit` | 選用 Linux 每來源連線數條件，預設停用。獨立於 rateLimit，目前 PF 忽略。 |
| `spec.connLimit.maxPerSource` | 非負上限，預設 0 停用。正 N 比對每來源位址 ct count 超過 N，再套用 action；IPv4/IPv6 使用獨立計量。 |
| `spec.connLimit.log` | 預設 false。true 為超額連線數比對加入 conn-limit 紀錄；maxPerSource=0 時無效。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `action`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)
