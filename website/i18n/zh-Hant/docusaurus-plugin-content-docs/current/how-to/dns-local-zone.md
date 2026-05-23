---
title: 本地 DNS 區域
slug: /how-to/dns-local-zone
---

# 本地 DNS 區域

## 適用情境

當您希望透過名稱解析內部主機，但又不想手動同步各裝置的 `/etc/hosts` 時，具體而言是希望實現以下目標：

- 擁有少量固定記錄（路由器、NAS、印表機）。
- 為取得 DHCP 租約的裝置自動產生 A / AAAA / PTR 記錄。
- 正向查詢與反向查詢均正常運作。

## routerd 的解決方式

使用 `DNSZone` 管理單一 DNS 網域的本地權威記錄。
可以結合**手動記錄**（以 YAML 宣告）與**來自 DHCP 的記錄**（從租約資料庫建立）。
`DNSResolver` 將這些記錄作為回應來源之一載入，使內部查詢在本地回應，外部查詢則轉送至設定的上游解析器。

DHCP 衍生的記錄透過事件匯流排同步。dnsmasq 在租約變更時呼叫 `routerd-dhcp-event-relay`，relay 發布 routerd 事件，`routerd-dns-resolver` 則更新記憶體中的區域資料。
dnsmasq 的租約檔案在啟動時也會重新讀取，因此即使重啟常駐程式也不會遺失記錄。

## 範例

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: lan
  spec:
    zone: lan.example.org
    ttl: 300
    dnssec:
      enabled: false
    records:
      - hostname: router
        ipv4: 192.0.2.1
        ipv6: 2001:db8:1::1
      - hostname: nas
        ipv4: 192.0.2.10
    dhcpDerived:
      sources:
        - DHCPv4Server/lan-dhcpv4
        - DHCPv6Server/lan-dhcpv6
      hostnameSuffix: lan.example.org
      ddns: true
      ttl: 60
      leaseFile: /run/routerd/dnsmasq.leases
    reverseZones:
      - name: 2.0.192.in-addr.arpa
```

套用後，`nas.lan.example.org` 及 `<dhcp-client-name>.lan.example.org` 將解析為本地位址，`192.0.2.x` 的 PTR 查詢也會回傳對應的名稱。

## 補充說明

- 請選擇您擁有管理權的網域，或為內部使用保留的網域（如 `example.org`、`home.arpa`）。請勿使用可能與公眾 DNS 衝突的 suffix，例如 `.lan`。
- 啟用 DNSSEC（`dnssec.enabled: true`）後，外部的 DNSSEC 驗證仍可正常運作。本地區域在設計上不簽署。
- 若有多個內部子網路，請為每個子網路分別撰寫一條 `reverseZones` 條目，以確保雙向 PTR 查詢均可運作。

## 相關文件

- [專用 DNS 上游](./dns-private-upstream.md)
- [DNS 解析器概念](../concepts/dns-resolver.md)
