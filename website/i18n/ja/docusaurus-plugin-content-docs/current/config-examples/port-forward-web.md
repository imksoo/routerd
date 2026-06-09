---
title: 内部 Web サーバーへのポートフォワード
sidebar_position: 50
---

# 内部 Web サーバーへのポートフォワード

![内部 HTTPS サーバー向けの PortForward による受信 DNAT、LAN ヘアピンアクセス、ファイアウォールゾーン分離の構成](/img/diagrams/config-example-port-forward-web.png)

内部の HTTPS サーバーを WAN 側の IPv4 アドレスで公開し、LAN クライアントからも同じ公開名で
到達できるよう hairpin を有効にする例です。

完全な YAML は `examples/example-port-forward-web.yaml` にあります。

## 構成図

```mermaid
flowchart LR
  internet((Internet))
  wan["[1] wan<br/>203.0.113.10:443"]
  router["[2] routerd host"]
  lan["[3] lan"]
  web["[4] inside web server<br/>192.168.10.20:443"]
  client["[5] LAN client"]

  internet --- wan --- router --- lan --- web
  client --- lan
```

## 図の対応表

| 番号 | 意味 | 主なリソース |
| --- | --- | --- |
| [1] | 外部クライアントが接続する公開側のアドレスと port。 | `PortForward/web-https.spec.listen` |
| [2] | ingress DNAT と hairpin ルールを生成するルーター。 | `PortForward/web-https` |
| [3] | hairpin のトラフィックが入ってくる LAN インターフェース。 | `PortForward/web-https.spec.hairpin.interfaces` |
| [4] | DNAT 先の内部 HTTPS バックエンド。 | `PortForward/web-https.spec.target` |
| [5] | 公開アドレスや公開 DNS 名を使う LAN クライアント。 | ヘアピン経路 |

## この例で管理するもの

| 領域 | routerd リソース |
| --- | --- |
| 受信 DNAT | `PortForward/web-https` |
| ヘアピンアクセス | `PortForward.spec.hairpin` |
| ゾーンとポリシー | `FirewallZone/wan`, `FirewallZone/lan`, `FirewallPolicy/home` |

## 設定の要点

```yaml
# [1] 公開側のリスナー。ヘアピンではここに具体的なアドレスが必要。
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: web-https
  spec:
    listen:
      interface: wan
      address: 203.0.113.10
      protocol: tcp
      port: 443
    # [4] DNAT された接続を受ける内部バックエンド。
    target:
      address: 192.168.10.20
      port: 443
    # [3] LAN クライアントから同じ公開アドレスを使えるようにする。
    hairpin:
      enabled: true
      interfaces:
        - lan
```

hairpin を使うには、LAN 側から見える公開宛先のアドレスが必要です。
そのため `listen.address` または `listen.addressFrom` を指定します。

## 確認

```bash
routerctl validate --config examples/example-port-forward-web.yaml
routerctl apply --config examples/example-port-forward-web.yaml --dry-run
routerctl describe PortForward/web-https
nft list table ip routerd_nat
```

## よく変えるところ

- `203.0.113.10` を実際の WAN 側 IPv4 アドレスに変えます。
- 公開名がこのアドレスを返すよう、DNS は別途設定します。
- 公開する port は必要最小限にします。
