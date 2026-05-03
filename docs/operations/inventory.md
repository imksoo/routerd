---
title: ホストインベントリ
slug: /operations/inventory
---

# ホストインベントリ

routerd は、ホストの OS、使えるコマンド、ネットワーク機能を確認します。
この情報は、生成器と適用処理が OS 差分を判断するために使います。

## 確認する内容

- OS と版
- systemd、rc.d、NixOS などのサービス管理方式
- iproute2、nftables、dnsmasq、pppd、WireGuard などのコマンド
- IPv6、VRF、VXLAN、WireGuard などのカーネル機能
- `/run/routerd` や `/var/lib/routerd` の利用可否

## 使い道

Ubuntu では systemd と Linux のネットワーク機能を使います。
NixOS では宣言的な設定生成を優先します。
FreeBSD では daemon(8) や rc.d の経路を使います。

未対応の組み合わせでは、実行時に中途半端に失敗するより、計画や検証の段階で明示します。
