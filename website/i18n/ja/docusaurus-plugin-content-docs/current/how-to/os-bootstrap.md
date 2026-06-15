---
title: ルーターホストを宣言型でブートストラップする
---

# ルーターホストを宣言型でブートストラップする

![derived package、kernel module、sysctl、adoption drop-in、minimal installer networking による declarative host bootstrap](/img/diagrams/how-to-os-bootstrap.png)

routerd は、初回構築時に手作業になりがちなホスト準備を YAML へ寄せられます。インストーラーの代替ではなく、ルーター固有の差分を shell 履歴ではなく設定として残すための機能です。

## パッケージ

routerd は、設定内のリソースから通常の OS パッケージ依存を自動で導出します。`Package` は、まだ導出できない依存だけを補う、狭いオーバーライドとして使います。

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

## カーネルモジュール

Linux のカーネルモジュールは、NAT、ファイアウォールログ、トラフィックフローログ、WireGuard などのリソースから自動で導出します。`KernelModule` は、利用者が直接書く設定 Kind ではありません。

## Sysctl

routerd は、forwarding、conntrack accounting、reverse path filter、redirect、TCP、RA の sysctl を、ルーターのリソースから自動で導出します。通常、設定に `SysctlProfile` を書く必要はありません。

`SysctlProfile` は、routerd がまだ導出できないプラットフォーム固有のカーネル設定を補う、狭い逃げ道としてだけ使います。差分だけを `overrides` で指定します。

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

systemd-networkd や systemd-resolved の引き継ぎ用 drop-in は、`Interface`、DHCP、DNS、RA などのリソースから自動で導出します。DHCP、DNS、PPPoE、healthcheck、Tailscale などの routerd 管理ユニットも、それぞれのリソース Kind から生成されるため、重複して定義しないでください。

Ubuntu 26.04 LTS では、RA の状態によっては、インストーラーが書いた netplan で `dhcp6: false` にしていても、
systemd-networkd がインターフェース上で DHCPv6 クライアントのソケットを開くことがあります。
routerd が所有する WAN/LAN リンクでは、OS のブートストラップ時に `accept-ra: false` も明示し、
インストーラーの netplan レイヤーでは IPv6 のリンクローカルのみにしてください。
こうすると、UDP ポート 546 を `routerd-dhcpv6-client` が使える状態に保てます。OS の初期ネットワーク設定が、routerd の DHCPv6-PD や RA/DHCPv6 の生成と競合するのを避けられます。
管理用 DHCP は、別の管理インターフェースに残してください。

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

プロバイダー DNS や AFTR の解決のために、WAN リンクで RA 由来の IPv6 デフォルト経路が必要な場合は、その WAN インターフェースと DHCPv6 / RA のリソースを宣言します。routerd は必要な systemd-networkd の drop-in を導出し、systemd-networkd の DHCP クライアントが競合しないようにします。

Alpine / OpenRC では、`routerd render alpine --out-dir <dir>` が、明示的な生成済みサービス成果物、管理対象の dnsmasq、`routerd-healthcheck`、DHCP クライアント、DNS リゾルバー、ファイアウォールロガー、PPPoE、Tailscale の OpenRC スクリプトを生成できます。
適用時は `/etc/init.d` にスクリプトを配置し、現在の OpenRC 状態と差分がある場合だけ `rc-update` と `rc-service` を実行します。
自動生成された DNS リゾルバーのスクリプトは、コントローラーループの外でランタイム設定を実体化できるようになるまでは、enable や start を行いません。
systemd 専用の挙動は模倣しません。
systemd-networkd / resolved 向けの drop-in、systemd のサンドボックス用フィールド、timesyncd の所有は、Alpine ネイティブな意味を持つまで OpenRC では未対応として扱います。

## 適用の順序

リモートのルーターでは、手順を堅実に進めてください。

1. routerd のバイナリと最小限の設定をインストールする。
2. 完全な設定を検証する。
3. dry-run で適用を試す。
4. 管理インターフェースと SSH が保護されていることを確認する。
5. 適用する。
6. `routerctl status`、転送、DNS、Web 管理画面を確認する。
