---
title: NTT フレッツ IPv6 設定
slug: /how-to/flets-ipv6-setup
---

# NTT フレッツ IPv6 設定

このページでは、NTT フレッツの IPv6 サービスに routerd をつなぐ手順を扱います。
NTT のホームゲートウェイ (HGW) が経路上にあり、DHCPv6-PD で IPv6 プレフィックスを
委譲する構成を前提とします。

## 想定する構成

- 上流に NTT HGW (例: PR-400NE)、IPv6 有効化済み
- ひかり電話契約あり。HGW が DHCPv6-PD サーバーとして動くのは、この契約が
  プロビジョニングされている回線です。
- routerd を動かすホストは HGW の LAN 側にあり、WAN インターフェースは HGW の LAN
  に接続。

## 最小設定

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: wan
  spec:
    ifname: ens18
    adminUp: true
    managed: true

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Address
  metadata:
    name: wan-ra
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: wan-pd
  spec:
    interface: wan
    profile: ntt-hgw-lan-pd
```

`ntt-hgw-lan-pd` プロファイルが妥当な既定値を引き当てます。

- DUID 種別: `link-layer` (MAC 由来)。NTT が文書化する端末モデルは DUID-LL または
  DUID-LLT。
- プレフィックス長: `/60` (HGW が上流プレフィックスを 16 分割し `/60` ごとに渡す)
- Rapid Commit: 無効。NTT のオプション表でこのプロファイルでは未使用。
- IA_NA: 要求しない。NTT のプロファイルは PD のみ。

## ラボでの注意

routerd を Linux 上の仮想マシン (Proxmox など) で動かしていて HGW が Solicit に
応答しないように見える場合、HGW を疑う前に次を確認してください。

- ホスト NIC と VM の間にある Linux bridge で `multicast_snooping=0`。既定の `=1` だと、
  RA や DHCPv6 のマルチキャストが一部のカーネルで黙って drop されます。
- 経路上の L2 スイッチで IGMP / MLD snooping を無効化、もしくは MLD クエリアを
  立てて snooping テーブルを維持。
- `tcpdump` のフィルタは `udp port 546 or udp port 547`。NTT HGW は送信元ポートが
  547 ではなく一時ポートで返してきます。

これらは [設計メモ](../design-notes) の「ラボ環境特有の問題」に記録しています。

## 確認

```bash
routerctl describe ipv6pd/wan-pd
```

`currentPrefix` と直近の `lastObservedAt` が出ていれば取得できています。
HGW の管理画面「DHCPv6 サーバ払い出し状況」にも該当 MAC の行が出るはずです。

## よくあるはまりどころ

- `iaid` を明示してから後で変える。HGW はクライアント識別子で binding を保ちます。
  リース更新の間に識別子が変わると、古い binding は再利用されずに失効待ちになります。
  IAID を決めたらそのまま使ってください。
- routerd を再起動するたびに DHCPv6 Release を送る。NTT HGW は Release を即時に
  扱うので、通常運用の apply サイクルでリース表を揺らしかねません。routerd の NTT
  プロファイルは無関係な設定変更で `dhcp6c` を再起動しません。
- 前回もらったプレフィックスがまた来ると思い込む。NTT HGW は新規取得後に別の `/60`
  を割り当てることがあります。routerd 状態の `lastPrefix` は診断用で、LAN 側 YAML に
  値を埋め込まないでください。

## 関連

- [DHCPv6PrefixDelegation リファレンス](../reference/api-v1alpha1#ipv6prefixdelegation)
- [設計メモ](../design-notes) の元観測
