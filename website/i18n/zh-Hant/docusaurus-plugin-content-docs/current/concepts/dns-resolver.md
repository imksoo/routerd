---
title: DNS 解析器
slug: /concepts/dns-resolver
---

# DNS 解析器

routerd 的 DNS 將權威資料、解析器程序、轉發規則與上游端點，分別以小型資源表達。

`DNSZone` 保存本地的權威資料，包含手動輸入的記錄，以及從 DHCP 租約衍生的記錄。

`DNSResolver` 管理常駐程式實例，定義監聽位址、快取、metrics 及查詢紀錄。一個 `DNSResolver` 資源啟動一個 `routerd-dns-resolver` 程序。

`DNSForwarder` 是隸屬於解析器的一條 match 規則，可從 `DNSZone` 回應，或將符合的查詢轉發至 `DNSUpstream`。

`DNSUpstream` 是一個上游端點，可表示純文字的 UDP/TCP DNS、DoT 及 DoH。

## 啟動與部分拉起

`DNSResolver` 不會等到所有相依關係都就緒才開始服務。啟動時，它會使用當時已解析出的監聽位址與
source 拉起常駐程式，並在其餘相依關係就緒後收斂：

- listen entry 會綁定目前已解析出的位址；若某個位址的 `*From` source 尚未就緒
  （例如仍在等待 DHCPv6 prefix delegation 的 delegated-prefix address），會在後續
  reconcile 中加入。
- 若某個 forward/upstream source 的動態上游尚未解析（例如上游來自
  `DHCPv6Information` server 的 AFTR forwarder），會先省略該 source，直到該上游出現。
  zone source 以及使用靜態或已解析上游的 source 會立即提供服務。

當仍有部分內容在等待時，資源會回報 `phase: Degraded`，並帶有 `waiting` list，列出每個
listen/source 正在等待什麼。這是正常的 bootstrap 狀態，不是故障：通用 DNS 已經可以回應。
相依資源發佈 status 後，controller 會重新 reconcile，並以完整設定收斂到
`phase: Applied`（與一開始就全部解析完成的啟動結果相同）。只有在完全沒有監聽位址可解析，
或沒有任何可用 source 保留時，解析器才會回報 `phase: Pending`（不提供任何服務）。

這消除了等待 DHCPv6 prefix delegation 時 DNS 被拒絕的啟動窗口（在生產路由器上的實測：
AFTR forwarder 顯示 `Degraded` 時，通用 DNS 從第一秒起即可回應；delegated prefix 抵達後
收斂到 `Applied`）。主動重新啟動 `routerd` 時，程序自身重新啟動期間仍會有不到一秒的間隙。

## 回應來源的評估順序

參照解析器的 `DNSForwarder` 依設定中的順序評估。
具有 `zoneRefs` 的轉發器從 `DNSZone` 回應。
具有 `upstreams` 的轉發器將符合的查詢轉發至上游。
`match: ["."]` 是預設的遞迴查詢路徑。

解析器支援 DoH、DoT、TCP DNS 及純文字 UDP DNS。
上游依優先順序嘗試。
優先度高的上游失敗時，切換至下一個上游。

## 多監聽設定檔

`spec.listen` 是陣列。
每個監聽可選擇要使用的回應來源名稱子集合。
這讓 LAN 用與 VPN 用的監聽可有不同的行為，
同時仍共用一個解析器資源。
`listen[].sources` 中的名稱參照 `DNSForwarder`。省略時，使用該解析器所屬的所有轉發器。

若監聽位址需從其他資源狀態取得，請使用 `listen[].addressFrom`。
由於明確表達了相依關係，當來源資源變更時，可重新設定常駐程式。

```yaml
listen:
  - name: lan
    addresses:
      - 192.0.2.1
    addressFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    port: 53
```

若所需位址尚無法解析，解析器不會以舊位址啟動，
而是保持 `Pending(AddressUnresolved)` 狀態等待。

## 動態區域記錄

`DNSZone.spec.records[].ipv4` 與 `ipv6` 為固定值。
若記錄的位址需從其他資源狀態取得，請使用 `ipv4From` 或
`ipv6From`。

```yaml
records:
  - hostname: router
    ipv4From:
      resource: IPv4StaticAddress/lan-base
      field: address
    ipv6From:
      resource: IPv6DelegatedAddress/lan-base
      field: address
```

若所需的參照來源尚無法解析，該記錄會記錄至 `DNSZone.status.pendingRecords`。
來源資源變更時，解析器會重新產生，並在成功解析後發佈記錄。

## 限定網路的上游

`DNSUpstream.spec.sourceInterface` 在 Linux 上綁定送出介面。
以固定值指定 `ens18` 或 `wg0` 等 OS 介面名稱。
若隧道或 VRF 資源負責建立該介面，請透過資源的擁有權或順序明確表達相依關係，
讓解析器等待介面就緒後再啟動。

`DNSUpstream.spec.bootstrap` 是用於解析 DoH 或 DoT 連線目標名稱的輔助 DNS 伺服器，
適用於連線目標名稱只能在接入網路內部解析的情境。

若上游伺服器清單需從其他資源狀態取得，請使用 `addressFrom`。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DNSForwarder
metadata:
  name: ngn-aftr
spec:
  resolver: DNSResolver/lan-resolver
  match:
    - transix.jp
  upstreams:
    - DNSUpstream/wan-dns
---
apiVersion: net.routerd.net/v1alpha1
kind: DNSUpstream
metadata:
  name: wan-dns
spec:
  protocol: udp
  addressFrom:
    - resource: DHCPv6Information/wan-info
      field: dnsServers
```

使用者撰寫的 YAML 不接受 `DNSResolver.spec.sources`。請將舊式的內嵌 source 拆分為 `DNSForwarder` 與 `DNSUpstream`。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DNSForwarder
metadata:
  name: default
spec:
  resolver: DNSResolver/lan-resolver
  match:
    - "."
  upstreams:
    - DNSUpstream/cloudflare
---
apiVersion: net.routerd.net/v1alpha1
kind: DNSUpstream
metadata:
  name: cloudflare
spec:
  protocol: doh
  address: cloudflare-dns.com
  path: /dns-query
```

## 與 dnsmasq 的界線

dnsmasq 僅負責 DHCPv4、DHCPv6、DHCP 中繼及 RA。
不產生 `server=`、`local=`、`host-record=`。
DNS 的回應與轉發全部由 `routerd-dns-resolver` 負責。
