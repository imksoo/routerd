---
title: Router Lab
---

# Router Lab

このチュートリアルでは `examples/router-lab.yaml` の構成を説明します。routerd が育てている resource model を一通り見るための compact な router config です。

この example は production host にそのまま貼り付けるものではありません。interface name、prefix、credential、upstream の挙動は環境ごとに異なります。

## Topology

lab model は次の構成を想定します。

- `wan`: upstream Ethernet interface
- `lan`: downstream Ethernet interface
- WAN 側 DHCPv4
- WAN 側 DHCPv6-PD
- LAN 側 static IPv4
- LAN 側 dnsmasq DHCP/DNS/RA
- DS-Lite tunnel resource
- health check 付き default route policy

## Validate と Plan

```bash
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
```

plan では DNS/DHCP service が明示された LAN interface だけで提供されることを確認します。server は `listenInterfaces` で提供 interface を列挙し、scope が許可されていない interface に bind しようとすると reject されます。

## DHCP と DNS

lab config では次のように書きます。

```yaml
kind: IPv4DHCPServer
spec:
  server: dnsmasq
  managed: true
  listenInterfaces:
    - lan
```

IPv6 では `dnsSource: self` が `pd-prefix::3` のような delegated LAN address を選びます。dnsmasq はこの DNS server を DHCPv6 と RA RDNSS の両方で広告するため、Android client でも自然に使えます。

## NTP と Syslog

local system resource の例です。

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

`interface` を指定すると、routerd はその systemd-networkd link に NTP server を render します。`LogSink` は routerd の内部 event を local syslog または remote syslog endpoint へ送れます。

## 慎重に適用する

実ホストではまず dry-run します。

```bash
sudo /usr/local/sbin/routerd reconcile \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

cloud-init や別の network manager が所有している interface を routerd が奪わないことを確認してから `--dry-run` を外します。
