---
title: NixOS から始める
---

# NixOS から始める

NixOS は routerd の第二対象です。
Phase 1.7 では、router02 の DHCPv6-PD デーモンを宣言的な NixOS 設定へ移しました。

## 現在の推奨範囲

NixOS では、まず DHCPv6-PD デーモンの宣言的管理から使います。
すべての routerd リソースを NixOS ネイティブ設定へ完全変換する段階ではありません。

## 生成される形

`/etc/nixos/routerd-generated.nix` に systemd ユニットを生成し、次のように反映します。

```bash
sudo nixos-rebuild test
sudo nixos-rebuild switch
```

ユニットは `routerd-dhcpv6-client` を明示パスで起動します。
`RuntimeDirectory`、`StateDirectory`、`ProtectSystem=strict`、必要な capability などを持ちます。

## 注意

NixOS では一時的に `/run/systemd/system` へ置いたユニットは永続設定ではありません。
再起動後も維持するには、NixOS 設定へ入れる必要があります。
routerd の他リソースの NixOS 対応は段階的に進めます。
