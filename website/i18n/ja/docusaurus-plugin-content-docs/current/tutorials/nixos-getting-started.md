---
title: NixOS から始める
---

# NixOS から始める

NixOS は routerd の first-class な secondary プラットフォームです。
NixOS 上では、transient な systemd unit ではなく、宣言的 NixOS 設定経由で routerd 管理サービスを駆動するのが推奨パスです。

## 推奨範囲

NixOS では、まず DHCPv6-PD クライアントを宣言的経路で管理することから始めてください。
NixOS 連携で最も成熟している部分で、観測しやすい end-to-end な振る舞いが得られます。
他のリソースは NixOS module 対応が揃い次第、段階的に追加できます。

## 生成物

routerd は systemd unit を `/etc/nixos/routerd-generated.nix` に書き出します。次で適用します：

```bash
sudo nixos-rebuild test
sudo nixos-rebuild switch
```

生成された unit は `routerd-dhcpv6-client` を明示パスで起動し、適切な `RuntimeDirectory`、`StateDirectory`、`ProtectSystem=strict`、capability を持ちます。

## なぜ transient unit ではないのか

NixOS 上で `/run/systemd/system` に置かれた unit は system 設定の一部ではありません。
再起動や `nixos-rebuild switch` で消えます。
再起動と再ビルドを跨いで生き残らせるには、unit を NixOS 設定として宣言する必要があります。
routerd は `/etc/nixos/routerd-generated.nix` に書き出すことでこれを実現します。

## 現在の対応範囲

実装済み：

- `routerd-dhcpv6-client` の systemd unit 生成
- `Package`、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` の NixOS module 生成
- `nixos-rebuild switch` 後に DHCPv6-PD が `Bound` まで到達
- WireGuard / VXLAN を NixOS / Linux / FreeBSD 間で確認

未対応：

- nftables / dnsmasq / DNS resolver / HealthCheck の end-to-end
- `Package` を Ubuntu リファレンス完全対応にする
- NixOS の `generation` rollback semantics 連携

詳細は [対応プラットフォーム](../platforms.md) を参照。

## 関連項目

- [インストール](./install.md)
- [最初のルーターを上げる](./first-router.md)
- [WAN 側サービス](./wan-side-services.md)
