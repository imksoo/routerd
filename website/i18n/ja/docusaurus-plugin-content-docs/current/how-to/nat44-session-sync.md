---
title: HA ルータ向け NAT44 セッション同期
slug: /how-to/nat44-session-sync
---

# HA ルータ向け NAT44 セッション同期

![NAT44SessionSync が active router の conntrack SNAT entry を dump し、SSH restore し、insert failure を standby status に出す流れ](/img/diagrams/how-to-nat44-session-sync.png)

`NAT44SessionSync` は、LAN 側ゲートウェイの役割を共有する 2 台の
routerd ノードで、active ノードの NAT44 conntrack セッションを standby
ノードへ同期するためのリソースです。初期実装は snapshot 方式です。
routerd は選択した SNAT アドレスごとにローカル conntrack テーブルを取得し、
一致するエントリを各ターゲットに復元します。

通常は `spec.when` で active ノードだけが動くようにします。VRRP 構成では
ローカル `VirtualAddress` の role を条件にするのが基本です。

## 対象 NAT rule を同期する

同期したい SNAT アドレスを持つ NAT rule を参照します。動的 SNAT アドレスは
`NAT44Rule` の status から読みます。そのため、session sync が active になる
前に NAT44 コントローラーが `snatAddress` を解決している必要があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: dslite-abc-sessions
  spec:
    mode: snapshot
    interval: 2s
    natRules:
      - NAT44Rule/lan-to-dslite-a
      - NAT44Rule/lan-to-dslite-b
      - NAT44Rule/lan-to-dslite-c
    excludeNatRules:
      - NAT44Rule/lan-to-dslite-ra
    targets:
      - name: standby
        host: routerd-standby.lan.example
        user: routerd
        restoreCommand: [sudo, conntrack]
    when:
      state:
        VirtualAddress/lan-vip.role:
          equals: master
```

アドレスが固定であれば、`snatAddresses` で直接指定できます。

```yaml
spec:
  snatAddresses: [192.0.0.2, 192.0.0.3, 192.0.0.4]
```

## 復元の仕組み

コントローラーは以下を実行します。

```bash
conntrack --dump -o extended -n <snat-address>
```

`extended` 出力には conntrack mark が含まれます。routerd は各行を
delete-then-insert の復元スクリプトへ変換し、SSH 経由でターゲットに送ります。
既存フローを同じ出口経路に残すには `ct mark` の維持が重要です。

`restoreCommand` の既定値は `[conntrack]` です。ターゲット側のユーザーに権限昇格が必要な場合は `[sudo, conntrack]` を指定します。

## 確認する

```bash
routerctl describe NAT44SessionSync/dslite-abc-sessions
routerd serve --controllers nat44-session-sync --config router.yaml
```

`spec.when` が false の間は `Pending` / `WhenFalse` になります。参照した
`NAT44Rule` がまだ `snatAddress` を解決していない場合は、`Pending` /
`SNATAddressPending` になります。
