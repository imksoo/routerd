---
title: インストール
sidebar_position: 1
---

# インストール

このページでは、Ubuntu Server に routerd をソースから入れる最小手順を説明します。
NixOS と FreeBSD は土台がありますが、最初に試す場合は Ubuntu のラボ VM を推奨します。

## Ubuntu の依存パッケージ

Ubuntu では、次のパッケージを先に入れます。
routerd の `Package` リソースでも同じ内容を宣言できます。
実運用では YAML に書き、起動時に不足を検出させます。

```bash
sudo apt-get update
sudo apt-get install -y dnsmasq-base nftables conntrack iproute2 iputils-ping iputils-tracepath dnsutils tcpdump traceroute procps ppp wireguard-tools strongswan-swanctl radvd systemd net-tools kmod
```

主な用途は次の通りです。

| パッケージ | 用途 |
| --- | --- |
| `dnsmasq-base` | DHCPv4、DHCPv6、RA の配布 |
| `nftables` | NAT、経路印、ファイアウォール |
| `conntrack` | IPv4/IPv6 コネクションの観測と切り分け |
| `iproute2` | アドレス、経路、DS-Lite、VRF、VXLAN、WireGuard デバイス |
| `ppp` | PPPoE。`pppd` と `rp-pppoe.so` を使います。 |
| `wireguard-tools` | `wg setconf` と状態観測 |
| `strongswan-swanctl` | cloud VPN 向け IPsec 接続 |
| `radvd` | radvd 経路の RA 送信。通常は dnsmasq 経路を優先します。 |
| `dnsutils` / `iputils-ping` / `iputils-tracepath` / `tcpdump` / `traceroute` | 検証と障害調査 |
| `procps` / `systemd` / `net-tools` / `kmod` | sysctl、サービス管理、互換診断、カーネルモジュール確認 |

## ビルド

```bash
make build
```

主な実行ファイルは次の通りです。

- `routerd`
- `routerctl`
- `routerd-dhcpv6-client`
- `routerd-dhcpv4-client`
- `routerd-pppoe-client`
- `routerd-healthcheck`

## インストール先

標準の配置は `/usr/local` 配下です。

| 種類 | パス |
| --- | --- |
| 設定 | `/usr/local/etc/routerd/router.yaml` |
| 実行ファイル | `/usr/local/sbin` |
| プラグイン | `/usr/local/libexec/routerd/plugins` |
| 実行時ソケット | `/run/routerd` |
| 状態 | `/var/lib/routerd` |

## 最初の確認

ビルド後、スキーマと例を確認します。

```bash
make check-schema
make validate-example
make dry-run-example
```

本番ルーターへ適用する前に、必ず予行実行を行います。

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

## systemd で動かす

本体は `routerd serve` として常駐します。
DHCPv6-PD、DHCPv4、PPPoE、ヘルスチェックは専用デーモンとして別に動きます。

最初の実機では、管理用 SSH が残ることを確認してから自動起動へ進みます。
WAN 側の設定を変更する前に、コンソールまたは PVE などの帯域外手段を用意してください。
