---
title: インストール
sidebar_position: 1
---

# インストール

このページでは Ubuntu Server に routerd をソースからインストールする手順を示します。
NixOS と FreeBSD は secondary プラットフォームとして対応していますが、最初の評価には Ubuntu の lab VM が一番スムーズです。

## Ubuntu の依存パッケージ

最初に runtime と diagnostics のパッケージを入れます。
(同じ内容を `Package` リソースとして宣言しておけば、起動時に不足を検出させることもできます。)

```bash
sudo apt-get update
sudo apt-get install -y \
  dnsmasq-base nftables conntrack iproute2 \
  iputils-ping iputils-tracepath dnsutils tcpdump traceroute \
  procps ppp wireguard-tools strongswan-swanctl radvd \
  systemd net-tools kmod
```

各パッケージの用途：

| パッケージ | 用途 |
| --- | --- |
| `dnsmasq-base` | DHCPv4、DHCPv6、RA |
| `nftables` | NAT、route mark、stateful filter |
| `conntrack` | 実時間 IPv4/IPv6 コネクション観測 |
| `iproute2` | アドレス、経路、DS-Lite、VRF、VXLAN、WireGuard デバイス |
| `ppp` | PPPoE (`pppd` + `rp-pppoe.so`) |
| `wireguard-tools` | `wg setconf` と状態確認 |
| `strongswan-swanctl` | クラウド VPN 向け IPsec |
| `radvd` | 任意の radvd RA path (既定は dnsmasq) |
| `dnsutils` / `iputils-*` / `tcpdump` / `traceroute` | 検証と障害調査 |
| `procps` / `systemd` / `net-tools` / `kmod` | sysctl、サービス管理、kernel module 確認 |

## ビルド

```bash
make build
```

主なバイナリ：

- `routerd`
- `routerctl`
- `routerd-dhcpv6-client`
- `routerd-dhcpv4-client`
- `routerd-pppoe-client`
- `routerd-healthcheck`

## インストール先

標準配置は `/usr/local` 配下です。

| 種類 | パス |
| --- | --- |
| 設定 | `/usr/local/etc/routerd/router.yaml` |
| 実行ファイル | `/usr/local/sbin` |
| プラグイン | `/usr/local/libexec/routerd/plugins` |
| 実行時 socket | `/run/routerd` |
| 永続状態 | `/var/lib/routerd` |

## 最初の確認

ビルド後、スキーマと付属 example を確認します：

```bash
make check-schema
make validate-example
make dry-run-example
```

本番ルーターへ apply する前に、対象設定で必ず dry-run apply を行ってください：

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan     --config /usr/local/etc/routerd/router.yaml
routerd apply    --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

## systemd で動かす

本体は `routerd serve` として常駐します。
DHCPv6-PD、DHCPv4、PPPoE、healthcheck は routerd が管理する別プロセスです。

初回投入時は、自動起動を有効にする前に管理 SSH 経路が生きていることを確認してください。
WAN 側を変更する前に、コンソール (シリアルやハイパーバイザー) を必ず確保してください。
