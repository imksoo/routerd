---
title: ルーターラボ
---

# ルーターラボ

このチュートリアルでは `examples/router-lab.yaml` の構成を説明します。routerd が育てているリソースモデルを一通り見るための、小さなルーター設定です。

この例は本番ホストにそのまま貼り付けるものではありません。インターフェース名、プレフィックス、認証情報、上流ネットワークの挙動は環境ごとに異なります。

## トポロジー

ラボモデルは次の構成を想定します。

- `wan`: 上流側の Ethernet インターフェース
- `lan`: 下流側の Ethernet インターフェース
- WAN 側 DHCPv4
- WAN 側 DHCPv6-PD
- LAN 側 静的 IPv4
- LAN 側 dnsmasq DHCP/DNS/RA
- DS-Lite トンネルリソース
- ヘルスチェック付き標準経路ポリシー

## 検証と計画確認

```bash
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
```

計画では DNS/DHCP サービスが明示された LAN インターフェースだけで提供されることを確認します。サーバーは `listenInterfaces` で提供インターフェースを列挙し、スコープが許可されていないインターフェースへ結び付こうとすると拒否されます。

## DHCP と DNS

ラボ設定では次のように書きます。

```yaml
kind: IPv4DHCPServer
spec:
  server: dnsmasq
  managed: true
  listenInterfaces:
    - lan
```

IPv6 では `dnsSource: self` が `pd-prefix::3` のような委譲された LAN アドレスを選びます。dnsmasq はこの DNS サーバーを DHCPv6 と RA RDNSS の両方で広告するため、Android クライアントでも自然に使えます。

## NTP と Syslog

ローカルシステムリソースの例です。

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

`interface` を指定すると、routerd はその systemd-networkd リンクに NTP サーバーを出力します。`LogSink` は routerd の内部イベントをローカル syslog またはリモート syslog 送信先へ送れます。

## 慎重に適用する

実ホストではまず予行実行します。

```bash
sudo /usr/local/sbin/routerd reconcile \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

cloud-init や別のネットワーク管理ツールが所有しているインターフェースを routerd が奪わないことを確認してから `--dry-run` を外します。
