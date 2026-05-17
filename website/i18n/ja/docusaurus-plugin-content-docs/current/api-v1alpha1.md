---
title: リソース API v1alpha1
slug: /reference/api-v1alpha1
---

# リソース API v1alpha1

routerd の設定は、最上位の `Router` と、型付きリソースの一覧で構成します。
このページは現在の実装に合わせたリソース一覧です。
Phase 1.6 以降は RFC 表記に合わせ、DHCP 関連 Kind は `DHCPv4*` と `DHCPv6*` を使います。
旧名の互換別名はありません。

## 共通形

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: wan
spec:
  ifname: ens18
  adminUp: true
```

| フィールド | 意味 |
| --- | --- |
| `apiVersion` | API グループと版です。 |
| `kind` | リソース種別です。 |
| `metadata.name` | 同じ種別内の名前です。 |
| `spec` | 利用者が宣言する意図です。 |
| `status` | routerd または専用デーモンが観測した状態です。 |

## API グループ

| API グループ | 主な Kind |
| --- | --- |
| `routerd.net/v1alpha1` | `Router` |
| `net.routerd.net/v1alpha1` | インターフェース、再利用可能な `IPAddressSet`、DHCP、DNS、経路、トンネル、イベント、通信フローログ |
| `firewall.routerd.net/v1alpha1` | `FirewallZone`, `FirewallPolicy`, `FirewallRule`, `FirewallLog`, `ClientPolicy`, `PortForward`, `IngressService`, `LocalServiceRedirect` |
| `system.routerd.net/v1alpha1` | `Hostname`, `Sysctl`, `SysctlProfile`, `KernelModule`, `Package`, `NetworkAdoption`, `SystemdUnit`, `NTPClient`, `LogSink`, `LogRetention`, `WebConsole`, `NixOSHost` |
| `plugin.routerd.net/v1alpha1` | プラグインマニフェスト |

## システム準備

| Kind | 役割 |
| --- | --- |
| `Package` | OS ごとのパッケージ名を宣言し、不足していれば導入します。 |
| `Sysctl` | 実行時の sysctl 値を設定します。`compare: exact` と `compare: atLeast` で読み戻し判定を選べます。 |
| `SysctlProfile` | ルーター向け sysctl 推奨値をまとめて設定します。 |
| `KernelModule` | Linux kernel module を `modprobe` し、必要なら `/etc/modules-load.d` に永続化します。 |
| `NetworkAdoption` | OS 標準の DHCP クライアントや systemd-resolved の待ち受けを調整します。DHCPv4 の経路と DNS だけを無効にする設定も扱います。 |
| `SystemdUnit` | routerd が使う systemd ユニットを生成し、有効化します。 |
| `Hostname` | ホスト名を設定します。 |
| `NTPClient` | OS の NTP クライアントを有効にします。DHCPv4 / DHCPv6 の状態から時刻サーバーを導出し、空なら public NTP サーバーへ戻せます。 |
| `LogSink` | routerd のイベントを syslog や外部プログラムへ送ります。 |
| `LogRetention` | イベント、DNS、通信フロー、ファイアウォールログの保管期間を管理します。 |
| `WebConsole` | 読み取り専用の Web 画面を管理ネットワークで待ち受けます。 |

## インターフェースとリンク

| Kind | 役割 |
| --- | --- |
| `Interface` | routerd が扱う安定した名前と OS のインターフェース名を結び付けます。 |
| `Link` | 下流のリソースが参照するリンク状態を表します。 |
| `PPPoEInterface` | PPPoE 用の下位インターフェース設定を表します。 |
| `PPPoESession` | `routerd-pppoe-client` が管理する PPPoE セッションです。 |
| `WireGuardInterface` | WireGuard インターフェースを表します。 |
| `WireGuardPeer` | WireGuard の相手を表します。 |
| `TailscaleNode` | Tailscale ノードを設定します。Exit node と subnet router の広告を管理対象 systemd ユニットで行います。 |
| `IPsecConnection` | strongSwan の cloud VPN 向け接続定義を表します。 |
| `VRF` | Linux VRF デバイスと経路表を表します。 |
| `VXLANTunnel` | VXLAN トンネルを表します。 |

`PPPoEInterface.spec.disabled` を `true` にすると、PPPoE の定義は残したまま、管理対象の pppd ユニットを停止・無効化します。
通常運用では PPPoE セッション枠を使わず、必要なときだけ手動で試験する fallback 経路に使えます。

`TailscaleNode` は初回登録用に `authKey` を使えます。
本番設定では `authKeyEnv` と `authKeyFile` を推奨します。
これにより、秘密値を YAML と Git 履歴に残しません。
どちらも未指定の場合、`tailscaled` はログイン済みとみなします。
routerd は広告するノード設定だけを再適用します。
Tailscale の既定 UDP/41641 は予約済みとして扱います。
WireGuard の待ち受けポートには別の番号を使ってください。
詳しい設定手順は Tailscale の設定ガイドを参照してください。

`WireGuardInterface` は `privateKeyFile` を受け取れます。
秘密鍵を router YAML の外に置くためです。
`WireGuardPeer` も任意の PSK 用に `presharedKeyFile` を受け取れます。
インライン鍵フィールドは主に例とテスト向けです。
FreeBSD では、routerd が rc.d サービスを生成します。
そのサービスは `wg` インターフェースを作成し、ファイルから秘密鍵を読み込み、
宣言された peer と static address を適用します。

`KernelModule` は Linux bootstrap resource です。`runtime: true` で即時ロードし、`persistent: true` で modules-load.d を書きます。NixOS は宣言的設定側の所有として扱い、FreeBSD は Linux module loading と同等扱いせず未対応として表示します。

## WAN アドレスと委譲

| Kind | 役割 |
| --- | --- |
| `IPv4StaticAddress` | 静的 IPv4 アドレスを付与します。 |
| `DHCPv4Lease` | `routerd-dhcpv4-client` が DHCPv4 リース、IPv4 アドレス、任意のデフォルト経路を管理します。 |
| `DHCPv6Address` | DHCPv6 IA_NA の意図を表します。 |
| `DHCPv6PrefixDelegation` | `routerd-dhcpv6-client` が管理する DHCPv6-PD リースです。 |
| `DHCPv6Information` | DHCPv6 情報要求の結果です。DNS、SNTP、ドメイン検索、AFTR 情報を観測します。 |
| `IPv6DelegatedAddress` | 委譲プレフィックスから LAN 側アドレスを導出します。 |
| `IPv6RAAddress` | RA/SLAAC で得る IPv6 アドレスを表します。 |

`DHCPv6PrefixDelegation` は旧 OS クライアント選択フィールドを持ちません。
DHCPv6-PD は `routerd-dhcpv6-client` が担当します。

## LAN 側サービス

| Kind | 役割 |
| --- | --- |
| `DHCPv4Server` | dnsmasq で DHCPv4 アドレスプールを提供します。 |
| `DHCPv4Scope` | DHCPv4 のプール範囲を表します。 |
| `DHCPv4Reservation` | MAC アドレスごとの固定割り当てを表します。 |
| `DHCPv4Relay` | dnsmasq の DHCPv4 中継を表します。 |
| `IPv6RouterAdvertisement` | RA、PIO、RDNSS、DNSSL、M/O フラグ、MTU、優先度、寿命を生成します。 |
| `DHCPv6Server` | dnsmasq の DHCPv6 サーバーです。`stateless`、`stateful`、`both` を扱います。 |
| `DHCPv6Scope` | DHCPv6 の範囲を表します。 |
| `DNSZone` | ローカル権威ゾーンを表します。手動レコードと DHCP リース由来のレコードを扱います。 |
| `DNSResolver` | `routerd-dns-resolver` が管理する DNS 待ち受け、応答元、上流、キャッシュを表します。 |

Android は DHCPv6 の DNS だけでは名前解決を完結できないため、IPv6 LAN では `IPv6RouterAdvertisement.spec.rdnss` を設定します。

dnsmasq は DHCPv4、DHCPv6、中継、RA だけを担当します。
DNS の待ち受けと応答は `DNSResolver` が担当します。
LAN の DNS suffix は、`DHCPv4Server.spec.domainFrom`、
`IPv6RouterAdvertisement.spec.dnsslFrom`、`DHCPv6Server.spec.domainSearchFrom`
から `DNSZone/<name>.zone` を参照して、ローカルゾーンと一致させられます。
`DNSResolver.spec.sources` では、ローカルゾーン、条件付き転送、既定の上流を優先順に並べます。
`https://` は DoH、`tls://` は DoT、`quic://` は DoQ、`udp://` は平文 DNS です。
`listen` は複数指定できます。
待ち受けごとに利用する `sources` の部分集合を選べます。
`sources[].viaInterface` は特定インターフェース経由の送信を指定します。
`sources[].bootstrapResolver` は DoH や DoT の名前解決に使う補助 DNS サーバーです。
DNSSEC は `DNSZone.spec.dnssec` と `DNSResolver.spec.sources[].dnssecValidate` で指定します。

