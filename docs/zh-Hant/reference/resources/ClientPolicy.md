# ClientPolicy

[各資源的設定](../resources.md#clientpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

對選定或非例外用戶端套用訪客服務與路由目的限制。Linux 以設定的 MAC 識別，FreeBSD PF 需要明確 IP 身分。

## 指定條件與參照關係

- mode 必須為 include/exclude。include 限制選定集合，exclude 限制選定可信集合以外的用戶端。Linux include 沒有選定 MAC 時不產生限制；exclude 集合空時比對所選介面全部用戶端。
- Linux 分類僅使用 match.macs：guest/isolated 加入 include 集合，trusted 加入 exclude 集合。OUI、主機名、指紋選擇器會驗證，但防火牆產生器不使用。ipv4Reservation/ipv6Addresses 不形成 Linux 比對身分。
- FreeBSD 拒絕頂層 MAC 與所有 match 選擇器；每類改需 ipv4Reservation 或明確 ipv6Addresses。PF 使用預約的 ipAddress，不是 MAC。PF 請明確提供 interfaces；省略時不繼承 trust 區域介面。
- 這是路由/本機輸入限制，不是 L2 交換器埠隔離。既有狀態、控制與服務允許可能先執行。PF 的 trust/mgmt-to-self 隱含允許在用戶端本機限制之前，且目前本機服務限制僅使用 IPv4 身分。不可視為兩個 OS 或所有位址族皆一致強制。
- Linux guestEgressAllow 先於探索/拒絕規則，可覆蓋它們；其餘流量繼續區域規則。PF IPv4 允許先於拒絕，但 IPv6 拒絕規則在全域前段，無法由之後的 IPv6 允許覆蓋。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    classification: # 選填; array
      - # object
        ipv4Reservation: "<string>" # 選填; string
        ipv6Addresses: # 選填; array
          - "<string>" # string
        match: # 必填; object
          dhcpFingerprints: # 選填; array
            - "<string>" # string
          hostnamePatterns: # 選填; array
            - "<string>" # string
          macs: # 選填; array
            - "<string>" # string
          ouiPrefixes: # 選填; array
            - "<string>" # string
        mode: "trusted" # 必填; string; enum=["trusted", "guest", "isolated"]
        name: "<string>" # 選填; string
    guestEgressAllow: # 選填; array
      - "<string>" # string
    guestEgressDeny: # 選填; array
      - "<string>" # string
    guestServices: # 選填; array
      - "<string>" # string
    interfaces: # 選填; array
      - "<string>" # string
    isolation: # 選填; object
      lanInternet: "" # 選填; string; enum=["", "allow", "deny"]
      lanLAN: "" # 選填; string; enum=["", "allow", "deny"]
      lanMgmt: "" # 選填; string; enum=["", "allow", "deny"]
      mDNSBroadcast: "" # 選填; string; enum=["", "allow", "deny"]
    macs: # 選填; array
      - "<string>" # string
    mode: "include" # 必填; string; enum=["include", "exclude"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.mode` | 必填 include/exclude，無預設值。決定身分集合為限制對象或例外。 |
| `spec.interfaces` | 選用 Interface/name 或單純邏輯介面名，參照須存在且不可重複。Linux 省略時使用所有 trust 區域介面，皆無則失敗。PF 省略時沒有適用介面限制。 |
| `spec.macs` | 選用 Linux 固定 MAC 清單，預設空。此清單與 classification.match.macs 間正規化後的重複會被拒絕。FreeBSD 禁用。 |
| `spec.classification` | 選用分類清單，預設空。Linux 每類需至少一個選擇器，但只有 MAC 形成防火牆集合。FreeBSD 需要穩定 IP 身分並禁止選擇器。 |
| `spec.classification[].name` | 選用描述名稱，預設空。不改變比對優先順序，也不建立資源識別。 |
| `spec.classification[].mode` | 必填 trusted、guest 或 isolated。include 將 guest/isolated 同樣加入限制集合；exclude 將 trusted 加入例外集合。 |
| `spec.classification[].match` | Linux 至少需要一個非空選擇器清單。FreeBSD 即使提供穩定 IP 也須留空。目前防火牆不將各選擇器動態解析成用戶端 MAC。 |
| `spec.classification[].match.macs` | 選用完整 MAC 清單，是 Linux 的有效分類器。MAC 須有效，且不可跨類別或頂層 macs 重複。 |
| `spec.classification[].match.ouiPrefixes` | 接受如 18:ec:e7 的三個十六進位八位元組，同類重複會被拒絕。目前不形成防火牆強制，FreeBSD 拒絕。 |
| `spec.classification[].match.hostnamePatterns` | 接受非空 Go path.Match glob，不是正規表示式。目前僅驗證語法，不解析為防火牆身分；FreeBSD 拒絕。 |
| `spec.classification[].match.dhcpFingerprints` | 接受非空且不含空白的代碼，同類不可重複。目前不形成防火牆集合；FreeBSD 拒絕。 |
| `spec.classification[].ipv4Reservation` | 選用既有 DHCPv4Reservation 的單純 metadata.name，拒絕 Kind/name。PF 使用其非空 ipAddress；Linux 驗證參照但不以位址比對用戶端。 |
| `spec.classification[].ipv6Addresses` | 選用 PF 的純 IPv6 位址，不接受 CIDR 或 IPv4 映射格式。所有類別間正規化後不可重複。不從 MAC/IPv4 預約推導，Linux 用戶端比對不使用。 |
| `spec.isolation` | 選用 allow/deny 提示，只有 Linux 產生器讀取這四個欄位；PF 使用明確 guest egress 清單或預設。不會逐類獨立套用。 |
| `spec.isolation.lanInternet` | 預設空。Linux deny 且未明確設 guestEgressDeny 時，將拒絕集合換成 0.0.0.0/0 與 ::/0。allow/空不增加無條件網際網路允許。PF 忽略。 |
| `spec.isolation.lanLAN` | 預設空。Linux 僅在 lanLAN 與 lanMgmt 皆 allow 時移除預設私有前綴拒絕，不區分個別 LAN。明確 guestEgressDeny 覆寫預設。PF 忽略。 |
| `spec.isolation.lanMgmt` | 預設空，與 lanLAN 共同決定預設集合。不會獨立解析管理區域目的。PF 忽略。 |
| `spec.isolation.mDNSBroadcast` | 預設空。Linux deny 在 guestEgressAllow 後加入轉送 mDNS/SSDP 多播與 UDP 137/138 丟棄。不控制 L2 橋接或獨立封鎖本機服務。PF 忽略。 |
| `spec.guestServices` | 允許的本機服務名稱：dns、dhcp、ntp、mdns、ssdp。省略或空預設 dhcp/dns/ntp，空清單無法停用全部例外。只開放防火牆，不啟動服務。 |
| `spec.guestEgressDeny` | 選用 IPv4/IPv6 CIDR。省略或空預設 10.0.0.0/8、172.16.0.0/12、192.168.0.0/16、fc00::/7，Linux 依上述 isolation 調整。明確值取代而非附加預設；PF 空時固定使用私有前綴預設。 |
| `spec.guestEgressAllow` | 選用 IPv4/IPv6 CIDR 例外，預設空。Linux 與 PF IPv4 先於拒絕評估，PF IPv6 則在拒絕之後。這些是路由目的前綴，不是資源參照。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/webconsole/clients.go](https://github.com/imksoo/routerd/blob/main/pkg/webconsole/clients.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)
