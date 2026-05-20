# Path MTU と TCP MSS

routerd は tunnel path を作る resource から Path MTU の扱いを自動導出します。
DS-Lite、PPPoE、WireGuard interface が有効 MTU を提供し、firewall zone が
LAN から WAN へ転送する向きを表します。

trusted interface から untrusted tunnel へ転送する場合、routerd は TCP MSS
clamp を自動で生成します。IPv4 TCP では MSS を `MTU - 40`、IPv6 TCP では
`MTU - 60` にします。

trusted interface に `DHCPv6Scope` または `IPv6RouterAdvertisement` があり、
転送経路が小さい tunnel MTU を使う場合、RA にも派生 MTU を反映します。
config には LAN、WAN、tunnel、firewall zone、RA/DHCPv6 の intent を宣言し、
個別の MTU policy resource は書きません。

reverse path filter sysctl も同じ考え方です。routerd は router と tunnel
resource から、router 向けの保守的な default と tunnel 別 `rp_filter=0` を
自動導出します。