## DS-Lite、経路、NAT

| Kind | 役割 |
| --- | --- |
| `DSLiteTunnel` | AFTR へ `ip6tnl` トンネルを張ります。AFTR は直接 IPv6、FQDN、または DHCPv6 情報から得ます。 |
| `IPAddressSet` | 直接指定したアドレスや FQDN から、再利用可能な IP address set を定義します。Linux nftables renderer はこれを named set として出力し、redirect、NAT、policy routing から参照できます。 |
| `IPv4Route` | IPv4 経路を追加します。DS-Lite 経由の既定経路や、明示的な破棄経路にも使います。 |
| `NAT44Rule` | nftables の `routerd_nat` テーブルで IPv4 NAPT を行います。 |
| `IPv4SourceNAT` | 旧来の IPv4 送信元 NAT リソースです。新しい設定では `NAT44Rule` を優先します。 |
| `PortForward` | WAN 側の IPv4 TCP/UDP ポートを 1 つの内部 IPv4 宛先へ DNAT します。 |
| `IngressService` | WAN 側の IPv4 TCP/UDP サービスを公開します。最初の実装では backend は 1 つだけ対応し、backend pool と health check は後続 controller 向けに予約しています。 |
| `LocalServiceRedirect` | LAN 側 client から `IPAddressSet` 宛てに出る IPv4/IPv6 通信を router の local port へ redirect します。平文 DNS/NTP の集約を想定し、DoH や DoT の port には触れません。 |
| `IPv4PolicyRoute` | IPv4 ポリシールーティングを表します。 |
| `IPv4PolicyRouteSet` | 複数のポリシールートをまとめます。 |
| `IPv4DefaultRoutePolicy` | 既定経路の方針を表します。 |
| `IPv4ReversePathFilter` | reverse path filter を表します。 |
| `PathMTUPolicy` | MTU と TCP MSS 調整の方針を表します。`mtu.source: probe` では DF 付きの疎通確認で経路 MTU を測ります。 |

