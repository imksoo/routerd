# リソース別の設定

各リソースはRouter.spec.resourcesに記述します。例の名前・インターフェース・アドレス・認証情報・対向設定は環境に合わせて変更してください。必須は親オブジェクト内での指定要否です。リソース間参照や意味上の制約はスキーマだけでは確定せず、routerd validateで確認します。

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、既定値はdefault注記だけで示します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```sh
routerd validate --config router.yaml
```

[JSON Schema](https://github.com/imksoo/routerd/blob/main/schemas/routerd-config-v1alpha1.schema.json) · [YAML](https://github.com/imksoo/routerd/tree/main/examples)

### Router

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: "<router-name>"
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: "<name>"
      spec:
        ifname: "<interface-name>"
```

- [Plugin](#plugin)
- [DynamicConfigSource](#dynamicconfigsource)
- [DynamicOverridePolicy](#dynamicoverridepolicy)
- [LogSink](#logsink)
- [Telemetry](#telemetry)
- [ObservabilityPipeline](#observabilitypipeline)
- [LogRetention](#logretention)
- [Sysctl](#sysctl)
- [SysctlProfile](#sysctlprofile)
- [Package](#package)
- [NTPClient](#ntpclient)
- [NTPServer](#ntpserver)
- [WebConsole](#webconsole)
- [ControlAPI](#controlapi)
- [RouterdCluster](#routerdcluster)
- [Interface](#interface)
- [PPPoESession](#pppoesession)
- [WireGuardInterface](#wireguardinterface)
- [WireGuardPeer](#wireguardpeer)
- [TailscaleNode](#tailscalenode)
- [IPsecConnection](#ipsecconnection)
- [VRF](#vrf)
- [VXLANTunnel](#vxlantunnel)
- [IPv4StaticAddress](#ipv4staticaddress)
- [VirtualAddress](#virtualaddress)
- [BGPRouter](#bgprouter)
- [BGPPeer](#bgppeer)
- [BGPDynamicPeer](#bgpdynamicpeer)
- [BFD](#bfd)
- [DHCPv4Client](#dhcpv4client)
- [ClusterNetworkRoute](#clusternetworkroute)
- [DHCPv4Server](#dhcpv4server)
- [DHCPv4Reservation](#dhcpv4reservation)
- [DHCPv6Address](#dhcpv6address)
- [IPv6RAAddress](#ipv6raaddress)
- [DHCPv6PrefixDelegation](#dhcpv6prefixdelegation)
- [IPv6DelegatedAddress](#ipv6delegatedaddress)
- [DHCPv6Information](#dhcpv6information)
- [IPv6RouterAdvertisement](#ipv6routeradvertisement)
- [DHCPv6Server](#dhcpv6server)
- [DHCPv4ServerLeaseSync](#dhcpv4serverleasesync)
- [DHCPv6ServerLeaseSync](#dhcpv6serverleasesync)
- [DHCPv6PrefixDelegationLeaseSync](#dhcpv6prefixdelegationleasesync)
- [DHCPv4Relay](#dhcpv4relay)
- [SelfAddressPolicy](#selfaddresspolicy)
- [DNSZone](#dnszone)
- [DNSResolver](#dnsresolver)
- [DNSForwarder](#dnsforwarder)
- [DNSUpstream](#dnsupstream)
- [DSLiteTunnel](#dslitetunnel)
- [TunnelInterface](#tunnelinterface)
- [OverlayPeer](#overlaypeer)
- [HybridRoute](#hybridroute)
- [CloudProviderProfile](#cloudproviderprofile)
- [ProviderActionPolicy](#provideractionpolicy)
- [EventGroup](#eventgroup)
- [EventPeer](#eventpeer)
- [EventSubscription](#eventsubscription)
- [FederationSLO](#federationslo)
- [MobilityPool](#mobilitypool)
- [SAMNodeSet](#samnodeset)
- [SAMEnrollmentPolicy](#samenrollmentpolicy)
- [SAMEnrollmentClaim](#samenrollmentclaim)
- [SAMEnrollmentClient](#samenrollmentclient)
- [SAMTransportProfile](#samtransportprofile)
- [SAMSubnetPolicy](#samsubnetpolicy)
- [IPv4Route](#ipv4route)
- [HealthCheck](#healthcheck)
- [EgressRoutePolicy](#egressroutepolicy)
- [EventRule](#eventrule)
- [DerivedEvent](#derivedevent)
- [NAT44Rule](#nat44rule)
- [NAT44FlowDNATPinhole](#nat44flowdnatpinhole)
- [NAT44SessionSync](#nat44sessionsync)
- [ManagementAccess](#managementaccess)
- [PortForward](#portforward)
- [IngressService](#ingressservice)
- [IPAddressSet](#ipaddressset)
- [LocalServiceRedirect](#localserviceredirect)
- [FirewallFlowPinhole](#firewallflowpinhole)
- [TrafficFlowLog](#trafficflowlog)
- [FirewallZone](#firewallzone)
- [FirewallPolicy](#firewallpolicy)
- [ClientPolicy](#clientpolicy)
- [FirewallRule](#firewallrule)
- [FirewallEventLog](#firewalleventlog)
- [Hostname](#hostname)

## Plugin

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: Plugin
  metadata:
    name: "<name>"
  spec:
    capabilities: # 任意; array
      - "observe.cloud" # string; enum=["observe.cloud", "observe.providerPrivateIPs", "propose.dynamicConfig", "propose.providerAction", "execute.providerAction"]
    context: # 任意; object
      resources: # 任意; array
        - # object
          apiVersion: "<string>" # 必須; string
          kind: "<string>" # 必須; string
          name: "<string>" # 必須; string
    env: # 任意; object
      "<key>": "<string>" # 任意; string
    executable: "<string>" # 必須; string
    timeout: "<string>" # 任意; string
    triggers: # 任意; array
      - # object
        every: "<string>" # 任意; string
        topic: "<string>" # 任意; string
        type: "interval" # 必須; string; enum=["interval", "event"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## DynamicConfigSource

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: DynamicConfigSource
  metadata:
    name: "<name>"
  spec:
    mergePolicy: # 任意; object
      conflict: "" # 任意; string; enum=["", "reject"]
    pluginRef: "<string>" # 必須; string
    triggers: # 任意; array
      - # object
        every: "<string>" # 任意; string
        topic: "<string>" # 任意; string
        type: "interval" # 必須; string; enum=["interval", "event"]
    ttl: "<string>" # 任意; string
```


## DynamicOverridePolicy

`apiVersion: config.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: config.routerd.net/v1alpha1
  kind: DynamicOverridePolicy
  metadata:
    name: "<name>"
  spec:
    allow: # 必須; array
      - # object
        operations: # 必須; array
          - "<string>" # string
        source: "<string>" # 必須; string
        targets: # 必須; array
          - # object
            apiVersion: "<string>" # 必須; string
            kind: "<string>" # 必須; string
            name: "<string>" # 必須; string
```


## LogSink

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogSink
  metadata:
    name: "<name>"
  spec:
    enabled: true # 任意; boolean
    file: # 任意; object
      name: "<string>" # 任意; string
    journald: # 任意; object
      identifier: "<string>" # 任意; string
    minLevel: "debug" # 任意; string; enum=["debug", "info", "warning", "error"]
    otlp: # 任意; object
      endpoint: "<string>" # 任意; string
      telemetryRef: "<string>" # 任意; string
    syslog: # 任意; object
      address: "<string>" # 任意; string
      facility: "kern" # 任意; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
      network: "" # 任意; string; enum=["", "unix", "unixgram", "tcp", "udp"]
      tag: "<string>" # 任意; string
    type: "syslog" # 必須; string; enum=["syslog", "otlp", "webhook", "file", "journald"]
    webhook: # 任意; object
      headers: # 任意; object
        "<key>": "<string>" # 任意; string
      timeout: "<string>" # 任意; string
      url: "<string>" # 任意; string
```


## Telemetry

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: "<name>"
  spec:
    attributes: # 任意; object
      "<key>": "<string>" # 任意; string
    otlp: # 必須; object
      endpoint: "<string>" # 必須; string
      insecure: true # 任意; boolean
    serviceNamespace: "<string>" # 任意; string
    signals: # 任意; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)


## ObservabilityPipeline

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ObservabilityPipeline
  metadata:
    name: "<name>"
  spec:
    attributes: # 任意; object
      "<key>": "<string>" # 任意; string
    logs: # 任意; object
      enabled: true # 任意; boolean
      sinks: # 任意; array
        - # object
          enabled: true # 任意; boolean
          kafka: # 任意; object
            brokers: # 任意; array
              - "<string>" # string
            topic: "<string>" # 任意; string
          labels: # 任意; object
            "<key>": "<string>" # 任意; string
          loki: # 任意; object
            headers: # 任意; object
              "<key>": "<string>" # 任意; string
            tenant: "<string>" # 任意; string
            url: "<string>" # 任意; string
          minLevel: "debug" # 任意; string; enum=["debug", "info", "warning", "error"]
          name: "<string>" # 任意; string
          syslog: # 任意; object
            address: "<string>" # 任意; string
            facility: "kern" # 任意; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
            network: "" # 任意; string; enum=["", "unix", "unixgram", "tcp", "udp"]
            tag: "<string>" # 任意; string
          type: "stdout" # 必須; string; enum=["stdout", "syslog", "loki", "kafka"]
    otlp: # 任意; object
      endpoint: "<string>" # 任意; string
      headers: # 任意; object
        "<key>": "<string>" # 任意; string
      insecure: true # 任意; boolean
      tls: # 任意; object
        caFile: "<string>" # 任意; string
        certFile: "<string>" # 任意; string
        insecureSkipVerify: true # 任意; boolean
        keyFile: "<string>" # 任意; string
    sampling: # 任意; object
      rate: 0 # 任意; number
    serviceNamespace: "<string>" # 任意; string
    signals: # 任意; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)


## LogRetention

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogRetention
  metadata:
    name: "<name>"
  spec:
    retention: "<string>" # 必須; string
    schedule: "" # 任意; string; enum=["", "daily"]
    signals: # 任意; array
      - "events" # string; enum=["events", "accessLogs", "pluginRuns", "dnsQueries", "trafficFlows", "firewallEvents", "dhcpFingerprints"]
    sinks: # 任意; array
      - "<string>" # string
    vacuum: true # 任意; boolean
```


## Sysctl

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Sysctl
  metadata:
    name: "<name>"
  spec:
    compare: "" # 任意; string; enum=["", "exact", "atLeast"]
    expectedValue: "<string>" # 任意; string
    key: "<string>" # 必須; string
    optional: true # 任意; boolean
    persistent: true # 任意; boolean
    runtime: true # 任意; boolean
    value: "<string>" # 必須; string
```


## SysctlProfile

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: SysctlProfile
  metadata:
    name: "<name>"
  spec:
    overrides: # 任意; object
      "<key>": "<string>" # 任意; string
    persistent: true # 任意; boolean
    profile: "router-linux" # 必須; string; enum=["router-linux", "router-freebsd"]
    runtime: true # 任意; boolean
```


## Package

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Package
  metadata:
    name: "<name>"
  spec:
    packages: # 必須; array
      - # object
        manager: "" # 任意; string; enum=["", "apt", "dnf", "pkg"]
        names: # 必須; array
          - "<string>" # string
        optional: true # 任意; boolean
        os: "ubuntu" # 必須; string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"]
    state: "" # 任意; string; enum=["", "present"]
```


## NTPClient

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata:
    name: "<name>"
  spec:
    fallbackServers: # 任意; array
      - "<string>" # string
    interface: "<string>" # 任意; string
    managed: true # 任意; boolean
    provider: "systemd-timesyncd" # 任意; string; enum=["systemd-timesyncd", "chrony", "ntpd"]
    serverFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    servers: # 任意; array
      - "<string>" # string
    source: "static" # 任意; string; enum=["static", "auto", "dhcp", "dhcpv6"]
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)


## NTPServer

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPServer
  metadata:
    name: "<name>"
  spec:
    allowCIDRFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    allowCIDRs: # 任意; array
      - "<string>" # string
    fallbackServers: # 任意; array
      - "<string>" # string
    listenAddressFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    listenAddresses: # 任意; array
      - "<string>" # string
    managed: true # 任意; boolean
    provider: "chrony" # 任意; string; enum=["chrony", "ntpd"]
    serverFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    servers: # 任意; array
      - "<string>" # string
    source: "static" # 任意; string; enum=["static", "auto", "dhcp", "dhcpv6"]
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


## WebConsole

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    basePath: "<string>" # 任意; string
    enabled: true # 任意; boolean
    links: # 任意; array
      - # object
        description: "<string>" # 任意; string
        label: "<string>" # 必須; string
        url: "<string>" # 必須; string
    listenAddress: "<string>" # 任意; string
    listenAddressFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    port: 1 # 任意; integer; minimum=1; maximum=65535
    title: "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## ControlAPI

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    allowCIDRs: # 任意; array
      - "<string>" # string
    enabled: true # 任意; boolean
    listenAddress: "<string>" # 任意; string
    port: 1 # 任意; integer; minimum=1; maximum=65535
    tls: # 任意; object
      certFile: "<string>" # 任意; string
      clientCAFile: "<string>" # 任意; string
      keyFile: "<string>" # 任意; string
    tokenFrom: # 任意; object
      base64: true # 任意; boolean
      env: "<string>" # 任意; string
      file: "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)


## RouterdCluster

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    identity: "<string>" # 任意; string
    leasePath: "<string>" # 任意; string
    leaseTTL: "<string>" # 任意; string
    peers: # 必須; array
      - "<string>" # string
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)


## Interface

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    adminUp: true # 任意; boolean
    ifname: "<string>" # 必須; string
    managed: true # 必須; boolean
    mtu: 576 # 任意; integer; minimum=576; maximum=9216
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)


## PPPoESession

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    acName: "<string>" # 任意; string
    authMethod: "chap" # 任意; string; enum=["chap", "pap", "both"]
    defaultRoute: true # 任意; boolean
    enabled: true # 任意; boolean
    ifname: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    ipv6: true # 任意; boolean
    lcpEchoFailure: 0 # 任意; integer; minimum=0
    lcpEchoInterval: 0 # 任意; integer; minimum=0
    lcpFailure: 0 # 任意; integer; minimum=0
    lcpInterval: 0 # 任意; integer; minimum=0
    managed: true # 任意; boolean
    mru: 576 # 任意; integer; minimum=576; maximum=1500
    mtu: 576 # 任意; integer; minimum=576; maximum=1500
    password: "<string>" # 任意; string
    passwordFile: "<string>" # 任意; string
    persist: true # 任意; boolean
    secretEncoding: "plain" # 任意; string; enum=["plain"]
    serviceName: "<string>" # 任意; string
    usePeerDNS: true # 任意; boolean
    username: "<string>" # 必須; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)


## WireGuardInterface

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardInterface
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # 任意; string
    listenPort: 1 # 任意; integer; minimum=1; maximum=65535
    mtu: 576 # 任意; integer; minimum=576; maximum=9216
    peersFrom: # 任意; array
      - # object
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    privateKey: "<string>" # 任意; string
    privateKeyFile: "<string>" # 任意; string
    selfNodeRef: "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## WireGuardPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs: # 必須; array
      - "<string>" # string
    endpoint: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    persistentKeepalive: 0 # 任意; integer; minimum=0; maximum=65535
    presharedKey: "<string>" # 任意; string
    presharedKeyFile: "<string>" # 任意; string
    publicKey: "<string>" # 必須; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)


## TailscaleNode

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TailscaleNode
  metadata:
    name: "<name>"
  spec:
    acceptDNS: true # 任意; boolean
    acceptRoutes: true # 任意; boolean
    advertiseExitNode: true # 任意; boolean
    advertiseRoutes: # 任意; array
      - "<string>" # string
    advertiseTags: # 任意; array
      - "<string>" # string
    authKey: "<string>" # 任意; string
    authKeyEnv: "<string>" # 任意; string
    authKeyFile: "<string>" # 任意; string
    hostname: "<string>" # 任意; string
    loginServer: "<string>" # 任意; string
    shieldsUp: true # 任意; boolean
    ssh: true # 任意; boolean
    state: "" # 任意; string; enum=["", "present", "absent"]
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/tailscale-exit-subnet.yaml)


## IPsecConnection

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    certificateRef: "<string>" # 任意; string
    cloudProviderHint: "" # 任意; string; enum=["", "aws", "azure", "gcp"]
    leftSubnet: "<string>" # 必須; string
    localAddress: "<string>" # 必須; string
    phase1Proposals: # 任意; array
      - "<string>" # string
    phase2Proposals: # 任意; array
      - "<string>" # string
    preSharedKey: "<string>" # 任意; string
    remoteAddress: "<string>" # 必須; string
    rightSubnet: "<string>" # 必須; string
```


## VRF

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VRF
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # 任意; string
    members: # 任意; array
      - "<string>" # string
    routeTable: 1 # 必須; integer; minimum=1; maximum=4294967295
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/multi-instance-bgp.yaml)


