---
title: ホストインベントリ
slug: /operations/inventory
---

# ホストインベントリ

routerd は apply の開始時にホストの素性を小さく観測して
`routerd.net/v1alpha1/Inventory/host` として記録します。apply、render、運用者が
推測ではなく観測値で判断できるようにするためのものです。

## 収集する内容

| フィールド | 取得元 |
|---|---|
| OS 名とバージョン | runtime + `/etc/os-release` |
| カーネル名とバージョン | `uname` |
| 仮想化判定 | `systemd-detect-virt` (Linux)、`kern.vm_guest` (FreeBSD) |
| DMI ベンダー (取れる範囲) | `/sys/class/dmi/id/sys_vendor` |
| サービス管理 | systemd / rc.d を判定 |
| 使えるコマンド | `nft`、`pf`、`dnsmasq`、`dhcp6c`、`sysctl`、... |

インベントリは **観測のみ** です。YAML に書きません。

## 確認方法

```bash
routerctl describe inventory/host
routerctl show inventory/host -o yaml
```

`routerctl describe` は複数行の要約を出します。`routerctl show` は完全な
構造データ (SQLite の `objects` 行と同じ JSON) を出します。

## 現状の使い道

- 運用者がホストの素性を確認できる (Ubuntu と NixOS の間で移行したり、Proxmox 上の
  VM とベアメタルを行き来したときに特に有用)
- apply はインベントリが前の周から変わると `InventoryObserved` イベントを記録する。
  たとえばカーネルを上げた等の足跡が残る

## これから使う方向

将来の routerd ではレンダラがインベントリで分岐するようにしていきます。

- サービス管理が `rc.d` のホストでは systemd-networkd の経路を取らない
- 仮想ホストでは `multicast_snooping=0` を提案する
- 必須コマンド (`dnsmasq` が未インストール、など) が無ければ早期に失敗させる

現実装はまだ記録するだけで、レンダラはインベントリを参照していません。
これは意図的で、まず観測値の基礎を整えてから分岐を増やす方針です。

## プライバシー上の注意

インベントリは routerd のローカル状態 DB に置かれ、外部送信はしません。
DB をバックアップするとインベントリ行も一緒についてきます。DMI ベンダーと OS
バージョンはフリート内ではしばしば識別子になり得るので、`/etc/os-release` と
同じ扱いの慎重さで DB を扱ってください。
