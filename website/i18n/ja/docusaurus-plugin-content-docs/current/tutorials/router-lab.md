---
title: 検証用ラボの組み方
---

# 検証用ラボの組み方

routerd を本番ネットワークに入れる前に、隔離されたラボ環境で評価することを推奨します。
このページでは、仮想化ハイパーバイザーで複数のルーター VM を立ち上げ、cross-OS で routerd の挙動を確認するためのレファレンス構成を紹介します。

特定のハードウェアを前提にしませんが、Proxmox VE、KVM、VMware、Hyper-V のいずれでも組めます。

## 想定するシナリオ

新しいホームルーター・SOHO ルーターを routerd で組む前に、以下を確認したいケースを想定しています。

- DHCPv6-PD、DS-Lite、PPPoE、NAT44、firewall、DNS の各機能が想定どおり動くか
- Linux、FreeBSD、NixOS のいずれで動かすかを比較したい
- ある変更を本番に出す前に、手元で同じ構成を組んで挙動差を見たい

ラボでは「壊しても本番に影響しない」「OS や構成を入れ替えやすい」「routerd のリソース YAML をそのまま流用できる」状態を作るのが目的です。

## ハードウェア要件

評価対象のすべてが VM 上で動きます。物理ルーターは不要です。

- 仮想化ホスト 1 台 (Proxmox VE、KVM、VMware、Hyper-V のいずれか)
- 4 GB 以上のメモリ余裕 (1 VM あたり 512 MB 程度)
- 上流ネットワーク (DHCPv6-PD を試すなら IPv6 PD を配布する HGW か模擬サーバーが必要)

## 推奨される VM 構成

最小構成は **2 VM**、推奨は **4-5 VM** です。

| 役割 | OS の例 | 確認できる機能 |
| --- | --- | --- |
| 計測ホスト | Ubuntu / Debian | `iperf3`、`dig`、`curl`、`mtr` など計測ツール |
| WAN 側ルーター A | Ubuntu | `routerd-dhcpv6-client`、PPPoE、DS-Lite |
| WAN 側ルーター B | NixOS | NixOS の declarative module 経由で routerd を運用 |
| WAN 側ルーター C | FreeBSD | FreeBSD `pf` + DS-Lite + rc.d unit |
| LAN 側ルーター | Ubuntu | controller chain、DNS、firewall、NAT、`HealthCheck`、Web Console |

VM どうしを共通の virtual switch (VLAN trunk または untagged bridge) で接続し、上流とは別ネットワークで隔離します。
各 VM の WAN NIC を「上流側 vSwitch」、LAN NIC を「ラボ内 vSwitch」へ接続するのが最小構成です。

## チェックリスト

ラボが正しく組めたかどうかは、以下を確認します。

1. **DHCPv6-PD 取得**: 各ルーター VM で `routerctl status` が `DHCPv6PrefixDelegation` を `Bound` にしている。
2. **PD 重複なし**: 5 台以上立てる場合、配布された prefix が互いに重ならない (HGW 側が IA_PD ごとに分割する設定であること)。
3. **IPv6 疎通**: ラボ VM どうしで link-local と GUA の双方向 ping が通る。
4. **DS-Lite 試験**: `routerctl describe DSLiteTunnel/<name>` が `Up`、`HealthCheck` が `Healthy`、`curl --interface ds-lite-X http://example.com/` が応答を返す。
5. **NixOS 経路**: `nixos-rebuild test` で routerd の宣言設定が反映され、再起動後も同じ状態になる。
6. **FreeBSD 経路**: `pfctl -sr` に routerd が生成した pf ルールが入っており、`service routerd onestatus` が active。

## 実施しないでよい確認

以下はネットワーク機器側 (HGW、ISP) の挙動依存で、routerd 単体ではコントロールできません。
ラボでこの再現に時間を使わないでください。

- HGW が DHCPv6 情報要求 (info-request) で AFTR option を返さない場合 → `DSLiteTunnel.spec.aftrFQDN` の静的フォールバックを使う。
- 一部 ISP の PD 払い出しが特定の IA_ID にしか応答しない → IA_ID をクライアント側で固定する。
- 物理スイッチの VLAN 設定差で IPv6 RA が通らない → 物理スイッチを通さない経路 (vSwitch 内) で評価する。

これらは `docs/knowledge-base/` に既知事象としてまとめてあります。

## 次にやること

ラボが動いたら、以下のチュートリアルへ進んでください。

- [routerd を最初に動かす](./first-router.md)
- [LAN 向けサービスを構成する](./lan-side-services.md)
- [基本的な firewall を組む](./basic-firewall.md)
- [マルチ WAN を組む](../how-to/multi-wan.md)