## VXLANTunnel

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: "<name>"
  spec:
    bridge: "<string>" # 任意; string
    ifname: "<string>" # 任意; string
    localAddress: "<string>" # 必須; string
    mtu: 0 # 任意; integer; minimum=0
    outerDF: "" # 任意; string; enum=["", "inherit", "set", "unset"]
    peers: # 任意; array
      - "<string>" # string
    tcpMSSClamp: true # 任意; boolean
    udpPort: 1 # 任意; integer; minimum=1; maximum=65535
    underlayInterface: "<string>" # 必須; string
    vni: 1 # 必須; integer; minimum=1; maximum=16777215
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


## IPv4StaticAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必須; string
    allowOverlap: true # 任意; boolean
    allowOverlapReason: "<string>" # 任意; string
    exclusive: true # 任意; boolean
    interface: "<string>" # 必須; string
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/basic-static.yaml)


## VirtualAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必須; string
    addressFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    externalDNS: true # 任意; boolean
    family: "ipv4" # 必須; string; enum=["ipv4", "ipv6"]
    gratuitousARP: true # 任意; boolean
    hostname: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    mode: "" # 任意; string; enum=["", "static", "vrrp"]
    track: # 任意; array
      - # object
        confirmConsecutiveHealthy: 1 # 任意; integer; minimum=1; maximum=255
        confirmConsecutiveUnhealthy: 1 # 任意; integer; minimum=1; maximum=255
        resource: "<string>" # 必須; string
        unhealthyPenalty: 0 # 任意; integer; minimum=0; maximum=254
    vrrp: # 任意; object
      additionalFailoverVMACs: # 任意; array
        - # object
          interface: "<string>" # 必須; string
          linkLocalAddress: "<string>" # 任意; string
          macAddress: "<string>" # 必須; string
          parentInterface: "<string>" # 必須; string
          withdrawRouterAdvertisement: true # 任意; boolean
      authentication: "<string>" # 任意; string
      authenticationFrom: # 任意; object
        base64: true # 任意; boolean
        env: "<string>" # 任意; string
        file: "<string>" # 任意; string
      failoverVMAC: # 任意; object
        interface: "<string>" # 必須; string
        linkLocalAddress: "<string>" # 任意; string
        macAddress: "<string>" # 必須; string
        parentInterface: "<string>" # 必須; string
        withdrawRouterAdvertisement: true # 任意; boolean
      gracefulActivation: # 任意; object
        readyWhen: # 必須; object
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
        timeout: "<string>" # 任意; string
      peers: # 任意; array
        - "<string>" # string
      preempt: true # 任意; boolean
      priority: 1 # 任意; integer; minimum=1; maximum=254
      virtualRouterID: 1 # 必須; integer; minimum=1; maximum=255
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


