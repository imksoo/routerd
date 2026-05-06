---
title: routerd が DHCPv6-PD クライアントを自前で持つ理由
---

# routerd が DHCPv6-PD クライアントを自前で持つ理由

routerd の現在方針では、DHCPv6-PD は専用デーモン `routerd-dhcpv6-client` が担当します。
過去に評価した OS 付属クライアントの経路は、現在の設定例としては使いません。

## 専用デーモンにした理由

DHCPv6-PD は取得だけで終わらず、Renew、再起動復元、event 記録までが重要です。
OS 付属クライアントへ設定を生成するだけでは、routerd の状態モデルと LAN 側反映をきれいに繋げませんでした。

専用デーモンにしたことで次が揃います：

- lease を `lease.json` に保存。
- 起動時に lease を復元。
- Renew 結果を event に記録。
- `/v1/status` で `Bound` / `Pending` を返す。
- 他の controller (LAN アドレス導出、RA、DHCPv6 server、DS-Lite、DNS) が消費する event を発行。

## バイナリと配置

```text
routerd-dhcpv6-client
```

| パス | 用途 |
| --- | --- |
| `/run/routerd/dhcpv6-client/<name>.sock` | リソース別の制御 socket |
| `/var/lib/routerd/dhcpv6-client/<name>/lease.json` | lease 永続化 |
| `/var/lib/routerd/dhcpv6-client/<name>/events.jsonl` | append-only event log |

## 評価して採用しなかった選択肢

`systemd-networkd`、WIDE/KAME 系クライアント、その他 DHCP クライアントを比較しましたが、最終的に routerd 所有の daemon を採用しました。
これらの調査は背景として有用ですが、現在の出荷構成には含まれません。

現在の Kind は `DHCPv6PrefixDelegation` です。OS 付属実装を選ぶ `client` フィールドは意図的に存在しません。

## 運用上の注意

同じ WAN インターフェースで複数の DHCPv6-PD クライアントを並行して動かさないでください。
2 つ同時に出すと上流が混乱して Reply が返らなくなります。

routerd 管理へ移行するときは、まず古いクライアント (とその lease ファイル、それを起動していた systemd / rc.d 設定) を停止してから `routerd-dhcpv6-client` を起動してください。
