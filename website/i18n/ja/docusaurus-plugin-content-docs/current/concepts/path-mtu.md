# Path MTU と TCP MSS

![routerd が tunnel MTU、TCP MSS clamp、router advertisement MTU、任意の IPv4 force fragmentation を導出する流れ](/img/diagrams/concept-path-mtu.png)

routerd は、トンネル経路を作るリソースから、Path MTU の扱いを自動で導出します。
DS-Lite、PPPoE、WireGuard、`TunnelInterface` underlay（`ipip`、`gre`、`fou`、`gue`）
が有効 MTU を提供し、firewall zone が LAN から WAN へ転送する向きを表します。

trusted インターフェースから untrusted トンネルへ転送する場合、routerd は TCP MSS の
clamp を自動で生成します。MSS は、IPv4 TCP では `MTU - 40`、IPv6 TCP では
`MTU - 60` にします。有効値は、送信元インターフェースの MTU と宛先経路の Path MTU の
うち小さい方を、送信元経路と宛先経路ごとに計算します。Linux の nftables 生成器は、
SYN パケットが広告する MSS がこの派生値より大きいときだけ書き換えます。
そのため、別の小さい MTU を持つインターフェースから来た低い MSS を引き上げることはなく、
無関係な LAN 経路まで低い MTU に引っ張ることもありません。

それでも、TCP 以外の IPv4 通信や DF ビットが立った過大パケットが、信頼済みオーバーレイ上で
PMTU ブラックホールに落ちる場合があります。このとき、`OverlayPeer.spec.pathMTU.forceFragmentIPv4` と
`TunnelInterface.spec.pathMTU.forceFragmentIPv4` を明示的に有効化できます。Linux では
routerd が `ip routerd_forcefrag` nftables テーブルを生成し、導出した転送経路上で
`ip length` が Path MTU より大きく、かつ DF が立っている IPv4 パケットだけの DF
を消します。
その後の分割はカーネルの送出側 MTU に任せます。
この機能は IPv4 専用で、
既定では無効です。まず正しい MTU、PMTUD、TCP MSS clamp を優先し、強制分割は
分割を許容できる信頼済みオーバーレイやアンダーレイでの最終手段として使ってください。

trusted インターフェースに `DHCPv6Server` または `IPv6RouterAdvertisement` があり、
転送経路が小さいトンネル MTU を使う場合、RA にも派生した MTU を反映します。
設定には LAN、WAN、トンネル、firewall zone、RA/DHCPv6 の意図を宣言するだけで、
個別の MTU ポリシーのリソースは書きません。

Reverse path filter の sysctl も同じ考え方です。routerd はルーターとトンネルの
リソースから、ルーター向けの保守的な既定値と、トンネル別の `rp_filter=0` を
自動で導出します。