## BGPRouter

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: 1 # 必須; integer; minimum=1
    backend: "" # 任意; string; enum=["", "gobgp"]
    communities: # 任意; object
      accept: # 任意; array
        - "<string>" # string
      send: "" # 任意; string; enum=["", "standard", "extended", "both"]
      set: # 任意; object
        in: # 任意; array
          - "<string>" # string
        out: # 任意; array
          - "<string>" # string
    convergenceProfile: "" # 任意; string; enum=["", "default", "fast", "stable"]
    exportPolicy: # 任意; object
      allowedPrefixes: # 任意; array
        - "<string>" # string
    gracefulRestart: # 任意; object
      enabled: true # 任意; boolean
      restartTime: "<string>" # 任意; string
      stalePathTime: "<string>" # 任意; string
    importPolicy: # 任意; object
      allowedPrefixLengthMax: 0 # 任意; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 任意; integer; minimum=0; maximum=128
      allowedPrefixes: # 任意; array
        - "<string>" # string
      forbiddenCommunities: # 任意; array
        - "<string>" # string
      localPreference: 0 # 任意; integer
      nextHopRewrite: "" # 任意; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 任意; array
        - "<string>" # string
    listen: # 任意; object
      address: "<string>" # 任意; string
      port: 1 # 任意; integer; minimum=1; maximum=65535
    redistribute: # 任意; object
      connected: # 任意; object
        allowedPrefixes: # 任意; array
          - "<string>" # string
      static: # 任意; object
        allowedPrefixes: # 任意; array
          - "<string>" # string
    routerID: "<string>" # 必須; string
    timers: # 任意; object
      profile: "" # 任意; string; enum=["", "default", "fast", "slow"]
    vrf: "<string>" # 任意; string
    watcher: # 任意; object
      maxPrefixes: 1 # 任意; integer; minimum=1; maximum=999999
      peerStateChangeThrottle: "<string>" # 任意; string
      pollInterval: "<string>" # 任意; string
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## BGPPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPPeer
  metadata:
    name: "<name>"
  spec:
    bfd: "<string>" # 任意; string
    communities: # 任意; object
      accept: # 任意; array
        - "<string>" # string
      send: "" # 任意; string; enum=["", "standard", "extended", "both"]
      set: # 任意; object
        in: # 任意; array
          - "<string>" # string
        out: # 任意; array
          - "<string>" # string
    convergenceProfile: "" # 任意; string; enum=["", "default", "fast", "stable"]
    ebgpMultihop: 0 # 任意; integer; minimum=0; maximum=255
    exportPolicy: # 任意; object
      allowedPrefixes: # 任意; array
        - "<string>" # string
    importPolicy: # 任意; object
      allowedPrefixLengthMax: 0 # 任意; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 任意; integer; minimum=0; maximum=128
      allowedPrefixes: # 任意; array
        - "<string>" # string
      forbiddenCommunities: # 任意; array
        - "<string>" # string
      localPreference: 0 # 任意; integer
      nextHopRewrite: "" # 任意; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 任意; array
        - "<string>" # string
    passiveMode: true # 任意; boolean
    password: "<string>" # 任意; string
    passwordFrom: # 任意; object
      base64: true # 任意; boolean
      env: "<string>" # 任意; string
      file: "<string>" # 任意; string
    peerASN: 1 # 必須; integer; minimum=1
    peers: # 任意; array
      - "<string>" # string
    routeReflectorClient: true # 任意; boolean
    routeReflectorClusterID: "<string>" # 任意; string
    routerRef: "<string>" # 必須; string
    timers: # 任意; object
      profile: "" # 任意; string; enum=["", "default", "fast", "slow"]
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## BGPDynamicPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPDynamicPeer
  metadata:
    name: "<name>"
  spec:
    ebgpMultihop: 0 # 任意; integer; minimum=0; maximum=255
    exportPolicy: # 任意; object
      allowedPrefixes: # 任意; array
        - "<string>" # string
    importPolicy: # 任意; object
      allowedPrefixLengthMax: 0 # 任意; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 任意; integer; minimum=0; maximum=128
      allowedPrefixes: # 任意; array
        - "<string>" # string
      forbiddenCommunities: # 任意; array
        - "<string>" # string
      localPreference: 0 # 任意; integer
      nextHopRewrite: "" # 任意; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 任意; array
        - "<string>" # string
    listen: # 必須; object
      sourcePrefixes: # 必須; array
        - "<string>" # string
    password: "<string>" # 任意; string
    passwordFrom: # 任意; object
      base64: true # 任意; boolean
      env: "<string>" # 任意; string
      file: "<string>" # 任意; string
    peerASN: 1 # 必須; integer; minimum=1
    routeReflectorClient: true # 任意; boolean
    routeReflectorClusterID: "<string>" # 任意; string
    routerRef: "<string>" # 必須; string
    timers: # 任意; object
      profile: "" # 任意; string; enum=["", "default", "fast", "slow"]
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## BFD

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BFD
  metadata:
    name: "<name>"
  spec:
    detectMultiplier: 1 # 任意; integer; minimum=1; maximum=50
    interface: "<string>" # 任意; string
    minRx: "<string>" # 任意; string
    minTx: "<string>" # 任意; string
    peer: "<string>" # 必須; string
    profile: "" # 任意; string; enum=["", "fast", "normal", "slow"]
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## DHCPv4Client

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: "<name>"
  spec:
    classID: "<string>" # 任意; string
    clientID: "<string>" # 任意; string
    hostname: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    requestedAddress: "<string>" # 任意; string
    routeMetric: 0 # 任意; integer; minimum=0
    useDNS: true # 任意; boolean
    useRoutes: true # 任意; boolean
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)


## ClusterNetworkRoute

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ClusterNetworkRoute
  metadata:
    name: "<name>"
  spec:
    pods: # 任意; object
      cidrs: # 任意; array
        - "<string>" # string
    services: # 任意; object
      cidrs: # 任意; array
        - "<string>" # string
    via: # 必須; array
      - # object
        interface: "<string>" # 必須; string
        nextHop: "<string>" # 必須; string
        weight: 0 # 任意; integer; minimum=0; maximum=999
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/k8s-cluster-routes.yaml)