`IPv4PolicyRoute` と `IPv4PolicyRouteSet` は、CIDR 指定に加えて
`destinationSetRefs` と `excludeDestinationSetRefs` を持ちます。FQDN-backed な
宛先 set を policy resource にアドレス展開せず、経路制御や除外条件として使えます。

`IPv4PolicyRoute`、`IPv4PolicyRouteSet`、`IPv4DefaultRoutePolicy` は `excludeDestinationCIDRs` を持ちます。これにより、LAN 内部、管理網、HGW LAN、RFC 1918 の内部網などを policy routing の対象から外せます。

`FirewallRule` は宛先 CIDR に加えて `destinationSetRefs` と
`excludeDestinationSetRefs` を持ちます。これにより、再利用可能な FQDN-backed set
を各 rule にアドレス展開せず、許可・拒否・reject の条件として使えます。

`NAT44Rule` は `destinationCIDRs`、`destinationSetRefs`、`excludeDestinationCIDRs`、
`excludeDestinationSetRefs` を持ちます。これにより、インターネット向け通信だけを
マスカレードし、静的経路を持つプライベート宛先や再利用可能な address set は NAT
しない構成にできます。

`PortForward` と `IngressService` は Linux nftables と FreeBSD pf に DNAT を生成します。
`spec.hairpin.enabled: true` と `spec.hairpin.interfaces` を指定すると、LAN
クライアントから WAN アドレス経由で同じサービスへ到達する hairpin NAT も生成します。
hairpin は `listen.address` または `listen.addressFrom` が必須で、routerd は LAN 側
DNAT と戻り経路用の masquerade/NAT reflection を生成します。
`listen.addressFrom` と backend の `addressFrom` は `IPv4StaticAddress/<name>.address`
のような静的に描画できるアドレスリソースを参照できます。
動的な status source は後続の runtime controller 向けに残しています。

