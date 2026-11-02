# Linux の DHCPv6-PD RDNSS 撤回および復帰

- 対象リビジョンは `baa6c8451fc60f280d63768031cb715850716d75` です。
  Linux 配布物の SHA-256 は `4cdf14a7160b120633b7fa1731362d85bbd95640bd15e683c23b3df497066d17` です。
- 構成は隔離した PVE の VLAN 3131 と VLAN 3132 です。
  VM219 は候補ルーター、VM220 は独立 Kea DHCPv6 サーバ、VM224 は独立 Linux クライアントです。
- Kea は VLAN 3132 で `2001:db8:1500::/60` を委任しました。
  VM219 の現行 `routerd-dhcpv6-client` は Solicit、Advertise、Request、Reply を完了しました。
  LAN アドレスは `2001:db8:1500::1/64` になりました。
- VM224 は VLAN 3131 で SLAAC アドレス、既定経路、RDNSS `2001:db8:1500::1` を取得しました。
  VM224 はその RDNSS への UDP DNS 問合せで `router.home.arpa` を解決しました。
  応答は `2001:db8:1500::1` です。
- Kea を停止すると、VM219 は `Renewing`、`Rebinding`、`PrefixExpired` を記録しました。
  VM219 の委任 LAN アドレスは撤回されました。
  VM224 は SLAAC アドレス、既定経路、RDNSS を失いました。
  同じ DNS 問合せは `network unreachable` で失敗しました。
- Kea を再開し、候補の DHCPv6 子プロセスを再起動しました。
  VM219 は Solicit、Advertise、Request、Reply、`PrefixBound` を記録しました。
  VM224 は RDNSS、既定経路、SLAAC アドレスを再取得しました。
  DNS 問合せも再び成功しました。
- 復帰後、WAN から LAN への拒否規則は 0 パケットです。
  WAN から自装置への UDP 宛先ポート 546 規則は 5 パケットでした。
- この記録は DHCPv6-PD による RDNSS 配布と撤回・復帰を対象にします。
  DHCPv6 の DNS オプション、再採番、WAN 側ルーター広告、IPv6 転送、FreeBSD は未認定です。
