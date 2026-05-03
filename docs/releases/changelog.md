---
title: Changelog
---

# Changelog

routerd は出荷前のソフトウェアです。
この履歴は、利用者が現在の API 名と実装済み範囲を間違えないために残します。

## Unreleased

- Phase 1.8: 文書を現在のコードに合わせて全面整理しました。
  旧 DHCPv6 クライアント経路、旧 Kind 名、古いラボ前提を利用者向け本文から取り除きました。
- Phase 1.7: router02 の NixOS 設定を宣言的な `routerd-dhcpv6-client` ユニットへ移しました。
  `/etc/nixos/routerd-generated.nix` を使い、`nixos-rebuild test` と `switch` で反映します。
- Phase 1.6: DHCP 関連 Kind とバイナリ名を RFC 表記へ整理しました。
  `routerd-dhcpv4-client` と `routerd-dhcpv6-client` が現在名です。
  旧名の互換別名はありません。
- Phase 1.5e: router05 で DS-Lite を実適用しました。
  条件付き DNS 転送で AFTR FQDN を解決し、ip6tnl、IPv4 既定経路、NAT44、IPv4 HTTP 通信を確認しました。
- Phase 1.5d: `routerd-pppoe-client` と `PPPoESession` を追加しました。
  Linux は pppd と rp-pppoe、FreeBSD は ppp(8) の経路を持ちます。
- Phase 1.5c: `routerd-dhcpv4-client` と `DHCPv4Lease` を追加しました。
  DHCPv4 リースを lease ファイルと Unix ソケット API で管理します。
- Phase 1.5b: `NAT44Rule` と conntrack 観測を追加しました。
  `/proc/net/nf_conntrack` がない環境では、sysctl 由来の集計へ縮退します。
- Phase 1.5a: dnsmasq による LAN 側サービスを拡張しました。
  DHCPv4、DHCPv6、RA、DNS 応答、条件付き転送を 1 つの管理対象 dnsmasq 設定へ統合します。
- Phase 2-B: `WANEgressPolicy`、`EventRule`、`DerivedEvent`、`HealthCheck` を追加しました。
- Phase 2-A: DHCPv6 情報要求、DS-Lite、IPv4 経路、RA、DHCPv6 サーバーをカスケードに追加しました。
- Phase 1: DHCPv6-PD から LAN アドレス、DNS 応答へつながる最初の controller chain を追加しました。
- Phase 0: `routerd-dhcpv6-client`、daemon contract、bus、controller framework を追加しました。

## 0.1.0

初期の v1alpha1 実装です。
この版以降、出荷前の整理として API 名や実装方針を大きく変更しています。
現在の設定例では、この changelog の `Unreleased` に書かれた名前を使ってください。
