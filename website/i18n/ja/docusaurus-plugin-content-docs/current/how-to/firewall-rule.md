# ファイアウォール規則を追加する

`FirewallRule` は、役割の組み合わせでは表せない例外だけに使います。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: allow-admin-ssh
  spec:
    fromZone: management
    toZone: self
    protocol: tcp
    port: 22
    action: accept
```

`fromZone` と `toZone` には `FirewallZone` の名前を書きます。`toZone: self`
はルーターホスト自身を表します。

規則は、役割から決まる既定動作より先に評価されます。routerd が内部で
生成する開口も、ユーザー定義の規則より先に評価されます。これにより、
DHCP、DNS、DS-Lite などの管理対象サービスが止まりにくくなります。

適用前にローカルで確認します。

```sh
routerctl firewall test from=wan to=self proto=tcp dport=22
routerctl describe firewall
```
