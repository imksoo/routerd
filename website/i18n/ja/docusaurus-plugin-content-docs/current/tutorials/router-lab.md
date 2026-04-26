---
title: ルータラボ
---

# ルータラボ

このチュートリアルでは `examples/router-lab.yaml` の構成を順に説明します。routerd が育ててきたリソースの大半をひと通り触れる、コンパクトなラボ向けルータ設定です。本番ホストにそのまま貼り付ける前提ではありません。インターフェース名、プレフィックス、認証情報、上流の挙動は環境ごとに違うため、必ず読み替えが必要です。

## ラボが宣言する内容

ラボ設定は次を組み合わせています。

- WAN 側 Ethernet で DHCPv4 と DHCPv6 プレフィックス委譲を受ける。
- LAN 側 Ethernet に IPv4 静的アドレスと、委譲プレフィックス由来の IPv6 アドレスを載せる。
- LAN 側に dnsmasq で DHCP / DNS / RA を提供する。
- WAN 経由で AFTR に向かう DS-Lite トンネルを張る。
- 複数の上流に対するヘルスチェックを伴う IPv4 デフォルト経路ポリシー。

YAML の各リソースが、[リソース API リファレンス](/ja/docs/reference/api-v1alpha1) のどの振る舞いに対応するかを照らし合わせながら読むと、構成の意図が掴みやすくなります。

## まず検証と計画を確認する

```bash
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
```

計画上は、DHCP と DNS のサービスが指定した LAN インターフェースだけに乗ることを確認できます。各 DHCP サーバリソースは `listenInterfaces` で提供を許可するインターフェースを列挙し、許可されていないインターフェースに紐付けようとするスコープは計画段階で拒否されます。

## DHCP と DNS の構成

ラボ設定では、dnsmasq インスタンスを 1 つ立て、LAN に紐付けています。

```yaml
kind: IPv4DHCPServer
spec:
  server: dnsmasq
  managed: true
  listenInterfaces:
    - lan
```

IPv6 では `dnsSource: self` のとき、dnsmasq が委譲された LAN アドレス（例: `pd-prefix::3`）を DNS サーバとして広告します。dnsmasq はこの DNS サーバを DHCPv6 と RA RDNSS の両方に流すため、RA RDNSS でしか受け取らない Android のクライアントでも、DHCPv6 を使うクライアントと同じ DNS サーバにたどり着けます。

## NTP とイベント送出

ラボ設定にはシステム側のリソースも含まれています。

```yaml
kind: NTPClient
spec:
  provider: systemd-timesyncd
  managed: true
  source: static
  interface: wan
  servers:
    - pool.ntp.org
```

`interface` を指定すると、routerd はそのリンクに対する `NTP=` の追加設定を systemd-networkd 経由で書き出します。`LogSink` リソースを使うと、routerd の内部イベントをローカルの journald や syslog、あるいはリモートの syslog 送信先に流せます。他のリソースの動きには影響しません。

## 反映は慎重に

実機に向けるときは、必ず予行実行を先に通します。

```bash
sudo /usr/local/sbin/routerd reconcile \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

cloud-init や別のネットワーク管理ツールが握っているインターフェースを routerd が奪わないことを確かめてから、`--dry-run` を外します。判断に迷う場合は、先に `routerd adopt --candidates` で候補を確認し、[リソース所有の手順](/ja/docs/reference/resource-ownership) に従って既存の構成物を台帳に取り込んでから反映するのが安全です。
