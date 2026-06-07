---
title: NixOS から始める
---

# NixOS から始める

![routerd release binary、generated NixOS module、declarative service、nixos-rebuild test/switch、rollback を示す NixOS getting started flow](/img/diagrams/tutorial-nixos-getting-started.png)

NixOS は routerd の主要な補助プラットフォームです。
NixOS 上では、一時的な systemd ユニットではなく、宣言型の NixOS 設定を通じて routerd の管理サービスを動かすことを推奨します。
routerd の実行ファイルはリリースアーカイブから導入します。
ただし、OS パッケージは NixOS 設定で管理してください。
`install.sh` は `nix-env` でパッケージを導入せず、警告だけを出します。

## 推奨する進め方

NixOS では、まずデーモン型の WAN 側サービスを宣言型の経路で管理してください。
DHCPv6-PD、DHCPv4 クライアントリース、PPPoE セッション、HealthCheck、dnsmasq、ファイアウォールログ、nftables の有効化、そして主プロセスの `routerd.service` は、生成された NixOS モジュールで表現できます。
基礎となるサービスが `nixos-rebuild test` で正常に収束してから、他のルーターリソースを追加してください。

## 生成物

routerd は、systemd ユニットを `/etc/nixos/routerd-generated.nix` に書き出します。次のコマンドで適用します。

```bash
sudo nixos-rebuild test
sudo nixos-rebuild switch
```

生成されたユニットは、routerd デーモンを明示的なパスで起動します。
あわせて、適切な `RuntimeDirectory`、`StateDirectory`、`ProtectSystem=strict`、capability を持ちます。

## なぜ一時的なユニットではないのか

NixOS では、`/run/systemd/system` に置かれたユニットはシステム設定の一部になりません。
再起動や `nixos-rebuild switch` で消えてしまいます。
再起動と再ビルドをまたいで残すには、ユニットを NixOS 設定として宣言する必要があります。
routerd は、`/etc/nixos/routerd-generated.nix` に書き出すことでこれを実現します。

## 現在の対応範囲

次は実装済みです。

- `routerd-dhcpv6-client` の systemd ユニット生成
- `routerd-dhcpv4-client` の systemd ユニット生成
- `routerd-pppoe-client` の systemd ユニット生成
- `Package` override、`SysctlProfile`、derived host runtime artifact、`generated service artifacts` の NixOS モジュール生成
- `nixos-rebuild switch` 後に DHCPv6-PD が `Bound` へ到達すること
- dnsmasq、DNS リゾルバ、HealthCheck、ファイアウォールロガー、Tailscale、DHCPv4 クライアント、DHCPv6 クライアント、PPPoE クライアントのサービスを生成モジュールで宣言できること
- NAT、firewall、policy routing、Path MTU リソースが必要とする nftables の有効化
- `nixos-rebuild switch` の失敗時に `nixos-rebuild switch --rollback` を試みること
- WireGuard / Tailscale / VXLAN が NixOS / Linux / FreeBSD 間で動作することの確認

詳細は [対応プラットフォーム](../platforms.md) を参照してください。

## 関連項目

- [インストール](./install.md)
- [最初のルーターを上げる](./first-router.md)
- [WAN 側サービス](./wan-side-services.md)
