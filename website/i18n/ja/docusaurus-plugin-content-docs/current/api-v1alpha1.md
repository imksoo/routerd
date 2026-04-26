---
title: リソース API v1alpha1
slug: /reference/api-v1alpha1
---

# API v1alpha1

routerd の設定は Kubernetes 風のリソース形状を使います。

- `apiVersion`
- `kind`
- `metadata.name`
- `spec`
- 必要に応じて `status`

## API グループ

- `routerd.net/v1alpha1`: 最上位の `Router`
- `net.routerd.net/v1alpha1`: ネットワークリソース
- `system.routerd.net/v1alpha1`: ローカルシステムリソース
- `plugin.routerd.net/v1alpha1`: プラグインマニフェスト

## 主なリソース

- `Interface`
- `PPPoEInterface`
- `IPv4StaticAddress`
- `IPv4DHCPAddress`
- `IPv4DHCPServer`
- `IPv4DHCPScope`
- `HealthCheck`
- `IPv4DefaultRoutePolicy`
- `IPv4SourceNAT`
- `IPv4PolicyRoute`
- `IPv4PolicyRouteSet`
- `IPv4ReversePathFilter`
- `IPv6DHCPAddress`
- `IPv6PrefixDelegation`
- `IPv6DelegatedAddress`
- `IPv6DHCPServer`
- `IPv6DHCPScope`
- `SelfAddressPolicy`
- `DNSConditionalForwarder`
- `DSLiteTunnel`
- `LogSink`
- `NTPClient`
- `Hostname`
- `Sysctl`

## インターフェースの所有

`Interface.spec.managed` は、routerd がそのインターフェースを変更してよいかを示します。

- `managed: false`: 観測と別名解決だけを行い、リンク状態やアドレス状態は変更しません。
- `managed: true`: routerd がそのインターフェースを管理できます。ただし cloud-init や netplan の既存所有が見える場合は、いきなり奪わず `RequiresAdoption` として計画に出します。

## PPPoEInterface

`PPPoEInterface` は、別の `Interface` の上に PPPoE インターフェースを作るリソースです。Linux では pppd/rp-pppoe の接続先設定、CHAP/PAP secrets、任意の systemd ユニットを生成します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: PPPoEInterface
metadata:
  name: wan-ppp
spec:
  interface: wan-ether
  ifname: ppp0
  username: user@example.jp
  passwordFile: /usr/local/etc/routerd/pppoe-password
  defaultRoute: true
  usePeerDNS: true
  managed: true
  mtu: 1492
  mru: 1492
```

`interface` は下位の Ethernet `Interface` を参照します。`ifname` は省略すると `ppp-<metadata.name>` ですが、Linux のインターフェース名制限に合わせて15文字以内である必要があります。
`password` と `passwordFile` はどちらか一方だけを指定します。認証情報を主設定 YAML に置かないため、通常は `passwordFile` を推奨します。

`managed: true` の場合、routerd は `routerd-pppoe-<name>.service` を有効化して起動します。`managed: false` の場合は設定ファイルだけを生成し、ユニットは起動しません。

## LogSink

`system.routerd.net/v1alpha1` の `LogSink` は、routerd の内部イベントをどこへ出すかを宣言します。

ローカルの journald/syslog に出す場合:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogSink
metadata:
  name: local-syslog
spec:
  type: syslog
  minLevel: info
  syslog:
    facility: local6
    tag: routerd
```

信頼済みローカル log plugin に出す場合:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogSink
metadata:
  name: external-log
spec:
  type: plugin
  minLevel: warning
  plugin:
    path: /usr/local/libexec/routerd/log-sinks/example
    timeout: 5s
```

`enabled` は省略時 `true`、`minLevel` は省略時 `info` です。`syslog.facility` は省略時 `local6`、`syslog.tag` は省略時 `routerd` です。
リモート syslog へ送る場合は `syslog.network` と `syslog.address` を指定します。例: `network: udp`、`address: syslog.example.net:514`。

## NTPClient

`system.routerd.net/v1alpha1` の `NTPClient` はローカル NTP クライアントを宣言します。初期実装では `systemd-timesyncd` と静的サーバー指定を管理します。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: NTPClient
metadata:
  name: system-time
spec:
  provider: systemd-timesyncd
  managed: true
  source: static
  interface: wan
  servers:
    - pool.ntp.org
```

`interface` を指定した場合、routerd は systemd-networkd のリンク別 `NTP=` としてそのインターフェースに NTP サーバーを設定します。省略時は `systemd-timesyncd` のグローバルサーバーとして設定します。

## Sysctl

`system.routerd.net/v1alpha1` の `Sysctl` は kernel parameter を宣言します。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Sysctl
metadata:
  name: ipv4-forwarding
spec:
  key: net.ipv4.ip_forward
  value: "1"
  runtime: true
  persistent: false
