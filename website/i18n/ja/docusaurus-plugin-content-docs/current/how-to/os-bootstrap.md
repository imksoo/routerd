---
title: ルーターホストを宣言的に bootstrap する
---

# ルーターホストを宣言的に bootstrap する

routerd は、初回構築時に手作業になりがちなホスト準備を YAML に寄せられます。インストーラーの代替ではなく、ルーター固有の差分を shell 履歴ではなく設定として残すための機能です。

## パッケージ

`Package` で、routerd の controller や managed daemon が必要とする OS パッケージを宣言します。

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

Linux で conntrack、NFLOG、WireGuard などの module を明示したい場合は `KernelModule` を使います。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: KernelModule
metadata:
  name: router-kernel-modules
spec:
  modules:
    - nf_conntrack
    - nfnetlink_log
    - wireguard
  runtime: true
  persistent: true
  optional: true
```

Ubuntu / Debian では `runtime: true` が `modprobe` を実行し、`persistent: true` が `/etc/modules-load.d/90-routerd-<name>.conf` を書きます。NixOS では NixOS 設定側で所有すべきものとして宣言的状態を記録します。FreeBSD では未対応として表示します。

## Sysctl

`SysctlProfile` は forwarding、conntrack accounting、ルーター用途向け kernel default をまとめます。差分だけ `overrides` で指定します。

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

`NetworkAdoption` は、systemd-networkd や systemd-resolved の既存設定が routerd と競合する場合に使います。`generated service artifacts` は明示的なローカル unit を routerd から配置・有効化したい場合に使います。DHCP、DNS、PPPoE、healthcheck、Tailscale などの routerd managed unit は、それぞれの resource kind から生成されるので重複定義しないでください。

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

provider DNS や AFTR 解決のために WAN link で RA 由来の IPv6 default route が必要な場合は、
その interface に `NetworkAdoption` を使います。routerd は systemd-networkd drop-in として
RA を受ける設定を書きつつ、systemd-networkd の DHCPv6 client は無効のままにします。

Alpine / OpenRC では、`routerd render alpine --out-dir <dir>` が明示的な `generated service artifacts`、managed dnsmasq、`routerd-healthcheck`、DHCP client、DNS resolver、firewall logger、PPPoE、Tailscale の OpenRC script を生成できます。
apply 時は `/etc/init.d` に script を配置し、現在の OpenRC 状態に差分がある場合だけ `rc-update` と `rc-service` を実行します。
自動生成された DNS resolver script は、controller loop 外で runtime config を materialize できるまでは enable / start しません。
systemd-only の意味は模倣しません。
systemd-networkd / resolved 向けの `NetworkAdoption` drop-in、systemd sandboxing field、timesyncd 所有は、Alpine native な意味を持つまで OpenRC では未対応として扱います。
