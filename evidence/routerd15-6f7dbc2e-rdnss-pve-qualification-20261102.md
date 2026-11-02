# routerd 1.5 Linux RDNSS と IPv6 DNS の PVE 実通信検証（2026-11-02）

## 対象

- ソース改訂: 6f7dbc2e516598a4ad89150f5d0f1dd16c6a5212
- 対象: DHCPv6-PD、IPv6DelegatedAddress、IPv6RouterAdvertisement の RDNSS、DNSResolver、DNSZone
- 構成: VM219 は候補ルーター、VM220 は独立 Kea DHCPv6 サーバー、VM224 は独立 LAN クライアント名前空間
- 分離: VLAN 3131 は LAN、VLAN 3132 は DHCPv6-PD 上流、VLAN 3133 は管理網
- 設定: 新規 routerd 1.5 設定だけを使用した。旧設定、旧状態、移行機能は使用していない。

## 実通信の観測

独立 Kea は 2001:db8:1500::/60 を候補へ委任した。候補は LAN 側へ 2001:db8:1500::1/64 を設定し、RA の RDNSS と DNSResolver の待受アドレスの両方を同じ IPv6DelegatedAddress の参照から生成した。

VM224 の独立クライアント名前空間は、2001:db8:1500::/64 の SLAAC アドレスとリンクローカル経由の既定経路を取得した。実際に捕捉した RA パケットには、PIO の有効期間 40 秒と RDNSS の有効期間 40 秒、RDNSS アドレス 2001:db8:1500::1 が入っていた。

VM224 から 2001:db8:1500::1 の UDP 53 番へ問い合わせた router.home.arpa の AAAA 応答は 2001:db8:1500::1 だった。候補 DNS リゾルバーが委任済みアドレスで実際に応答したことを示す。

## 撤回と復旧

空の新規 1.5 設定の適用は世代 1 で Healthy になった。その後、VM224 の IPv6 既定経路は消え、同じ DNS 問合せは network unreachable になった。

元の新規設定を再適用した世代 2 では、VM224 の SLAAC アドレス、既定経路、DNS 応答が復旧した。routerd の再起動後も IPv6RouterAdvertisement は Applied、RDNSS は 2001:db8:1500::1、DNS 問合せは成功だった。

## 試験台の事実

Kea の初回起動は AppArmor の既定プロファイルにより名前空間内の PID 作成を拒否された。また、PD プールの delegated-len を 64 とすると NoPrefixAvail になった。既存の独立 Kea 試験と同じ unconfined 起動および delegated-len 60 へ修正後に委任を確認した。これらは試験台の起動不備であり、候補の合格結果には含めない。

Linux カーネルだけでは RDNSS を resolv.conf へ自動反映しないため、OS のリゾルバー設定への自動採用はこの証跡の対象外である。RA に RDNSS が実際に載ることと、そのアドレスの DNS 応答までは確認した。

## 復元

VM219 の実行ファイルは試験前の SHA-256 へ戻し、routerd と routerd-dnsmasq は active を確認した。VM220 と VM224 の試験用名前空間を削除し、VM224 を停止した。候補 ISO は削除し、VM219 の起動用 ISO を試験前のものへ戻した。
