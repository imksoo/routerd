# ファイアウォールゾーンを定義する

`FirewallZone` で、インターフェースと役割を対応付けます。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: wan
  spec:
    role: untrust
    interfaces:
      - Interface/wan
      - DSLiteTunnel/ds-lite

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: lan
  spec:
    role: trust
    interfaces:
      - Interface/lan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: management
  spec:
    role: mgmt
    interfaces:
      - Interface/mgmt
```

`untrust` は WAN 側の経路に使います。`trust` は通常の LAN に使います。
`mgmt` は管理用ネットワークに使います。既定の動作は役割の組み合わせで
決まります。そのため、家庭用ルーターではゾーンだけで始められます。