```

現在 `runtime: true` は実行中kernel値へ反映します。`persistent: true` は sysctl.d や rc.conf への永続化用として予約されています。

## SelfAddressPolicy

`SelfAddressPolicy` は `dnsSource: self` がどのローカルアドレスを選ぶかを定義します。LAN 用の委譲アドレスと DS-Lite 送信元アドレスのように、同じインターフェースに複数の IPv6 アドレスがある場合の選択を明示できます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: SelfAddressPolicy
metadata:
  name: lan-ipv6-self
spec:
  addressFamily: ipv6
  candidates:
    - source: delegatedAddress
      delegatedAddress: lan-ipv6-pd-address
      addressSuffix: "::3"
    - source: interfaceAddress
      interface: lan
      matchSuffix: "::3"
    - source: interfaceAddress
      interface: lan
      ordinal: 1
```

`IPv6DHCPScope` から参照します。

```yaml
spec:
  dnsSource: self
  selfAddressPolicy: lan-ipv6-self
```

候補は上から順番に評価され、最初に解決できたアドレスを使います。省略時は、IPv6 DHCP スコープの `IPv6DelegatedAddress.addressSuffix` を使った委譲アドレス、サフィックスが一致する観測済みアドレス、観測済みグローバルアドレスの先頭、という順番になります。

## IPv6 Prefix Delegation

`IPv6PrefixDelegation` は上流インターフェースで DHCPv6-PD を要求します。`IPv6DelegatedAddress` は委譲プレフィックスと固定サフィックスを組み合わせて、下流インターフェースの IPv6 アドレスを作ります。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6PrefixDelegation
metadata:
  name: wan-pd
spec:
  interface: wan
  client: networkd
  profile: ntt-hgw-lan-pd
  prefixLength: 60
```

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6DelegatedAddress
metadata:
  name: lan-ipv6-pd-address
spec:
  prefixDelegation: wan-pd
  interface: lan
  subnetID: "0"
  addressSuffix: "::3"
  announce: true
```

Linux では systemd-networkd の drop-in を `/etc/systemd/network/10-netplan-<ifname>.network.d/` に出力します。

NTT向けprofile:

- `ntt-ngn-direct-hikari-denwa`
- `ntt-hgw-lan-pd`

どちらも現時点では IA_PD のみを前提にし、prefix hint は明示がなければ `/60` を使います。

## DHCP と DNS

`IPv4DHCPServer` は DHCPv4 サーバーのインスタンス、`IPv4DHCPScope` はインターフェースとアドレスプール、配布オプションを定義します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DHCPServer
metadata:
  name: dhcp4
spec:
  server: dnsmasq
  managed: true
  listenInterfaces:
    - lan
  dns:
    enabled: true
    upstreamSource: dhcp4
    upstreamInterface: wan
    cacheSize: 1000
```

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DHCPScope
metadata:
  name: lan-dhcp4
spec:
  server: dhcp4
  interface: lan
  rangeStart: 192.168.160.100
  rangeEnd: 192.168.160.199
  leaseTime: 12h
  routerSource: interfaceAddress
  dnsSource: self
  authoritative: true
```

`IPv6DHCPServer` と `IPv6DHCPScope` は dnsmasq による DHCPv6/RA を扱います。`dnsSource: self` は委譲された LAN IPv6 アドレス、たとえば `pd-prefix::3` を DNS サーバーとして広告します。

dnsmasq の RA が有効な場合、routerd は同じ IPv6 DNS サーバー一覧を DHCPv6 DNS と RA RDNSS の両方に使います。Android は DHCPv6 クライアントとして期待せず、SLAAC/RDNSS クライアントとして扱う方が自然です。

dnsmasq 利用時は `listenInterfaces` がサービス提供インターフェースの許可リストです。スコープはサーバー側の `listenInterfaces` に含まれるインターフェースにだけ結び付けられます。指定されていないインターフェースは `except-interface` として出力されるため、WAN であっても LAN であっても明示しない限りサービスしません。

## HealthCheck と IPv4DefaultRoutePolicy

`HealthCheck` は疎通確認を宣言します。`interval` を省略した場合のデフォルトは `60s` です。経路切替が鋭敏になりすぎないよう、短い間隔は明示した場合だけ使います。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: dslite-v4
spec:
  type: ping
  targetSource: dsliteRemote
  interface: transix-a
