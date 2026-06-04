---
title: HA ルーターの DHCP リース同期
slug: /how-to/dhcp-lease-sync
---

# HA ルーターの DHCP リース同期

2 台の routerd ノードで LAN サービスを共有し、active node の dnsmasq lease file
を standby node に温存したい場合は `DHCPLeaseSync` を使います。このリソースは
active から standby への同期を想定しています。backup が古い lease を active へ
書き戻さないよう、通常は `VirtualAddress` の role で実行を制限します。

完全な例は `examples/dhcp-lease-sync-ha.yaml` にあります。

## lease file を永続パスに置く

DHCP server の両方で同じ dnsmasq lease file を明示します。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    leaseFile: /var/lib/routerd/dnsmasq/dnsmasq.leases
    addressPool:
      start: 192.168.30.100
      end: 192.168.30.199

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: lan-dhcpv6
  spec:
    interface: lan
    mode: stateful
    leaseFile: /var/lib/routerd/dnsmasq/dnsmasq.leases
    addressPool:
      start: fd00:30::100
      end: fd00:30::1ff
```

`/var/lib/routerd` は service restart や standby promotion をまたいで残ります。
HA 構成では、権威 DHCP server の lease file を `/run` 配下に置かないでください。

## active node からだけ同期する

`DHCPLeaseSync` をローカルの `VirtualAddress` status で制限します。VIP role が
`master` の間だけ同期します。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPLeaseSync
  metadata:
    name: lan-leases
  spec:
    leaseFile: /var/lib/routerd/dnsmasq/dnsmasq.leases
    interval: 30s
    targets:
      - name: standby
        host: routerd-standby.lan.example
        user: routerd
        path: /var/lib/routerd/dnsmasq/dnsmasq.leases
    when:
      state:
        VirtualAddress/lan-vip.role:
          equals: master
```

standby が昇格した時は、空の lease database ではなく、最後に同期された lease file
から開始できます。

## SSH の前提

`DHCPLeaseSync` は SSH 越しの `rsync` を使います。リソースを有効にする前に、
非対話 SSH を準備してください。

- active node の routerd 実行ユーザー用に SSH key を作成または配置する。
- standby node の `target.user` の `authorized_keys` に公開鍵を入れる。
- `target.host` の `known_hosts` を事前に管理する。`BatchMode=yes` により対話的な
  host-key prompt は出せません。
- target user が同期先 directory を作成し、lease file を書けるようにする。

routerd は、`target.sshOptions` が同じ key を上書きしていない場合、次の SSH
既定値を付けます。

```text
-o BatchMode=yes -o ConnectTimeout=10
```

また `rsync --timeout=60` を付け、各 sync command を controller の context deadline
付きで実行します。SSH option は `target.sshOptions`、rsync timeout は
`target.options` で上書きできます。

```yaml
targets:
  - host: routerd-standby.lan.example
    user: routerd
    path: /var/lib/routerd/dnsmasq/dnsmasq.leases
    sshOptions:
      - -o
      - ConnectTimeout=5
    options:
      - --timeout=30
```

## 確認

```bash
routerd validate --config examples/dhcp-lease-sync-ha.yaml
routerd apply --config examples/dhcp-lease-sync-ha.yaml --once --dry-run
routerctl describe VirtualAddress/lan-vip
routerctl describe DHCPLeaseSync/lan-leases
```

node が master ではない場合、`routerd serve` は `spec.when` によりこの resource を
除外します。master で lease file が存在する場合、status は `Synced` へ進みます。
lease file がまだ無い場合は、dnsmasq が作成するまで `Pending` のままです。