## DHCPv4Server

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

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


## DHCPv4Reservation

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # 任意; string
    ipAddress: "<string>" # 必須; string
    leaseTime: "<string>" # 任意; string
    macAddress: "<string>" # 必須; string
    options: # 任意; array
      - # object
        code: 1 # 任意; integer; minimum=1; maximum=65535
        name: "<string>" # 任意; string
        value: "<string>" # 任意; string
    scope: "<string>" # 任意; string
    server: "<string>" # 任意; string
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)


## DHCPv6Address

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Address
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    required: true # 任意; boolean
```


## IPv6RAAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RAAddress
  metadata:
    name: "<name>"
  spec:
    interface: "<string>" # 必須; string
    managed: true # 任意; boolean
    required: true # 任意; boolean
```


## DHCPv6PrefixDelegation

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # 任意; string
    clientDUID: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    prefixLength: 1 # 任意; integer; minimum=1; maximum=128
    profile: "default" # 任意; string; enum=["default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd"]
    required: true # 任意; boolean
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## IPv6DelegatedAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: "<name>"
  spec:
    addressSuffix: "<string>" # 必須; string
    announce: true # 任意; boolean
    dependsOn: # 任意; array
      - # object
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        notEmpty: true # 任意; boolean
        optional: true # 任意; boolean
        phase: "<string>" # 任意; string
        resource: "<string>" # 必須; string
    interface: "<string>" # 必須; string
    prefixDelegation: "<string>" # 必須; string
    prefixLength: 1 # 任意; integer; minimum=1; maximum=128
    sendRA: true # 任意; boolean
    subnetID: "<string>" # 任意; string
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DHCPv6Information

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 任意; array
      - # object
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        notEmpty: true # 任意; boolean
        optional: true # 任意; boolean
        phase: "<string>" # 任意; string
        resource: "<string>" # 必須; string
    interface: "<string>" # 必須; string
    request: # 任意; array
      - "<string>" # string
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


## IPv6RouterAdvertisement

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: "<name>"
  spec:
    configPath: "<string>" # 任意; string
    dependsOn: # 任意; array
      - # object
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        notEmpty: true # 任意; boolean
        optional: true # 任意; boolean
        phase: "<string>" # 任意; string
        resource: "<string>" # 必須; string
    dnssl: # 任意; array
      - "<string>" # string
    dnsslFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    interface: "<string>" # 必須; string
    mFlag: true # 任意; boolean
    mtu: 1280 # 任意; integer; minimum=1280; maximum=65535
    oFlag: true # 任意; boolean
    pidFile: "<string>" # 任意; string
    preferredLifetime: "<string>" # 任意; string
    prefix: "<string>" # 任意; string
    prefixFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    prfPreference: "" # 任意; string; enum=["", "low", "medium", "high"]
    rdnss: # 任意; array
      - "<string>" # string
    rdnssFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    validLifetime: "<string>" # 任意; string
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DHCPv6Server

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

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


## DHCPv4ServerLeaseSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # 任意; string
    interval: "<string>" # 任意; string
    source: # 任意; object
      resource: "<string>" # 必須; string
    targets: # 任意; array
      - # object
        host: "<string>" # 必須; string
        name: "<string>" # 任意; string
        options: # 任意; array
          - "<string>" # string
        sshOptions: # 任意; array
          - "<string>" # string
        user: "<string>" # 任意; string
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


## DHCPv6ServerLeaseSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # 任意; string
    interval: "<string>" # 任意; string
    source: # 任意; object
      resource: "<string>" # 必須; string
    targets: # 任意; array
      - # object
        host: "<string>" # 必須; string
        name: "<string>" # 任意; string
        options: # 任意; array
          - "<string>" # string
        sshOptions: # 任意; array
          - "<string>" # string
        user: "<string>" # 任意; string
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


## DHCPv6PrefixDelegationLeaseSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegationLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # 任意; string
    interval: "<string>" # 任意; string
    source: # 任意; object
      resource: "<string>" # 必須; string
    targets: # 任意; array
      - # object
        host: "<string>" # 必須; string
        name: "<string>" # 任意; string
        options: # 任意; array
          - "<string>" # string
        sshOptions: # 任意; array
          - "<string>" # string
        user: "<string>" # 任意; string
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


## DHCPv4Relay

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Relay
  metadata:
    name: "<name>"
  spec:
    interfaces: # 必須; array
      - "<string>" # string
    upstream: "<string>" # 必須; string
```


## SelfAddressPolicy

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: SelfAddressPolicy
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # 必須; string; enum=["ipv4", "ipv6"]
    candidates: # 必須; array
      - # object
        address: "<string>" # 任意; string
        addressSuffix: "<string>" # 任意; string
        delegatedAddress: "<string>" # 任意; string
        interface: "<string>" # 任意; string
        matchSuffix: "<string>" # 任意; string
        ordinal: 1 # 任意; integer; minimum=1
        source: "delegatedAddress" # 必須; string; enum=["delegatedAddress", "interfaceAddress", "static"]
```


