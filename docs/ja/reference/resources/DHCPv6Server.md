# DHCPv6Server

[リソース別の設定](../resources.md#dhcpv6server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 任意; object
      end: "<string>" # 任意; string
      leaseTime: "<string>" # 任意; string
      start: "<string>" # 任意; string
    configPath: "<string>" # 任意; string
    defaultRoute: true # 任意; boolean
    delegatedAddress: "<string>" # 任意; string
    dependsOn: # 任意; array
      - # object
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        notEmpty: true # 任意; boolean
        optional: true # 任意; boolean
        phase: "<string>" # 任意; string
        resource: "<string>" # 必須; string
    dnsServerFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    dnsServers: # 任意; array
      - "<string>" # string
    dnsSource: "self" # 任意; string; enum=["self", "static", "none"]
    domainSearch: # 任意; array
      - "<string>" # string
    domainSearchFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    interface: "<string>" # 任意; string
    leaseFile: "<string>" # 任意; string
    leaseTime: "<string>" # 任意; string
    listenInterfaces: # 任意; array
      - "<string>" # string
    logDHCP: true # 任意; boolean
    managed: true # 任意; boolean
    mode: "stateless" # 任意; string; enum=["stateless", "stateful", "both", "ra-only"]
    pidFile: "<string>" # 任意; string
    rapidCommit: true # 任意; boolean
    role: "server" # 任意; string; enum=["server", "transit"]
    selfAddressPolicy: "<string>" # 任意; string
    server: "dnsmasq" # 任意; string; enum=["dnsmasq"]
    sntpServerFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    sntpServers: # 任意; array
      - "<string>" # string
    stickyHoldDays: 0 # 任意; integer; minimum=0
    when: # 任意; object
      state: # 任意; object
        "<key>": # 任意; object
          contains: "<string>" # 任意; string
          equals: "<string>" # 任意; string
          exists: true # 任意; boolean
          for: "<string>" # 任意; string
          in: # 任意; array
            - "<string>" # string
          maxAge: "<string>" # 任意; string
          status: "set" # 任意; string; enum=["set", "unset", "unknown"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