`IPAddressSet` は直接指定した IPv4/IPv6 address を apply 時に nftables named set へ
出力します。FQDN の `A`/`AAAA` record は runtime controller が解決し、参照されている
set を firewall、NAT、policy table 全体を reload せずにその場で更新します。次回更新は
観測した最小 DNS TTL の半分を基本にし、60 秒より短くはしません。`refreshInterval` は
より積極的に更新したい場合の上限として使えます。

`IPAddressSet.spec.names` は完全一致の DNS 名だけを扱います。`microsoft.com` は
`microsoft.com` 自体の `A`/`AAAA` record を意味し、`www.microsoft.com`、
`login.microsoft.com`、`*.microsoft.com`、さらに深いサブドメインは含みません。
ワイルドカードや suffix 形式のサービス判定には、単純な FQDN 解決ではなく、
DNS query 観測や provider endpoint feed を扱う別リソースが必要です。

`LocalServiceRedirect` は Linux nftables の `prerouting` に `redirect` rule を生成します。
指定した interface から入ってきた packet と `IPAddressSet` 宛先だけを対象にします。
router 自身が発信する通信や health check はこの hook を通りません。

例:

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: PortForward
metadata:
  name: web-admin
spec:
  listen:
    interface: wan
    addressFrom:
      resource: IPv4StaticAddress/wan-ip
      field: address
    protocol: tcp
    port: 8443
  target:
    address: 172.18.1.88
    port: 443
  hairpin:
    enabled: true
    interfaces:
      - lan