## DNSZone

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: "<name>"
  spec:
    dhcpDerived: # 任意; object
      ddns: true # 任意; boolean
      hostnameSuffix: "<string>" # 任意; string
      leaseFile: "<string>" # 任意; string
      sources: # 任意; array
        - "<string>" # string
      ttl: 0 # 任意; integer; minimum=0
    dnssec: # 任意; object
      enabled: true # 任意; boolean
      mode: "" # 任意; string; enum=["", "validate", "sign"]
    records: # 任意; array
      - # object
        hostname: "<string>" # 必須; string
        ipv4: "<string>" # 任意; string
        ipv4From: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        ipv6: "<string>" # 任意; string
        ipv6From: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        ttl: 0 # 任意; integer; minimum=0
    reverseZones: # 任意; array
      - # object
        name: "<string>" # 必須; string
    ttl: 0 # 任意; integer; minimum=0
    zone: "<string>" # 必須; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSResolver

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    cache: # 任意; object
      enabled: true # 任意; boolean
      maxEntries: 0 # 任意; integer; minimum=0
      maxTTL: "<string>" # 任意; string
      minTTL: "<string>" # 任意; string
      negativeTTL: "<string>" # 任意; string
    listen: # 必須; array
      - # object
        addressFrom: # 任意; array
          - # object
            field: "<string>" # 任意; string
            optional: true # 任意; boolean
            resource: "<string>" # 必須; string
        addresses: # 任意; array
          - "<string>" # string
        name: "<string>" # 任意; string
        port: 1 # 任意; integer; minimum=1; maximum=65535
        sources: # 任意; array
          - "<string>" # string
    metrics: # 任意; object
      perUpstream: true # 任意; boolean
    queryLog: # 任意; object
      enabled: true # 任意; boolean
      path: "<string>" # 任意; string
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSForwarder

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    dnssecValidate: true # 任意; boolean
    healthcheck: # 任意; object
      failThreshold: 1 # 任意; integer; minimum=1
      interval: "<string>" # 任意; string
      passThreshold: 1 # 任意; integer; minimum=1
      timeout: "<string>" # 任意; string
    match: # 必須; array
      - "<string>" # string
    queryTimeout: "<string>" # 任意; string
    resolver: "<string>" # 必須; string
    upstreams: # 任意; array
      - "<string>" # string
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
    zoneRefs: # 任意; array
      - "<string>" # string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSUpstream

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 任意; string
    addressFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    bootstrap: # 任意; array
      - "<string>" # string
    bootstrapFrom: # 任意; array
      - # object
        field: "<string>" # 必須; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    path: "<string>" # 任意; string
    port: 1 # 任意; integer; minimum=1; maximum=65535
    protocol: "udp" # 必須; string; enum=["udp", "tcp", "dot", "doh"]
    sourceInterface: "<string>" # 任意; string
    tlsName: "<string>" # 任意; string
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DSLiteTunnel

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    aftrAddressOrdinal: 1 # 任意; integer; minimum=1
    aftrAddressSelection: "ordinal" # 任意; string; enum=["ordinal", "ordinalModulo"]
    aftrDNSServers: # 任意; array
      - "<string>" # string
    aftrFQDN: "<string>" # 任意; string
    aftrFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    aftrIPv6: "<string>" # 任意; string
    defaultRoute: true # 任意; boolean
    dependsOn: # 任意; array
      - # object
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        notEmpty: true # 任意; boolean
        optional: true # 任意; boolean
        phase: "<string>" # 任意; string
        resource: "<string>" # 必須; string
    enabled: true # 任意; boolean
    encapsulationLimit: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    localAddress: "<string>" # 任意; string
    localAddressFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    localAddressSource: "interface" # 任意; string; enum=["interface", "static", "delegatedAddress"]
    localAddressSuffix: "<string>" # 任意; string
    localDelegatedAddress: "<string>" # 任意; string
    mtu: 1280 # 任意; integer; minimum=1280; maximum=65535
    remoteAddress: "<string>" # 任意; string
    routeMetric: 0 # 任意; integer; minimum=0
    tunnelName: "<string>" # 任意; string
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## TunnelInterface

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 任意; string
    encapDport: 1 # 任意; integer; minimum=1; maximum=65535
    encapSport: 1 # 任意; integer; minimum=1; maximum=65535
    key: 0 # 任意; integer; minimum=0; maximum=4294967295
    local: "<string>" # 任意; string
    localFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    mode: "ipip" # 必須; string; enum=["ipip", "gre", "fou", "gue"]
    mtu: 576 # 任意; integer; minimum=576; maximum=9216
    pathMTU: # 任意; object
      forceFragmentIPv4: true # 任意; boolean
    peerAddress: "<string>" # 任意; string
    remote: "<string>" # 任意; string
    remoteFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    trustedUnderlay: true # 必須; boolean
    ttl: 1 # 任意; integer; minimum=1; maximum=255
    underlayInterface: "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## OverlayPeer

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: "<string>" # 必須; string
    pathMTU: # 任意; object
      forceFragmentIPv4: true # 任意; boolean
    remote: # 任意; object
      address: "<string>" # 任意; string
      nodeID: "<string>" # 任意; string
    role: "onprem" # 必須; string; enum=["onprem", "cloud"]
    underlay: # 必須; object
      address: "<string>" # 任意; string
      interface: "<string>" # 任意; string
      type: "wireguard" # 必須; string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## HybridRoute

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: HybridRoute
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # 必須; array
      - "<string>" # string
    healthCheckRef: "<string>" # 任意; string
    install: # 任意; object
      metric: 0 # 任意; integer; minimum=0
      table: "" # 任意; string; enum=["", "main"]
    peerRef: "<string>" # 必須; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## CloudProviderProfile

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: CloudProviderProfile
  metadata:
    name: "<name>"
  spec:
    auth: # 必須; object
      command: "<string>" # 任意; string
      mode: "external-command" # 必須; string; enum=["external-command"]
    capabilities: # 必須; array
      - "<string>" # string
    provider: "azure" # 必須; string; enum=["azure", "aws", "oci", "gcp"]
    resourceGroup: "<string>" # 任意; string
    subscriptionID: "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## ProviderActionPolicy

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    allowUndo: true # 任意; boolean
    allowedActions: # 任意; array
      - "<string>" # string
    allowedCIDRs: # 任意; array
      - "<string>" # string
    allowedProviderRefs: # 任意; array
      - "<string>" # string
    allowedProviders: # 任意; array
      - "<string>" # string
    dryRunOnly: true # 任意; boolean
    enabled: true # 任意; boolean
    executionWindow: "<string>" # 任意; string
    maxActionsPerRun: 0 # 任意; integer
    requireApproval: true # 任意; boolean
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventGroup

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    auth: # 任意; object
      mode: "" # 任意; string; enum=["", "hmac"]
      secretFile: "<string>" # 任意; string
      secretRef: "<string>" # 任意; string
    listen: # 任意; object
      address: "<string>" # 任意; string
      port: 0 # 任意; integer; minimum=0; maximum=65535
    nodeName: "<string>" # 必須; string
    peersFrom: # 任意; array
      - # object
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    replayWindow: "<string>" # 任意; string
    retention: # 任意; object
      maxAge: "<string>" # 任意; string
      maxEvents: 0 # 任意; integer; minimum=0
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventPeer

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    direction: "push" # 任意; string; enum=["push"]
    endpoint: "<string>" # 任意; string
    groupRef: "<string>" # 必須; string
    nodeName: "<string>" # 必須; string
    subjectPrefixes: # 任意; array
      - "<string>" # string
    types: # 任意; array
      - "<string>" # string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventSubscription

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventSubscription
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必須; string
    match: # 必須; object
      payload: # 任意; object
        "<key>": "<string>" # 任意; string
      sourceNodes: # 任意; array
        - "<string>" # string
      subjectPrefixes: # 任意; array
        - "<string>" # string
      types: # 必須; array
        - "<string>" # string
    trigger: # 必須; object
      batchWindow: "<string>" # 任意; string
      debounce: "<string>" # 任意; string
      pluginRef: "<string>" # 必須; string
```


## FederationSLO

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    delivery: # 任意; object
      expiresSoonSeconds: 0 # 任意; integer; minimum=0
      lagFailSeconds: 0 # 任意; integer; minimum=0
      lagWarnSeconds: 0 # 任意; integer; minimum=0
    groupRef: "<string>" # 必須; string
    subscription: # 任意; object
      maxFailedRuns: 0 # 任意; integer; minimum=0
      maxPendingRuns: 0 # 任意; integer; minimum=0
```


## MobilityPool

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必須; string
    members: # 任意; array
      - # object
        capture: # 任意; object
          activeWhen: # 任意; object
            type: "" # 任意; string; enum=["", "single-router", "vrrp-master"]
            virtualAddressRef: "<string>" # 任意; string
          captureStrategy: "" # 任意; string; enum=["", "route-table"]
          excludeAddresses: # 任意; array
            - "<string>" # string
          gratuitousARP: true # 任意; boolean
          interface: "<string>" # 任意; string
          nicRef: "<string>" # 任意; string
          providerRef: "<string>" # 任意; string
          sourceAddress: "<string>" # 任意; string
          sourceAddressFrom: # 任意; object
            field: "<string>" # 任意; string
            optional: true # 任意; boolean
            resource: "<string>" # 必須; string
          target: # 任意; object
            "<key>": "<string>" # 任意; string
          targetFrom: # 任意; object
            "<key>": "<string>" # 任意; string
          type: "provider-secondary-ip" # 任意; string; enum=["provider-secondary-ip", "proxy-arp"]
        nodeRef: "<string>" # 必須; string
        ownershipDiscovery: # 任意; object
          allowEmptyAfter: "<string>" # 任意; string
          leaseTTL: "<string>" # 任意; string
          mode: "" # 任意; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
          pluginRef: "<string>" # 任意; string
          providerRef: "<string>" # 任意; string
          scanInterval: "<string>" # 任意; string
          scope: # 任意; object
            excludeAddresses: # 任意; array
              - "<string>" # string
            includeAddresses: # 任意; array
              - "<string>" # string
          selector: # 任意; object
            tags: # 任意; object
              "<key>": "<string>" # 任意; string
          sources: # 任意; array
            - # object
              bridge: "<string>" # 任意; string
              interface: "<string>" # 任意; string
              leaseTTL: "<string>" # 任意; string
              network: "<string>" # 任意; string
              probeRetries: 0 # 任意; integer; minimum=0; maximum=20
              probeTimeout: "<string>" # 任意; string
              resource: "<string>" # 任意; string
              scanInterval: "<string>" # 任意; string
              sourceAddressFrom: # 任意; object
                field: "<string>" # 任意; string
                optional: true # 任意; boolean
                resource: "<string>" # 必須; string
              type: "dhcpv4-lease" # 必須; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
          stoppedInstancePolicy: "" # 任意; string; enum=["", "hold", "release"]
          subnetRef: "<string>" # 任意; string
          subnetRefFrom: "<string>" # 任意; string
        profileRef: "<string>" # 任意; string
        staticOwnedAddresses: # 任意; array
          - "<string>" # string
    membersFrom: # 必須; array; minItems=1
      - # object
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    prefix: "<string>" # 必須; string
    profiles: # 任意; object
      cloudCaptures: # 任意; object
        "<key>": # 任意; object
          capture: # 任意; object
            activeWhen: # 任意; object
              type: "" # 任意; string; enum=["", "single-router", "vrrp-master"]
              virtualAddressRef: "<string>" # 任意; string
            captureStrategy: "" # 任意; string; enum=["", "route-table"]
            excludeAddresses: # 任意; array
              - "<string>" # string
            gratuitousARP: true # 任意; boolean
            interface: "<string>" # 任意; string
            nicRef: "<string>" # 任意; string
            providerRef: "<string>" # 任意; string
            sourceAddress: "<string>" # 任意; string
            sourceAddressFrom: # 任意; object
              field: "<string>" # 任意; string
              optional: true # 任意; boolean
              resource: "<string>" # 必須; string
            target: # 任意; object
              "<key>": "<string>" # 任意; string
            targetFrom: # 任意; object
              "<key>": "<string>" # 任意; string
            type: "provider-secondary-ip" # 任意; string; enum=["provider-secondary-ip", "proxy-arp"]
          ownershipDiscovery: # 任意; object
            allowEmptyAfter: "<string>" # 任意; string
            leaseTTL: "<string>" # 任意; string
            mode: "" # 任意; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
            pluginRef: "<string>" # 任意; string
            providerRef: "<string>" # 任意; string
            scanInterval: "<string>" # 任意; string
            scope: # 任意; object
              excludeAddresses: # 任意; array
                - "<string>" # string
              includeAddresses: # 任意; array
                - "<string>" # string
            selector: # 任意; object
              tags: # 任意; object
                "<key>": "<string>" # 任意; string
            sources: # 任意; array
              - # object
                bridge: "<string>" # 任意; string
                interface: "<string>" # 任意; string
                leaseTTL: "<string>" # 任意; string
                network: "<string>" # 任意; string
                probeRetries: 0 # 任意; integer; minimum=0; maximum=20
                probeTimeout: "<string>" # 任意; string
                resource: "<string>" # 任意; string
                scanInterval: "<string>" # 任意; string
                sourceAddressFrom: # 任意; object
                  field: "<string>" # 任意; string
                  optional: true # 任意; boolean
                  resource: "<string>" # 必須; string
                type: "dhcpv4-lease" # 必須; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
            stoppedInstancePolicy: "" # 任意; string; enum=["", "hold", "release"]
            subnetRef: "<string>" # 任意; string
            subnetRefFrom: "<string>" # 任意; string
    staticHandovers: # 任意; array
      - # object
        address: "<string>" # 必須; string
        fromNodeRef: "<string>" # 必須; string
        toNodeRef: "<string>" # 必須; string
    values: # 任意; object
      "<key>": "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## SAMNodeSet

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes: # 必須; array
      - # object
        eventEndpoint: "<string>" # 任意; string
        macAddresses: # 任意; array
          - "<string>" # string; pattern="^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$"
        maintenance: # 任意; object
          drain: true # 任意; boolean
        maxSecondaryIPs: 0 # 任意; integer; minimum=0
        nodeRef: "<string>" # 必須; string
        placement: # 任意; object
          group: "<string>" # 任意; string
          priority: 0 # 任意; integer; minimum=0; maximum=1000000
        role: "" # 任意; string; enum=["", "onprem", "cloud"]
        routeReflector: true # 任意; boolean
        samEndpoint: "<string>" # 任意; string
        samEndpointFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        site: "<string>" # 任意; string
        wireGuard: # 任意; object
          allowedIPs: # 任意; array
            - "<string>" # string
          endpoint: "<string>" # 任意; string
          persistentKeepalive: 0 # 任意; integer; minimum=0; maximum=65535
          publicKey: "<string>" # 任意; string
          transportEndpoint: "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)


