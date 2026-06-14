---
title: HA ルーターの DHCP リース同期
slug: /how-to/dhcp-lease-sync
---

# HA ルーターの DHCP リース同期

![active DHCP lease sync が platform-derived lease file、VirtualAddress role gate、hardened SSH over rsync、standby warm lease を使う流れ](/img/diagrams/how-to-dhcp-lease-sync.png)

2 台の routerd ノードで DHCP の役割を共有し、active node の lease state を
standby node に温存したい場合は `DHCPv4ServerLeaseSync`、
`DHCPv6ServerLeaseSync`、または `DHCPv6PrefixDelegationLeaseSync` を使います。
これらは active から standby への同期を想定しています。backup が古い lease を
active へ書き戻さないよう、通常は `VirtualAddress` の role で実行を制限します。

完全な例は `examples/dhcp-lease-sync-ha.yaml` にあります。

## 永続化の既定値を使う

routerd は既定で dnsmasq server lease を platform state directory 配下に置きます。
DHCP resource には実装上の lease path を書かない構成にします。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
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
    addressPool:
      start: fd00:30::100
      end: fd00:30::1ff
```

sync resource は source kind から実際の file path を導出するため、設定に実装上の
path を繰り返す必要はありません。

## active node からだけ同期する

lease sync をローカルの `VirtualAddress` status で制限します。VIP role が
`master` の間だけ同期します。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4ServerLeaseSync
  metadata:
    name: lan-v4-leases
  spec:
    source:
      resource: DHCPv4Server/lan-dhcpv4
    interval: 30s
    targets:
      - name: standby
        host: routerd-standby.lan.example
        user: routerd
    when:
      state:
        VirtualAddress/lan-vip.role:
          equals: master
```

stateful DHCPv6 server では `DHCPv6ServerLeaseSync` に
`source.resource: DHCPv6Server/<name>` を指定します。WAN 側の prefix delegation は
`DHCPv6PrefixDelegationLeaseSync` に
`source.resource: DHCPv6PrefixDelegation/<name>` を指定します。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegationLeaseSync
  metadata:
    name: wan-pd-lease
  spec:
    source:
      resource: DHCPv6PrefixDelegation/wan-pd
    targets:
      - name: standby
        host: routerd-standby.lan.example
        user: routerd
    when:
      state:
        VirtualAddress/lan-vip.role:
          equals: master
```

standby が昇格した時は、空の database ではなく、最後に同期された lease state から
開始できます。

## SSH の前提

lease sync は SSH 越しの `rsync` を使います。リソースを有効にする前に、非対話
SSH を準備してください。

- active node の routerd 実行ユーザー用に SSH key を作成または配置する。
- standby node の `target.user` の `authorized_keys` に公開鍵を入れる。
- `target.host` の `known_hosts` を事前に管理する。`BatchMode=yes` により対話的な
  host-key prompt は出せません。
- target user が導出された同期先 directory を作成し、lease file を書けるようにする。

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
    sshOptions:
      - -o
      - ConnectTimeout=5
    options:
      - --timeout=30
```

## 確認

```bash
routerctl validate -f examples/dhcp-lease-sync-ha.yaml --replace
routerctl plan -f examples/dhcp-lease-sync-ha.yaml --replace
routerctl describe VirtualAddress/lan-vip
routerctl describe DHCPv4ServerLeaseSync/lan-v4-leases
```

node が master ではない場合、`routerd serve` は `spec.when` によりこの resource を
除外します。master で lease file が存在する場合、status は `Synced` へ進みます。
lease file がまだ無い場合は、dnsmasq が作成するまで `Pending` のままです。
