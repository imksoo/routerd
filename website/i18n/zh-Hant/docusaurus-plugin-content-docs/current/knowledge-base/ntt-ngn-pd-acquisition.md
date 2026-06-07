---
title: NTT NGN 系存取網路的 DHCPv6-PD 與 AFTR
---

# NTT NGN 系存取網路的 DHCPv6-PD 與 AFTR

![Diagram showing DHCPv6-PD and AFTR acquisition on NTT NGN-style access from prefix delegation and information request through carrier DNS AFTR resolution to DS-Lite tunnel, IPv4 route, NAT44, and LAN connectivity checks](/img/diagrams/knowledge-base-ntt-ngn-pd-acquisition.png)

本文是在 NTT NGN（日本 IPv6 光纖線路）等 IPv6 存取網路的 HGW 下使用 routerd 的實地筆記。
同樣結合 DHCPv6-PD 與網路內 AFTR 進行 DS-Lite 的其他業者，也可套用相同模式。

## DHCPv6-PD

`routerd-dhcpv6-client` 在這些 HGW 下可穩定取得 DHCPv6-PD。
無需過多重送或特殊取得程序，標準的 solicit / advertise / request / renew 即已足夠。

穩定運作時，可觀察到以下行為：

- 同一 HGW 下的多台路由器各自取得互不重疊的前綴。
- 依 T1 / T2 時序，Renew 持續成功。
- 重新啟動常駐程式後，可從 `lease.json` 還原租約。

## DHCPv6 information-request 可能不回傳 AFTR

部分 HGW / ONU 設定下，DHCPv6 的 information-request 會回傳 DNS、SNTP、domain-search，
但不回傳 AFTR 選項。AFTR 為空本身是正常現象。

此情況下，DS-Lite 需明確指定以下其中一項：

- `DSLiteTunnel.spec.aftrIPv6` — 直接固定 AFTR 的 IPv6 位址。
- `DSLiteTunnel.spec.aftrFQDN` — 解析 FQDN。

## AFTR 的 FQDN 通常需要條件式 DNS 轉送

業者管理的 AFTR FQDN（例如：`gw.transix.jp`），往往只能透過業者內部 DNS 解析，
公開解析器會回傳 NXDOMAIN。

在 routerd 中，透過 `DNSResolver` 的 `forward` source 來表達：

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: resolver
  spec:
    listen:
      - name: local
        addresses: [127.0.0.1]
        port: 53
    sources:
      - name: aftr
        kind: forward
        match: [transix.jp]
        upstreams:
          - udp://[2404:8e00::feed:101]:53
```

DS-Lite 控制器透過 `routerd-dns-resolver` 解析 AFTR 的 FQDN，不經過系統的 stub 解析器。

## DS-Lite 端對端確認清單

DS-Lite 正常運作時，應可觀察到以下狀態：

- 條件式轉送能解析 AFTR 的 FQDN。
- `ip6tnl` 隧道裝置存在。
- IPv4 預設路由指向隧道。
- nftables 的 NAT44 已為 LAN 往外的 IPv4 流量設定完畢。
- LAN 用戶端能成功連線至外部 IPv4（HTTP / ICMP）。

## 本筆記的定位

以上內容為在 routerd 評估環境中，使用業者出廠 HGW 所得到的觀測結果。
可作為類似部署的參考指引，但並非對國內所有 ISP 方案或 HGW 韌體版本的保證。
請將其作為自行驗證的起點。