## SAMEnrollmentPolicy

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentPolicy
  metadata:
    name: "<name>"
  spec:
    allowedLeafIDs: # 任意; object
      pattern: "<string>" # 任意; string
    directMesh: # 任意; object
      peerGroupRef: "<string>" # 任意; string
    endpointPrefixes: # 任意; array
      - "<string>" # string
    joinAudience: "<string>" # 任意; string
    joinTokenFrom: # 任意; object
      base64: true # 任意; boolean
      env: "<string>" # 任意; string
      file: "<string>" # 任意; string
    mobilityPoolRefs: # 任意; array
      - "<string>" # string
    mobilityPrefixes: # 任意; array
      - "<string>" # string
    rrNodeSetRef: "<string>" # 任意; string
    rrSetRef: "<string>" # 任意; string
    transportProfileRef: "<string>" # 必須; string
    ttl: "<string>" # 任意; string
    tunnelAddressPrefixes: # 必須; array
      - "<string>" # string
    wireGuard: # 任意; object
      allowedExtraIPPrefixes: # 任意; array
        - "<string>" # string
      endpointPrefixes: # 任意; array
        - "<string>" # string
      interface: "<string>" # 必須; string
      persistentKeepalive: 0 # 任意; integer; minimum=0; maximum=65535
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)


## SAMEnrollmentClaim

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClaim
  metadata:
    name: "<name>"
  spec:
    bgp: # 任意; object
      asn: 1 # 任意; integer; minimum=1
      routerID: "<string>" # 任意; string
    directMesh: true # 任意; boolean
    endpoint: "<string>" # 任意; string
    expiresAt: "<string>" # 任意; string
    joinAudience: "<string>" # 任意; string
    joinHMAC: "<string>" # 任意; string
    joinNonce: "<string>" # 任意; string
    joinTimestamp: "<string>" # 任意; string
    leafID: "<string>" # 必須; string
    mobility: # 任意; object
      ownedAddresses: # 任意; array
        - "<string>" # string
    policyRef: "<string>" # 必須; string
    revoked: true # 任意; boolean
    rrSetRef: "<string>" # 任意; string
    tunnelAddress: "<string>" # 必須; string
    wireGuard: # 任意; object
      allowedIPs: # 任意; array
        - "<string>" # string
      endpoint: "<string>" # 任意; string
      persistentKeepalive: 0 # 任意; integer; minimum=0; maximum=65535
      publicKey: "<string>" # 必須; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)


## SAMEnrollmentClient

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClient
  metadata:
    name: "<name>"
  spec:
    bootstrapEndpoints: # 任意; array
      - "<string>" # string
    claimRef: "<string>" # 必須; string
    controlAPITLS: # 任意; object
      caFile: "<string>" # 任意; string
      certFile: "<string>" # 任意; string
      insecureSkipVerify: true # 任意; boolean
      keyFile: "<string>" # 任意; string
      serverName: "<string>" # 任意; string
    controlAPITokenFrom: # 任意; object
      base64: true # 任意; boolean
      env: "<string>" # 任意; string
      file: "<string>" # 任意; string
    retryBackoff: # 任意; object
      max: "<string>" # 任意; string
      min: "<string>" # 任意; string
    rrSocket: "<string>" # 任意; string
    stateTTLRefreshBefore: "<string>" # 任意; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)


## SAMTransportProfile

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMTransportProfile
  metadata:
    name: "<name>"
  spec:
    addressingMode: "" # 任意; string; enum=["", "edge-index", "pair-stable"]
    bgp: # 必須; object
      bfd: # 任意; object
        detectMultiplier: 1 # 任意; integer; minimum=1; maximum=50
        enabled: true # 任意; boolean
        interface: "<string>" # 任意; string
        minRx: "<string>" # 任意; string
        minTx: "<string>" # 任意; string
        profile: "" # 任意; string; enum=["", "fast", "normal", "slow"]
      convergenceProfile: "" # 任意; string; enum=["", "default", "fast", "stable"]
      directLocalPreference: 0 # 任意; integer
      ebgpMultihop: 0 # 任意; integer; minimum=0; maximum=255
      exportPolicy: # 任意; object
        allowedPrefixes: # 任意; array
          - "<string>" # string
      generatePeers: true # 任意; boolean
      importPolicy: # 任意; object
        allowedPrefixLengthMax: 0 # 任意; integer; minimum=0; maximum=128
        allowedPrefixLengthMin: 0 # 任意; integer; minimum=0; maximum=128
        allowedPrefixes: # 任意; array
          - "<string>" # string
        forbiddenCommunities: # 任意; array
          - "<string>" # string
        localPreference: 0 # 任意; integer
        nextHopRewrite: "" # 任意; string; enum=["", "peer-address", "unchanged"]
        requiredCommunities: # 任意; array
          - "<string>" # string
      peerASN: 1 # 必須; integer; minimum=1
      routeReflectorClient: true # 任意; boolean
      routeReflectorClusterID: "<string>" # 任意; string
      routerRef: "<string>" # 必須; string
      timers: # 任意; object
        profile: "" # 任意; string; enum=["", "default", "fast", "slow"]
      timersPreset: "" # 任意; string; enum=["", "default", "fast", "slow"]
    encapDport: 1 # 任意; integer; minimum=1; maximum=65535
    encapSport: 1 # 任意; integer; minimum=1; maximum=65535
    encryption: "" # 任意; string; enum=["", "none", "wireguard"]
    innerPrefix: "<string>" # 必須; string
    localEndpoint: "<string>" # 任意; string
    localEndpointFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    mode: "ipip" # 必須; string; enum=["ipip", "gre", "fou", "gue"]
    peersFrom: # 任意; array
      - # object
        direct: true # 任意; boolean
        nodeRefs: # 任意; array
          - "<string>" # string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    publishPeerGroup: true # 任意; boolean
    selfNodeRef: "<string>" # 必須; string
    underlayInterface: "<string>" # 必須; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## SAMSubnetPolicy

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMSubnetPolicy
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必須; string
    poolRef: "<string>" # 必須; string
    shards: # 必須; array
      - # object
        assignedNodes: # 必須; array
          - "<string>" # string
        prefix: "<string>" # 必須; string
    sourcePrefix: "<string>" # 必須; string
