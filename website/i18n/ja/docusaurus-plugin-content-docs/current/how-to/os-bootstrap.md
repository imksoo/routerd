---
title: ルーターホストを宣言的に bootstrap する
---

# ルーターホストを宣言的に bootstrap する

routerd は、初回構築時に手作業になりがちなホスト準備を YAML に寄せられます。インストーラーの代替ではなく、ルーター固有の差分を shell 履歴ではなく設定として残すための機能です。

## パッケージ

routerd は config 内の resource から通常の OS package dependency を自動導出します。`Package` は、まだ導出できない依存だけを補う narrow override として使います。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Package
metadata:
  name: router-service-dependencies
spec:
  packages:
    - os: ubuntu
      manager: apt
      names:
        - dnsmasq
        - nftables
        - conntrack
        - kmod
        - wireguard-tools
        - tailscale
    - os: alpine
      manager: apk
      names:
        - dnsmasq
        - nftables
        - conntrack-tools
        - iproute2
        - wireguard-tools
        - tailscale
    - os: freebsd
      manager: pkg
      names:
        - dnsmasq
        - wireguard-tools
        - mpd5
```

## Kernel module

Linux の kernel module は、NAT、firewall log、traffic flow log、WireGuard などの resource から自動導出します。`KernelModule` は user-facing config kind ではありません。

## Sysctl

routerd は forwarding、conntrack accounting、reverse path filter、redirect、TCP、RA の sysctl を router resource から自動導出します。通常 config に `SysctlProfile` を書きません。

`SysctlProfile` は、routerd がまだ導出できない platform 固有 kernel 設定を補う narrow escape hatch としてだけ使います。差分だけ `overrides` で指定します。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: SysctlProfile
metadata:
  name: router-runtime
spec:
  profile: router-linux
  runtime: true
  persistent: true
  overrides:
    net.netfilter.nf_conntrack_udp_timeout: "60"
```

## 既存ホスト設定の引き継ぎ

systemd-networkd / systemd-resolved の adoption drop-in は、`Interface`、DHCP、DNS、RA などの resource から自動導出します。DHCP、DNS、PPPoE、healthcheck、Tailscale などの routerd managed unit も、それぞれの resource kind から生成されるので重複定義しないでください。

Ubuntu 26.04 LTS では、RA の状態によっては、installer が書いた netplan で `dhcp6: false` にしていても
systemd-networkd が interface 上で DHCPv6 client socket を開くことがあります。
routerd が所有する WAN/LAN link では、OS bootstrap 時に `accept-ra: false` も明示し、
installer netplan レイヤーでは IPv6 link-local のみにしてください。
これにより UDP port 546 を `routerd-dhcpv6-client` が使える状態に保ち、OS の初期ネットワーク設定が routerd の DHCPv6-PD や RA/DHCPv6 renderer と競合するのを避けられます。
management DHCP は別の management interface に残してください。

```yaml
network:
  version: 2
  renderer: networkd
  ethernets:
    wan0:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    lan0:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    mgmt0:
      dhcp4: true
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
```

provider DNS や AFTR 解決のために WAN link で RA 由来の IPv6 default route が必要な場合は、その WAN interface と DHCPv6 / RA resource を宣言します。routerd は必要な systemd-networkd drop-in を導出し、systemd-networkd の DHCP client は競合しないようにします。

Alpine / OpenRC では、`routerd render alpine --out-dir <dir>` が明示的な `generated service artifacts`、managed dnsmasq、`routerd-healthcheck`、DHCP client、DNS resolver、firewall logger、PPPoE、Tailscale の OpenRC script を生成できます。
apply 時は `/etc/init.d` に script を配置し、現在の OpenRC 状態に差分がある場合だけ `rc-update` と `rc-service` を実行します。
自動生成された DNS resolver script は、controller loop 外で runtime config を materialize できるまでは enable / start しません。
systemd-only の意味は模倣しません。
systemd-networkd / resolved 向け drop-in、systemd sandboxing field、timesyncd 所有は、Alpine native な意味を持つまで OpenRC では未対応として扱います。
