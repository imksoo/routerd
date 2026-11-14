# Path MTU 与 TCP MSS

routerd 从建立隧道路径的资源中，自动导出 Path MTU 的处理方式。
DS-Lite、PPPoE、WireGuard 各接口提供有效 MTU，防火墙区域则表示从 LAN 往 WAN 的转发方向。

从 trusted 接口转发至 untrusted 隧道时，routerd 自动生成 TCP MSS clamp。
MSS 设置为：IPv4 TCP 为 `MTU - 40`，IPv6 TCP 为 `MTU - 60`。
有效值以来源接口 MTU 与目的地路径的 Path MTU 中较小的一方为准，
分别针对来源路径与目的地路径计算。Linux 的 nftables 生成器只在
SYN 数据包所广播的 MSS 大于此导出值时才进行改写。
因此不会拉高来自其他具有较小 MTU 接口的较低 MSS，
也不会将无关的 LAN 路径拉低至较低的 MTU。

若 trusted 接口上有 `DHCPv6Server` 或 `IPv6RouterAdvertisement`，
且转发路径使用较小的隧道 MTU，RA 也会反映导出的 MTU。
配置中只需声明 LAN、WAN、隧道、防火墙区域及 RA/DHCPv6 的意图，无需编写各自的 MTU policy 资源。

reverse path filter 的 sysctl 也是相同的设计思想。routerd 从路由器与隧道的
资源，自动导出面向路由器的保守默认值，以及各隧道的 `rp_filter=0`。