```


## IPv4Route

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4Route
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 任意; array
      - # object
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        notEmpty: true # 任意; boolean
        optional: true # 任意; boolean
        phase: "<string>" # 任意; string
        resource: "<string>" # 必須; string
    destination: "<string>" # 必須; string
    device: "<string>" # 任意; string
    deviceFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    gateway: "<string>" # 任意; string
    gatewayFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    metric: 0 # 任意; integer; minimum=0
    preferredSource: "<string>" # 任意; string
    type: "unicast" # 任意; string; enum=["unicast", "blackhole"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)


## HealthCheck

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # 任意; string; enum=["ipv4", "ipv6"]
    enabled: true # 任意; boolean
    healthyThreshold: 1 # 任意; integer; minimum=1
    interface: "<string>" # 任意; string
    interval: "<string>" # 任意; string
    port: 0 # 任意; integer; minimum=0; maximum=65535
    protocol: "" # 任意; string; enum=["", "icmp", "tcp", "dns", "http"]
    role: "link" # 任意; string; enum=["link", "next-hop", "internet", "service", "policy"]
    target: "<string>" # 任意; string
    targetSource: "auto" # 任意; string; enum=["auto", "static", "defaultGateway", "dsliteRemote"]
    timeout: "<string>" # 任意; string
    type: "ping" # 任意; string; enum=["ping"]
    unhealthyThreshold: 1 # 任意; integer; minimum=1
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## EgressRoutePolicy

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates: # 必須; array
      - # object
        dependsOn: # 任意; array
          - # object
            equals: "<string>" # 任意; string
            field: "<string>" # 任意; string
            notEmpty: true # 任意; boolean
            optional: true # 任意; boolean
            phase: "<string>" # 任意; string
            resource: "<string>" # 必須; string
        device: "<string>" # 任意; string
        deviceFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        enabled: true # 任意; boolean
        gateway: "<string>" # 任意; string
        gatewayFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        gatewaySource: "" # 任意; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
        healthCheck: "<string>" # 任意; string
        interface: "<string>" # 任意; string
        mark: 0 # 任意; integer; minimum=0; maximum=4294967295
        metric: 0 # 任意; integer; minimum=0
        name: "<string>" # 任意; string
        preferredSource: "" # 任意; string; enum=["", "interface"]
        priority: 0 # 任意; integer; minimum=0; maximum=32765
        routeMetric: 0 # 任意; integer; minimum=0
        routeTable: 0 # 任意; integer; minimum=0; maximum=4294967295
        source: "<string>" # 任意; string
        table: 0 # 任意; integer; minimum=0; maximum=4294967295
        targets: # 任意; array
          - # object
            gateway: "<string>" # 任意; string
            gatewayFrom: # 任意; object
              field: "<string>" # 任意; string
              optional: true # 任意; boolean
              resource: "<string>" # 必須; string
            gatewaySource: "" # 任意; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
            healthCheck: "<string>" # 任意; string
            interface: "<string>" # 任意; string
            mark: 0 # 任意; integer; minimum=0; maximum=4294967295
            metric: 0 # 任意; integer; minimum=0
            name: "<string>" # 任意; string
            outboundInterface: "<string>" # 任意; string
            priority: 0 # 任意; integer; minimum=0; maximum=32765
            routeMetric: 0 # 任意; integer; minimum=0
            routeTable: 0 # 任意; integer; minimum=0; maximum=4294967295
            table: 0 # 任意; integer; minimum=0; maximum=4294967295
        weight: 0 # 任意; integer; minimum=0
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
    destinationCIDRs: # 任意; array
      - "<string>" # string
    destinationSetRefs: # 任意; array
      - "<string>" # string
    excludeDestinationCIDRs: # 任意; array
      - "<string>" # string
    excludeDestinationSetRefs: # 任意; array
      - "<string>" # string
    family: "ipv4" # 任意; string; enum=["ipv4", "ipv6"]
    hashFields: # 任意; array
      - "<string>" # string
    hostTraffic: true # 任意; boolean
    hysteresis: "<string>" # 任意; string
    mode: "priority" # 任意; string; enum=["priority", "mark", "hash"]
    selection: "highest-weight-ready" # 任意; string; enum=["highest-weight-ready", "weighted-ecmp"]
    sourceCIDRs: # 任意; array
      - "<string>" # string
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## EventRule

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EventRule
  metadata:
    name: "<name>"
  spec:
    emit: # 必須; object
      attributes: # 任意; object
        "<key>": "<string>" # 任意; string
      topic: "<string>" # 必須; string
    pattern: # 必須; object
      allow_missing_correlation: true # 任意; boolean
      correlate_by: "<string>" # 任意; string
      duration: "<string>" # 任意; string
      expected: "<string>" # 任意; string
      interval: "<string>" # 任意; string
      operator: "all_of" # 必須; string; enum=["all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"]
      quiet: "<string>" # 任意; string
      rate: 0 # 任意; integer; minimum=0
      strict: true # 任意; boolean
      threshold: 0 # 任意; integer; minimum=0
      topic: "<string>" # 任意; string
      topics: # 任意; array
        - "<string>" # string
      trigger: "<string>" # 任意; string
      window: "<string>" # 任意; string
```


## DerivedEvent

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DerivedEvent
  metadata:
    name: "<name>"
  spec:
    emitInitial: true # 任意; boolean
    emitWhen: "all_true" # 任意; string; enum=["all_true", "any_true"]
    hysteresis: "<string>" # 任意; string
    inputs: # 必須; array
      - # object
        any_of: # 任意; array
          - # array
            - # object
              equals: "<string>" # 任意; string
              field: "<string>" # 任意; string
              not_empty: true # 任意; boolean
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        not_empty: true # 任意; boolean
    retractWhen: "any_false" # 任意; string; enum=["any_false", "all_false"]
    topic: "<string>" # 必須; string
