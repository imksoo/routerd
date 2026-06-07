# Path MTU 與 TCP MSS

![routerd 推導 tunnel MTU、TCP MSS clamp、router advertisement MTU 與可選 IPv4 force fragmentation 的流程](/img/diagrams/concept-path-mtu.png)

routerd 從建立隧道路徑的資源中，自動導出 Path MTU 的處理方式。
DS-Lite、PPPoE、WireGuard 各介面提供有效 MTU，防火牆區域則表示從 LAN 往 WAN 的轉發方向。

從 trusted 介面轉發至 untrusted 隧道時，routerd 自動產生 TCP MSS clamp。
MSS 設定為：IPv4 TCP 為 `MTU - 40`，IPv6 TCP 為 `MTU - 60`。
有效值以來源介面 MTU 與目的地路徑的 Path MTU 中較小的一方為準，
分別針對來源路徑與目的地路徑計算。Linux 的 nftables 產生器只在
SYN 封包所廣播的 MSS 大於此導出值時才進行改寫。
因此不會拉高來自其他具有較小 MTU 介面的較低 MSS，
也不會將無關的 LAN 路徑拉低至較低的 MTU。

若 trusted 介面上有 `DHCPv6Server` 或 `IPv6RouterAdvertisement`，
且轉發路徑使用較小的隧道 MTU，RA 也會反映導出的 MTU。
設定中只需宣告 LAN、WAN、隧道、防火牆區域及 RA/DHCPv6 的意圖，無需撰寫個別的 MTU policy 資源。

reverse path filter 的 sysctl 也是相同的設計思想。routerd 從路由器與隧道的
資源，自動導出面向路由器的保守預設值，以及各隧道的 `rp_filter=0`。
