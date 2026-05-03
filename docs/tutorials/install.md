---
title: インストール
sidebar_position: 1
---

# インストール

このページでは、Ubuntu Server に routerd をソースから入れる最小手順を説明します。
NixOS と FreeBSD は土台がありますが、最初に試す場合は Ubuntu のラボ VM を推奨します。

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
