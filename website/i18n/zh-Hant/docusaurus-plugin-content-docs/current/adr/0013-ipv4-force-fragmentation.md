# ADR 0013: 受信任 overlay 路徑上的 IPv4 強制分片

![ADR 0013 的示意圖。從常規 MTU 推導和 MSS clamping，到非 TCP DF 黑洞風險，以及明確的 trusted-overlay routerd_forcefrag 處理](/img/diagrams/adr-0013-ipv4-force-fragmentation.png)

## 狀態

已核准為預發布實作。

## 背景

routerd 已從隧道和轉送意圖推導路徑 MTU 處理。
常規緩解措施是 TCP MSS clamping：Linux 上 `routerd_mss`
為推導出的低 MTU 轉送路徑的 TCP SYN MSS 進行重寫，
無需防火牆 zone。

MSS clamping 對非 TCP 流量無效。設定了 DF 位元的、超大的
UDP、QUIC、ICMP 及其他 IPv4 封包，當受信任 overlay 或 underlay 的
有效 MTU 較低時，如果 PMTUD 回饋被阻擋或忽略，
可能會形成黑洞。

清除 DF 不是通用網際網路的預設行為。它違反傳送端的明確路徑 MTU 偏好，
產生轉送成本高的分片且更易被丟棄。
因此此功能必須是明確的、路徑範圍的、預設關閉的。

## 決策

在 overlay 路徑 MTU 意圖中新增明確的 IPv4 強制分片選項：

- `OverlayPeer.spec.pathMTU.forceFragmentIPv4`
- `TunnelInterface.spec.pathMTU.forceFragmentIPv4`

此功能僅在 routerd 可以推導轉送路徑和有效 MTU 的受信任 routerd
overlay 裝置上支援：`wireguard`、`ipip`、`gre`、`fou`、`gue`。
啟用強制分片時，驗證將拒絕 `route`、`tailscale`、`ipsec` 及其他
underlay 型別。

在 Linux 上，routerd 渲染專用的 nftables 資料表：

```text
table ip routerd_forcefrag {
  chain forward {
    type filter hook forward priority mangle; policy accept;
    iifname <capture> oifname <tunnel> ip length > <path-mtu> ip frag-off 0x4000 ip frag-off set 0
  }
}
```

比對僅限 IPv4，範圍限定在推導的轉送路徑。僅對目前未分片的
超大 DF 封包清除 DF。之後核心在 egress 裝置按常規介面
MTU 進行分片。

TCP MSS clamping 仍是 TCP 的主要緩解措施。強制分片是
明確受信任路徑上非 TCP 或不當大小流量的兜底措施。

## 替代方案

- **Route MTU lock。** 對 routerd 擁有的路由更標準，但
  無法乾淨地涵蓋包含 BGP 匯入行動性路徑在內的所有路由來源。
  策略分散在路由寫入者之間。
- **iptables。** 現有的庫存目標在 DF 清除的跨路徑表達上
  不比 nftables 更乾淨。
- **首階段即支援 FreeBSD pf。** pf 有 `scrub ... no-df`，但
  routerd 的 SAM/overlay 實際資料平面以 Linux 為主。
  FreeBSD 支援留在後續階段而非假裝已對等。

## 結論

- 預設行為不變。
- Linux 在 `routerd_mss` 旁取得第二個路由器擁有的路徑 MTU nftables 資料表
  `routerd_forcefrag`。
- 操作員需要按 overlay 路徑或隧道介面逐個選擇啟用。
- 分片可能降低吞吐量並增加丟包敏感性。
  文件應將其描述為受信任 overlay PMTU 黑洞的最後手段。
