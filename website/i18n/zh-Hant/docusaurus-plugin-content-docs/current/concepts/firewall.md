# 有狀態防火牆

![routerd firewall zone、policy matrix、明確 rule、client policy 與產生的 nftables filter 輸出](/img/diagrams/concept-firewall.png)

routerd 管理 nftables 的 `inet routerd_filter` 表。
防火牆以 4 種資源表達。

- `FirewallZone` 將介面指派至區域。
- `FirewallPolicy` 表示拒絕日誌等全域設定。
- `FirewallRule` 表示無法以角色組合表達的例外。
- `ClientPolicy` 依 MAC 位址分類 LAN 用戶端。

角色有 `untrust`、`trust`、`mgmt` 三種。`wan`、`lan`、
`management` 等是區域名稱，需與角色區分看待。

預設行為由以下表格決定：

| 來源 | self | mgmt | trust | untrust |
| --- | --- | --- | --- | --- |
| `mgmt` | 允許 | 允許 | 允許 | 允許 |
| `trust` | 允許 | 拒絕 | 允許 | 允許 |
| `untrust` | 拒絕 | 拒絕 | 拒絕 | 拒絕 |

所產生的規則始終允許已建立的通訊、關聯通訊、loopback，以及必要的 ICMPv6。
DHCPv6 PD、DS-Lite、dnsmasq 的 DHCP、`routerd-dns-resolver` 等所需的開口也由 routerd 在內部產生。

NAT44 使用獨立的 `ip routerd_nat` 表。

## Rule expression

`FirewallRule` 支援依 CIDR 比對來源 / 目的地、可重複使用的 `IPAddressSet` 目的地參照、TCP/UDP 的 `sourcePorts` / `destinationPorts`、ICMP / ICMPv6 的 type 名稱、nftables 的 `rateLimit`，以及以來源為單位的 `connLimit`。
`rateLimit` 與 `connLimit` 在超過閾值的通訊時相符，因此基本上與 `drop` / `reject` 規則組合使用，以緩解掃描與暴力破解攻擊。

## 訪客裝置隔離

`ClientPolicy` 是依 MAC 位址分類同一 LAN 區段內裝置的訪客模式。
適用於可信任裝置與訪客裝置從同一 DHCP 伺服器取得位址的環境。
訪客裝置僅能使用路由器的最小限度服務，無法存取私有網路。

策略有兩種：

| mode | 行為 |
| --- | --- |
| `include` | 僅將清單中的 MAC 位址視為訪客，其餘為 trusted。 |
| `exclude` | 僅將清單中的 MAC 位址視為 trusted，對象介面上的其餘裝置視為訪客。 |

`ClientPolicy.spec.macs` 是常用情境的簡寫形式。省略 `interfaces` 時，對象為所有屬於 `trust` `FirewallZone` 的介面。`spec.isolation` 可以易讀的方式表達網際網路、LAN、management、本地探索的 allow / deny。

訪客裝置預設可使用 DNS、DHCP、NTP。
目的地為 `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`、`fc00::/7` 的通訊會被拒絕。
面向全球網際網路的通訊，依一般的區域矩陣與路由策略處理。

`ClientPolicy` 以 Linux nftables 的 Ethernet 來源位址集合實作。
FreeBSD pf 在 routed filter path 上不具備相同的 MAC 比對模型，routerd 明確將此資源視為不支援。

## 日誌

`FirewallPolicy.spec.logDeny` 為 true，且 `FirewallEventLog` 資源有效時，
所產生的 nftables 規則會將被拒絕的封包送至設定好的 NFLOG group。
在 Linux 上，`routerd-firewall-logger` 直接從 nfnetlink 讀取該 group，
並儲存至 `firewall-logs.db`，不另外啟動封包擷取程序。
可完整儲存 NFLOG 的 prefix、介面、封包 family、協定、
位址及連接埠。

Web 管理介面的 Firewall 頁籤與 `routerctl firewall-logs` 會讀取此資料庫。
請以受管理的 `generated service artifacts` 方式啟用 logger。
例如使用 `routerd-firewall-logger daemon --path /var/lib/routerd/firewall-logs.db --nflog-group 1`。

若要將已接受的通訊流量用於 DPI 觀測，可透過 `FirewallEventLog.spec.log.copyRange`
設定 NFLOG 從每個封包複製的資料量上限。
設定為 `1536` 或 `2048` 位元組左右，可在看到 TLS/HTTP/DNS 分類所需的封包開頭的同時，
避免持續將整個封包複製至使用者空間。
