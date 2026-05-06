---
title: NixOS から始める
---

# NixOS から始める

NixOS は routerd の first-class な secondary プラットフォームです。
NixOS 上では、transient な systemd unit ではなく、宣言的 NixOS 設定経由で routerd 管理サービスを駆動するのが推奨パスです。

## 推奨範囲

NixOS では、まず daemon 型の WAN 側サービスを宣言的経路で管理してください。
DHCPv6-PD、DHCPv4 クライアントリース、PPPoE セッション、HealthCheck、dnsmasq、firewall logging は生成された NixOS module で表現できます。
基礎サービスが `nixos-rebuild test` で正常に収束してから、他のルーターリソースを追加してください。

## 生成物

routerd は systemd unit を `/etc/nixos/routerd-generated.nix` に書き出します。次で適用します：

```bash
sudo nixos-rebuild test
sudo nixos-rebuild switch
```

生成された unit は routerd daemon を明示パスで起動します。
また、適切な `RuntimeDirectory`、`StateDirectory`、`ProtectSystem=strict`、capability を持ちます。

## なぜ transient unit ではないのか

NixOS 上で `/run/systemd/system` に置かれた unit は system 設定の一部ではありません。
再起動や `nixos-rebuild switch` で消えます。
再起動と再ビルドを跨いで生き残らせるには、unit を NixOS 設定として宣言する必要があります。
routerd は `/etc/nixos/routerd-generated.nix` に書き出すことでこれを実現します。

## 現在の対応範囲

実装済み：

- `routerd-dhcpv6-client` の systemd unit 生成
- `routerd-dhcpv4-client` の systemd unit 生成
- `routerd-pppoe-client` の systemd unit 生成
- `Package`、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` の NixOS module 生成
- `nixos-rebuild switch` 後に DHCPv6-PD が `Bound` まで到達
- dnsmasq、DNS resolver、HealthCheck、firewall logger のサービスを生成 module で宣言可能
- WireGuard / Tailscale / VXLAN を NixOS / Linux / FreeBSD 間で確認

未対応：

- Linux 実行時機能すべてに対する NixOS ネイティブ生成
- NixOS の `generation` rollback semantics 連携

詳細は [対応プラットフォーム](../platforms.md) を参照。

## 関連項目

- [インストール](./install.md)
- [最初のルーターを上げる](./first-router.md)
- [WAN 側サービス](./wan-side-services.md)