```

DS-Lite、IPv4 既定経路、NAT44 は実 lab で動作確認済みです。

## 状態連携

| Kind | 役割 |
| --- | --- |
| `HealthCheck` | `routerd-healthcheck` または開発用の組み込み実行器で到達性を測ります。`sourceInterface` はネットワークリソース名を受け取り、実行時に OS のインターフェース名へ解決します。`via`、`fwmark`、`sourceAddress`、`sourceAddressFrom` も指定できます。 |
| `EgressRoutePolicy` | 準備完了の候補から重みの高い外向き経路を選びます。`destinationCIDRs` と candidate の `gatewaySource`、`gateway` を持ちます。 |
| `EventRule` | イベント列に対して all_of、any_of、sequence、window、absence、throttle、debounce、count を評価します。 |
| `DerivedEvent` | 複数リソースの状態から仮想イベントを発行します。 |
| `SelfAddressPolicy` | 自ホストアドレスの選択方針を表します。 |
| `StatePolicy` | 状態管理の方針を表します。 |

`HealthCheck.spec.disabled` を `true` にすると、daemon ユニットは生成しますが停止・無効化します。
`EgressRoutePolicy` の候補にも `disabled: true` を指定できます。
無効化した候補は、最後の観測状態が Healthy のままでも選択されません。

`HealthCheck.spec.sourceInterface` は実行時に OS のインターフェース名へ解決されます。
Linux では `SO_BINDTODEVICE` を使います。`fwmark` を指定した場合は
`SO_MARK` も設定します。`HealthCheck` が `IPv4DefaultRoutePolicy` の
candidate や `IPv4PolicyRouteSet` の target から参照されている場合は、
routerd がその route target の mark から `SO_MARK` を自動導出します。
直接の `fwmark` 指定は、route target に紐づかない低レベルな probe 向けです。
FreeBSD では、指定したインターフェースから送信元アドレスを選びます。
FreeBSD には Linux と同じ socket option がないためです。

## システム

| Kind | 役割 |
| --- | --- |
| `Hostname` | ホスト名を管理します。 |
| `Sysctl` | sysctl 値を管理します。 |
| `NTPClient` | NTP クライアント設定を管理します。`serverFrom` で `DHCPv4Lease.status.ntpServers` や `DHCPv6Information.status.sntpServers` を参照できます。 |
| `LogSink` | ログ送信先を表します。 |
| `WebConsole` | 状態、イベント、IPv4/IPv6 コネクション観測を表示する読み取り専用画面です。 |
| `NixOSHost` | NixOS 宣言設定の生成に使います。 |

`WebConsole.spec.listenAddressFrom` は、ほかのリソース状態から HTTP 待ち受けアドレスを導出します。
たとえば、`Interface/mgmt.status.ipv4Addresses` を参照できます。
管理アドレスを DHCP、IPAM、別の宣言リソースから得る場合は、固定の `listenAddress` ではなくこちらを使います。

## ファイアウォール

| Kind | 役割 |
| --- | --- |
| `FirewallZone` | インターフェースをゾーンへ割り当て、`untrust`、`trust`、`mgmt` の役割を設定します。 |
| `FirewallPolicy` | 拒否ログなど、全体の設定を表します。 |
| `FirewallRule` | 役割の組み合わせでは表せない例外を表します。送信元 CIDR、宛先 CIDR、`IPAddressSet` 宛先参照で範囲を絞れます。 |
| `ClientPolicy` | MAC アドレスでクライアントを分類し、Linux nftables でゲスト隔離を行います。 |
| `PortForward` | 単一宛先の ingress DNAT ルールを追加します。routerd が firewall table も管理している場合は内部の forward accept も生成します。任意の hairpin mode では LAN 側 DNAT と戻り経路 SNAT も生成します。 |
| `IngressService` | `PortForward` と同じ単一 backend の ingress DNAT を追加します。複数 backend、選択方針、health check はまだ有効化していません。任意の hairpin mode も `PortForward` と同じです。 |
| `LocalServiceRedirect` | 明示的な `IPAddressSet` 宛ての通信を local service へ redirect します。firewall renderer は送信元 zone から該当 local input port への開口も生成します。 |

状態を持つフィルタは nftables の `inet routerd_filter` テーブルに生成します。
確立済み通信、loopback、必要な ICMPv6 は常に許可します。
DHCP、DNS、DS-Lite などに必要な開口は routerd が内部で生成します。

`ClientPolicy` は `mode: include` で「一覧に書いた MAC アドレスを guest」として扱います。
`mode: exclude` では「一覧に書いた MAC アドレスを trusted」とし、対象インターフェース上の残りを guest として扱います。
`spec.macs` は短縮形で、`classification[]` は名前や予約参照を残したい場合に使います。
`spec.isolation` では internet 許可、LAN/mgmt 拒否、mDNS/SSDP/NetBIOS discovery 拒否といった典型的な guest intent を表現できます。
FreeBSD pf は同じ MAC ベースの routed filtering モデルを持たないため、このリソースは FreeBSD では未対応として扱います。

## 名前変更の要点

Phase 1.6 で次の名前に整理しました。

| 旧名 | 現在の名前 |
| --- | --- |
| `IPv4DHCPServer` | `DHCPv4Server` |
| `IPv4DHCPReservation` | `DHCPv4Reservation` |
| `IPv4DHCPScope` | `DHCPv4Scope` |
| `IPv6DHCPAddress` | `DHCPv6Address` |
| `IPv6PrefixDelegation` | `DHCPv6PrefixDelegation` |
| `IPv6DHCPServer` / `IPv6DHCPv6Server` | `DHCPv6Server` |
| `IPv6DHCPScope` | `DHCPv6Scope` |
| `DHCPRelay` | `DHCPv4Relay` |

バイナリ名も `routerd-dhcpv4-client`、`routerd-dhcpv6-client` です。