```


## NAT44Rule

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # 任意; array
      - "<string>" # string
    destinationSetRefs: # 任意; array
      - "<string>" # string
    egressInterface: "<string>" # 任意; string
    egressPolicyRef: "<string>" # 任意; string
    excludeDestinationCIDRs: # 任意; array
      - "<string>" # string
    excludeDestinationSetRefs: # 任意; array
      - "<string>" # string
    outboundInterface: "<string>" # 任意; string
    snatAddress: "<string>" # 任意; string
    snatAddressFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    sourceCIDRs: # 任意; array
      - "<string>" # string
    sourceRanges: # 任意; array
      - "<string>" # string
    translation: # 任意; object
      address: "<string>" # 任意; string
      addresses: # 任意; array
        - "<string>" # string
      portMapping: # 任意; object
        end: 1 # 任意; integer; minimum=1; maximum=65535
        start: 1 # 任意; integer; minimum=1; maximum=65535
        type: "auto" # 任意; string; enum=["auto", "preserve", "range"]
      type: "interfaceAddress" # 必須; string; enum=["interfaceAddress", "address", "pool"]
    type: "masquerade" # 任意; string; enum=["masquerade", "snat"]
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)


## NAT44FlowDNATPinhole

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44FlowDNATPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # 任意; object
      key: "" # 任意; string; enum=["", "localPort"]
    fromInterfaceRefs: # 任意; array
      - "<string>" # string
    inbound: # 必須; object
      sourcePorts: # 必須; array
        - "<string>" # string
    outbound: # 必須; object
      destinationCIDRs: # 任意; array
        - "<string>" # string
      destinationPorts: # 必須; array
        - "<string>" # string
      destinationSetRefs: # 任意; array
        - "<string>" # string
      protocol: "udp" # 必須; string; enum=["udp"]
      sourceCIDRs: # 任意; array
        - "<string>" # string
      sourceSetRef: "<string>" # 任意; string
    timeout: "<string>" # 任意; string
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


## NAT44SessionSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: "<name>"
  spec:
    conntrackCommand: "<string>" # 任意; string
    conntrackd: # 任意; object
      ignoreIPv4: # 任意; array
        - "<string>" # string
      interface: "<string>" # 必須; string
      localAddress: "<string>" # 必須; string
      peerAddress: "<string>" # 必須; string
      port: 1 # 任意; integer; minimum=1; maximum=65535
    excludeNatRules: # 任意; array
      - "<string>" # string
    interval: "<string>" # 任意; string
    mode: "event-stream" # 任意; string; enum=["event-stream", "conntrackd"]
    natRules: # 任意; array
      - "<string>" # string
    snatAddresses: # 任意; array
      - "<string>" # string
    targets: # 任意; array
      - # object
        host: "<string>" # 必須; string
        name: "<string>" # 任意; string
        restoreCommand: # 任意; array
          - "<string>" # string
        sshOptions: # 任意; array
          - "<string>" # string
        user: "<string>" # 任意; string
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


## ManagementAccess

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ManagementAccess
  metadata:
    name: "<name>"
  spec:
    allowSourceCIDRs: # 任意; array
      - "<string>" # string
    interfaces: # 必須; array
      - "<string>" # string
    requireWebConsoleBound: true # 任意; boolean
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## PortForward

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    hairpin: # 任意; object
      enabled: true # 任意; boolean
      interfaces: # 任意; array
        - "<string>" # string
      mode: "" # 任意; string; enum=["", "auto", "manual", "off"]
    listen: # 必須; object
      address: "<string>" # 任意; string
      addressFrom: # 任意; object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
      interface: "<string>" # 必須; string
      port: 1 # 必須; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必須; string; enum=["tcp", "udp"]
    target: # 必須; object
      address: "<string>" # 任意; string
      addressFrom: # 任意; object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
      port: 1 # 必須; integer; minimum=1; maximum=65535
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)


## IngressService

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends: # 必須; array
      - # object
        address: "<string>" # 任意; string
        addressFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        name: "<string>" # 任意; string
        port: 1 # 必須; integer; minimum=1; maximum=65535
        weight: 0 # 任意; integer; minimum=0
    externalDNS: true # 任意; boolean
    hairpin: # 任意; object
      enabled: true # 任意; boolean
      interfaces: # 任意; array
        - "<string>" # string
      mode: "" # 任意; string; enum=["", "auto", "manual", "off"]
    healthCheck: # 任意; object
      expectedBody: "<string>" # 任意; string
      expectedStatus: # 任意; array
        - 0 # integer
      healthyThreshold: 1 # 任意; integer; minimum=1
      host: "<string>" # 任意; string
      interval: "<string>" # 任意; string
      path: "<string>" # 任意; string
      protocol: "" # 任意; string; enum=["", "tcp", "http", "https"]
      timeout: "<string>" # 任意; string
      tlsSkipVerify: true # 任意; boolean
      unhealthyThreshold: 1 # 任意; integer; minimum=1
    hostname: "<string>" # 任意; string
    listen: # 必須; object
      address: "<string>" # 任意; string
      addressFrom: # 任意; object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
      interface: "<string>" # 必須; string
      port: 1 # 必須; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必須; string; enum=["tcp", "udp"]
    policy: # 任意; object
      onNoHealthyBackends: "" # 任意; string; enum=["", "drop", "reject"]
      selection: "" # 任意; string; enum=["", "failover", "sourceHash", "random"]
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)


## IPAddressSet

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPAddressSet
  metadata:
    name: "<name>"
  spec:
    addresses: # 任意; array
      - "<string>" # string
    names: # 任意; array
      - "<string>" # string
    refreshInterval: "<string>" # 任意; string
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)


## LocalServiceRedirect

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: LocalServiceRedirect
  metadata:
    name: "<name>"
  spec:
    interface: "<string>" # 必須; string
    rules: # 必須; array
      - # object
        destinationPort: 1 # 必須; integer; minimum=1; maximum=65535
        destinationSetRef: "<string>" # 必須; string
        name: "<string>" # 任意; string
        protocols: # 必須; array
          - "<string>" # string
        redirectPort: 1 # 必須; integer; minimum=1; maximum=65535
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

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)


## FirewallFlowPinhole

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallFlowPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # 任意; object
      key: "" # 任意; string; enum=["", "remoteAddress", "localEndpoint"]
    fromZone: "<string>" # 必須; string
    inbound: # 必須; object
      sourcePorts: # 必須; array
        - "<string>" # string
    outbound: # 必須; object
      destinationCIDRs: # 任意; array
        - "<string>" # string
      destinationPorts: # 必須; array
        - "<string>" # string
      destinationSetRefs: # 任意; array
        - "<string>" # string
      protocol: "udp" # 必須; string; enum=["udp"]
      sourceCIDRs: # 任意; array
        - "<string>" # string
      sourceSetRef: "<string>" # 任意; string
    timeout: "<string>" # 任意; string
    toZone: "<string>" # 必須; string
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


## TrafficFlowLog

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TrafficFlowLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # 任意; boolean
    includeApplicationLayer: true # 任意; boolean
    includeTLSSNI: true # 任意; boolean
    path: "<string>" # 任意; string
    source: "" # 任意; string; enum=["", "conntrack"]
```


## FirewallZone

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: "<name>"
  spec:
    interfaces: # 必須; array
      - "<string>" # string
    role: "untrust" # 必須; string; enum=["untrust", "trust", "mgmt"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)


## FirewallPolicy

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: "<name>"
  spec:
    logDeny: true # 任意; boolean
    sameRoleAccept: true # 任意; boolean
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)


## ClientPolicy

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    classification: # 任意; array
      - # object
        ipv4Reservation: "<string>" # 任意; string
        ipv6Addresses: # 任意; array
          - "<string>" # string
        match: # 必須; object
          dhcpFingerprints: # 任意; array
            - "<string>" # string
          hostnamePatterns: # 任意; array
            - "<string>" # string
          macs: # 任意; array
            - "<string>" # string
          ouiPrefixes: # 任意; array
            - "<string>" # string
        mode: "trusted" # 必須; string; enum=["trusted", "guest", "isolated"]
        name: "<string>" # 任意; string
    guestEgressAllow: # 任意; array
      - "<string>" # string
    guestEgressDeny: # 任意; array
      - "<string>" # string
    guestServices: # 任意; array
      - "<string>" # string
    interfaces: # 任意; array
      - "<string>" # string
    isolation: # 任意; object
      lanInternet: "" # 任意; string; enum=["", "allow", "deny"]
      lanLAN: "" # 任意; string; enum=["", "allow", "deny"]
      lanMgmt: "" # 任意; string; enum=["", "allow", "deny"]
      mDNSBroadcast: "" # 任意; string; enum=["", "allow", "deny"]
    macs: # 任意; array
      - "<string>" # string
    mode: "include" # 必須; string; enum=["include", "exclude"]
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)


## FirewallRule

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: "<name>"
  spec:
    action: "accept" # 必須; string; enum=["accept", "drop", "reject"]
    connLimit: # 任意; object
      log: true # 任意; boolean
      maxPerSource: 0 # 任意; integer; minimum=0
    destinationCIDRs: # 任意; array
      - "<string>" # string
    destinationPorts: # 任意; array
      - "<string>" # string
    destinationSetRefs: # 任意; array
      - "<string>" # string
    excludeDestinationSetRefs: # 任意; array
      - "<string>" # string
    fromZone: "<string>" # 必須; string
    icmpType: "<string>" # 任意; string
    icmpv6Type: "<string>" # 任意; string
    log: true # 任意; boolean
    port: 0 # 任意; integer; minimum=0; maximum=65535
    protocol: "" # 任意; string; enum=["", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip"]
    rateLimit: # 任意; object
      burst: 0 # 任意; integer; minimum=0
      log: true # 任意; boolean
      packetsPerSecond: 0 # 任意; integer; minimum=0
      per: "" # 任意; string; enum=["", "second", "minute"]
      rate: 0 # 任意; integer; minimum=0
      unit: "" # 任意; string; enum=["", "packet", "byte", "kilobyte", "megabyte"]
    sourcePorts: # 任意; array
      - "<string>" # string
    srcCIDRs: # 任意; array
      - "<string>" # string
    toZone: "<string>" # 必須; string
```

[設定例](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)


## FirewallEventLog

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # 任意; boolean
    events: # 任意; array
      - "deny" # string; enum=["deny", "allow", "rateLimit", "connLimit"]
    fromZones: # 任意; array
      - "<string>" # string
    log: # 任意; object
      acceptSampleRate: 0 # 任意; integer; minimum=0
      copyRange: 0 # 任意; integer; minimum=0
      dropSampleRate: 0 # 任意; integer; minimum=0
      reject: true # 任意; boolean
    nflogGroup: 0 # 任意; integer; minimum=0; maximum=65535
    path: "<string>" # 任意; string
    retention: "<string>" # 任意; string
    rules: # 任意; array
      - "<string>" # string
    sampleRate: 0 # 任意; integer; minimum=0
    sinks: # 任意; array
      - "<string>" # string
    toZones: # 任意; array
      - "<string>" # string
```


## Hostname

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Hostname
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # 必須; string
    managed: true # 任意; boolean
```
