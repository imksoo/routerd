---
title: routerd が DHCPv6-PD クライアントを自前で持つ理由
---

# routerd が DHCPv6-PD クライアントを自前で持つ理由

![Diagram showing why routerd owns DHCPv6-PD from OS client variation and stale prefix risk through routerd-dhcpv6-client lease state, status, delegated LAN address inputs, and HA DUID operation](/img/diagrams/knowledge-base-dhcpv6-pd-clients.png)

routerd の現在の方針では、DHCPv6-PD は専用デーモン `routerd-dhcpv6-client` が担当します。
過去に評価した OS 付属クライアントを使う方法は、現在の設定例としては採用していません。

## 専用デーモンにした理由

DHCPv6-PD は取得だけで終わらず、Renew、再起動後の復元、イベントの記録までが重要です。
OS 付属クライアント向けに設定を生成するだけでは、routerd の状態モデルと LAN 側への反映をきれいにつなげられませんでした。

専用デーモンにしたことで、次が揃います。

- リースを `lease.json` に保存します。
- 起動時にリースを復元します。
- Renew の結果をイベントに記録します。
- `/v1/status` で `Bound` / `Pending` を返します。
- 他のコントローラー (LAN アドレス導出、RA、DHCPv6 サーバー、DS-Lite、DNS) が消費するイベントを発行します。

## バイナリと配置

```text
routerd-dhcpv6-client
```

| パス | 用途 |
| --- | --- |
| `/run/routerd/dhcpv6-client/<name>.sock` | リソースごとの制御ソケット |
| `/var/lib/routerd/dhcpv6-client/<name>/lease.json` | リースの永続化 |
| `/var/lib/routerd/dhcpv6-client/<name>/events.jsonl` | 追記専用のイベントログ |

## 評価して採用しなかった選択肢

`systemd-networkd`、WIDE/KAME 系のクライアント、その他の DHCP クライアントを比較しましたが、最終的に routerd が所有するデーモンを採用しました。
これらの調査は背景として有用ですが、現在の出荷構成には含めていません。

現在の Kind は `DHCPv6PrefixDelegation` です。OS 付属の実装を選ぶ `client` フィールドは、意図的に用意していません。

## 運用上の注意

同じ WAN インターフェースで、複数の DHCPv6-PD クライアントを並行して動かさないでください。
2 つを同時に出すと、上流が混乱して Reply が返らなくなります。

routerd の管理へ移行するときは、まず古いクライアント (とそのリースファイル、それを起動していた systemd / rc.d の設定) を停止してから、`routerd-dhcpv6-client` を起動してください。