```

`IPv4DefaultRoutePolicy` は、正常な候補のうち `priority` が最小のものを有効にします。候補は直接インターフェースを指すか、`IPv4PolicyRouteSet` を `routeSet` で参照します。直接候補は専用のルーティングテーブルとファイアウォールマークを持ちます。新しい通信フローは有効な直接候補へマークされ、既存フローはその候補が正常な間は conntrack マークで同じ経路を維持します。旧候補が異常になった場合は、該当フローも現在の有効候補へマークし直します。

有効候補が `routeSet` を参照する場合、routerd は新規フローをマークせず、参照先の `IPv4PolicyRouteSet` がハッシュで対象を選べるようにします。正常な経路セット対象の conntrack マークは維持します。失敗した候補の古いマークは消去し、経路セットに再選出させます。

`target` を省略すると `targetSource: auto` として近傍の確認先を選びます。DS-Lite は AFTR の IPv6 アドレス、通常のインターフェースや PPPoE はそのインターフェースの IPv4 標準ゲートウェイを確認します。これは次ホップやトンネル終端の生存確認です。IPv4 インターネット全体の到達性を見たい場合は、明示的な静的 IPv4 確認先を持つ別の `HealthCheck` を設定します。候補に `healthCheck` を指定しない場合、その候補は常に有効として扱います。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DefaultRoutePolicy
metadata:
  name: default-v4
spec:
  mode: priority
  sourceCIDRs:
    - 192.168.160.0/24
  destinationCIDRs:
    - 0.0.0.0/0
  candidates:
    - name: dslite
      routeSet: lan-dslite-balance
      priority: 10
      healthCheck: dslite-v4
    - name: pppoe
      interface: wan-pppoe
      gatewaySource: none
      priority: 20
      table: 111
      mark: 273
      routeMetric: 60
      healthCheck: pppoe-v4
    - name: dhcp4
      interface: wan
      gatewaySource: dhcp4
      priority: 30
      table: 112
      mark: 274
      routeMetric: 100
      healthCheck: wan-dhcp4-v4
```

## IPv4 Source NAT

`IPv4SourceNAT` は outbound NAT を宣言します。Linux では nftables にrenderされます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4SourceNAT
metadata:
  name: lan-to-transix-a
spec:
  outboundInterface: transix-a
  sourceCIDRs:
    - 192.168.160.0/24
  translation:
    type: address
    address: 192.0.0.2
    portMapping:
      type: range
      start: 1024
      end: 65535
```

`outboundInterface` は `Interface`、`PPPoEInterface`、または `DSLiteTunnel` を参照できます。

## IPv4PolicyRouteSet

`IPv4PolicyRouteSet` は、送信元/宛先アドレスをハッシュして複数のポリシー経路対象へ分散します。Linux では nftables マーク、conntrack マーク、`ip rule`、専用ルーティングテーブルを使います。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4PolicyRouteSet
metadata:
  name: lan-dslite-balance
spec:
  mode: hash
  hashFields:
    - sourceAddress
    - destinationAddress
  sourceCIDRs:
    - 192.168.160.0/24
  destinationCIDRs:
    - 0.0.0.0/0
  targets:
    - name: transix-a
      outboundInterface: transix-a
      table: 100
      priority: 10000
      mark: 256
    - name: transix-b
      outboundInterface: transix-b
      table: 101
      priority: 10001
      mark: 257
```

同じflowは conntrack mark によって同じtargetへ固定されます。

## IPv4ReversePathFilter

policy routing や複数DS-Lite tunnelでは、Linux `rp_filter` が正当な戻り通信を落とす場合があります。`IPv4ReversePathFilter` はそれをconfigで制御します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4ReversePathFilter
metadata:
  name: rp-filter-transix-a
spec:
  target: interface
  interface: transix-a
  mode: disabled
```

`target` は `all`、`default`、`interface`。`mode` は `disabled`、`strict`、`loose` です。

## DNSConditionalForwarder

特定domainだけ別DNSへforwardします。DS-Lite AFTR名のようにprovider DNSでしか正しいAAAAが返らない名前に使います。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DNSConditionalForwarder
metadata:
  name: transix-aftr
spec:
  domain: gw.transix.jp
  upstreamSource: static
  upstreamServers:
    - 2404:1a8:7f01:a::3
    - 2404:1a8:7f01:b::3
```

## DSLiteTunnel

`DSLiteTunnel` は DS-Lite B4 tunnel を宣言します。Linux では `ip -6 tunnel` の `ipip6` tunnel を作ります。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DSLiteTunnel
metadata:
  name: transix-a
spec:
  interface: wan
  tunnelName: ds-transix-a
  aftrFQDN: gw.transix.jp
  aftrDNSServers:
    - 2404:1a8:7f01:a::3
    - 2404:1a8:7f01:b::3
  aftrAddressOrdinal: 1
  aftrAddressSelection: ordinalModulo
  localAddressSource: delegatedAddress
  localDelegatedAddress: lan-ipv6-pd-address
  localAddressSuffix: "::100"
  mtu: 1460
```

`remoteAddress` を省略すると、`aftrFQDN` のAAAAを引きます。AAAAは文字列昇順にsortされ、`aftrAddressOrdinal` で1始まりの番号を選びます。

`aftrAddressSelection`:

- `ordinal`: 指定番号が存在しなければ失敗します。
- `ordinalModulo`: record数で折り返します。

AFTR レコード数が減っても複数トンネルを維持したい場合、`localAddressSuffix` はトンネルごとに分けてください。そうしないと同じ `(local, remote)` のトンネルが重複する可能性があります。
