# API v1alpha1

routerd の config は Kubernetes 風のリソース形状を使います。

- `apiVersion`
- `kind`
- `metadata.name`
- `spec`
- 必要に応じて `status`

## API Groups

- `routerd.net/v1alpha1`: top-level `Router`
- `net.routerd.net/v1alpha1`: network resources
- `system.routerd.net/v1alpha1`: local system resources
- `plugin.routerd.net/v1alpha1`: plugin manifests

## 主なリソース

- `Interface`
- `IPv4StaticAddress`
- `IPv4DHCPAddress`
- `IPv4DHCPServer`
- `IPv4DHCPScope`
- `IPv4DefaultRoute`
- `IPv4SourceNAT`
- `IPv4PolicyRoute`
- `IPv4PolicyRouteSet`
- `IPv4ReversePathFilter`
- `IPv6DHCPAddress`
- `IPv6PrefixDelegation`
- `IPv6DelegatedAddress`
- `IPv6DHCPServer`
- `IPv6DHCPScope`
- `DNSConditionalForwarder`
- `DSLiteTunnel`
- `Hostname`
- `Sysctl`

## Interface Ownership

`Interface.spec.managed` は、routerd がそのinterfaceを変更してよいかを示します。

- `managed: false`: observe と alias 解決だけを行い、link/address state は変更しません。
- `managed: true`: routerd がそのinterfaceを管理できます。ただし cloud-init や netplan の既存所有が見える場合は、いきなり奪わず `RequiresAdoption` として計画に出します。

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

## IPv6 Prefix Delegation

`IPv6PrefixDelegation` は uplink interface で DHCPv6-PD を要求します。`IPv6DelegatedAddress` は delegated prefix と固定suffixを組み合わせて downstream interface のIPv6 addressを作ります。

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

Linux では systemd-networkd drop-in を `/etc/systemd/network/10-netplan-<ifname>.network.d/` にrenderします。

NTT向けprofile:

- `ntt-ngn-direct-hikari-denwa`
- `ntt-hgw-lan-pd`

どちらも現時点では IA_PD のみを前提にし、prefix hint は明示がなければ `/60` を使います。

## DHCP と DNS

`IPv4DHCPServer` は DHCPv4 server instance、`IPv4DHCPScope` は interface と address pool / options を定義します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DHCPServer
metadata:
  name: dhcp4
spec:
  server: dnsmasq
  managed: true
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

`IPv6DHCPServer` と `IPv6DHCPScope` は dnsmasq による DHCPv6/RA を扱います。`dnsSource: self` は delegated LAN IPv6 address、たとえば `pd-prefix::3` をDNS serverとして広告します。

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

`outboundInterface` は `Interface` または `DSLiteTunnel` を参照できます。

## IPv4PolicyRouteSet

`IPv4PolicyRouteSet` は、source/destination address をhashして複数のpolicy route targetへ分散します。Linux では nftables mark、conntrack mark、`ip rule`、専用routing tableを使います。

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

AFTR record数が減っても複数tunnelを維持したい場合、`localAddressSuffix` はtunnelごとに分けてください。そうしないと同じ `(local, remote)` の tunnel が重複する可能性があります。
