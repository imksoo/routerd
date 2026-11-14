# DHCPv4Server

[リソース別の設定](../resources.md#dhcpv4server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 任意; object
      end: "<string>" # 任意; string
      leaseTime: "<string>" # 任意; string
      start: "<string>" # 任意; string
    authoritative: true # 任意; boolean
    dns: # 任意; object
      cacheSize: 0 # 任意; integer; minimum=0
      enabled: true # 任意; boolean
      upstreamInterface: "<string>" # 任意; string
      upstreamServers: # 任意; array
        - "<string>" # string
      upstreamSource: "dhcpv4" # 任意; string; enum=["dhcpv4", "static", "system", "none"]
    dnsInterface: "<string>" # 任意; string
    dnsServerFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    dnsServers: # 任意; array
      - "<string>" # string
    dnsSource: "dhcpv4" # 任意; string; enum=["dhcpv4", "static", "self", "none"]
    domain: "<string>" # 任意; string
    domainFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    gateway: "<string>" # 任意; string
    gatewayFrom: # 任意; object
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
    ntpServerFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    ntpServers: # 任意; array
      - "<string>" # string
    options: # 任意; array
      - # object
        code: 1 # 任意; integer; minimum=1; maximum=65535
        name: "<string>" # 任意; string
        value: "<string>" # 任意; string
    profiles: # 任意; array
      - # object
        addressPool: # 必須; object
          end: "<string>" # 任意; string
          leaseTime: "<string>" # 任意; string
          start: "<string>" # 任意; string
        dnsServers: # 任意; array
          - "<string>" # string
        gateway: "<string>" # 任意; string
        name: "<string>" # 必須; string
        netmask: "<string>" # 任意; string
        ntpServers: # 任意; array
          - "<string>" # string
        options: # 任意; array
          - # object
            code: 1 # 任意; integer; minimum=1; maximum=65535
            name: "<string>" # 任意; string
            value: "<string>" # 任意; string
    rangeEnd: "<string>" # 任意; string
    rangeStart: "<string>" # 任意; string
    role: "server" # 任意; string; enum=["server", "transit"]
    router: "<string>" # 任意; string
    routerSource: "interfaceAddress" # 任意; string; enum=["interfaceAddress", "static", "none"]
    scopes: # 任意; array
      - # object
        addressPool: # 必須; object
          end: "<string>" # 任意; string
          leaseTime: "<string>" # 任意; string
          start: "<string>" # 任意; string
        dnsServers: # 任意; array
          - "<string>" # string
        gateway: "<string>" # 任意; string
        match: # 必須; object
          macAddresses: # 任意; array
            - "<string>" # string
          ouiPrefixes: # 任意; array
            - "<string>" # string
        name: "<string>" # 必須; string
        netmask: "<string>" # 任意; string
        ntpServers: # 任意; array
          - "<string>" # string
        options: # 任意; array
          - # object
            code: 1 # 任意; integer; minimum=1; maximum=65535
            name: "<string>" # 任意; string
            value: "<string>" # 任意; string
        profileRef: "<string>" # 任意; string
    server: "dnsmasq" # 任意; string; enum=["dnsmasq", "kea", "dhcpd"]
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
