# 各資源的設定

資源寫在Router.spec.resources內。範例的名稱、介面、位址、憑證及對端設定必須依環境調整。必填表示在所屬物件內必須指定。跨資源參照與語意限制還須以routerd validate確認。

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；只有default註記表示預設值。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

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
    capabilities: # 選填; array
      - "observe.cloud" # string; enum=["observe.cloud", "observe.providerPrivateIPs", "propose.dynamicConfig", "propose.providerAction", "execute.providerAction"]
    context: # 選填; object
      resources: # 選填; array
        - # object
          apiVersion: "<string>" # 必填; string
          kind: "<string>" # 必填; string
          name: "<string>" # 必填; string
    env: # 選填; object
      "<key>": "<string>" # 選填; string
    executable: "<string>" # 必填; string
    timeout: "<string>" # 選填; string
    triggers: # 選填; array
      - # object
        every: "<string>" # 選填; string
        topic: "<string>" # 選填; string
        type: "interval" # 必填; string; enum=["interval", "event"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## DynamicConfigSource

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: DynamicConfigSource
  metadata:
    name: "<name>"
  spec:
    mergePolicy: # 選填; object
      conflict: "" # 選填; string; enum=["", "reject"]
    pluginRef: "<string>" # 必填; string
    triggers: # 選填; array
      - # object
        every: "<string>" # 選填; string
        topic: "<string>" # 選填; string
        type: "interval" # 必填; string; enum=["interval", "event"]
    ttl: "<string>" # 選填; string
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
    allow: # 必填; array
      - # object
        operations: # 必填; array
          - "<string>" # string
        source: "<string>" # 必填; string
        targets: # 必填; array
          - # object
            apiVersion: "<string>" # 必填; string
            kind: "<string>" # 必填; string
            name: "<string>" # 必填; string
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
    enabled: true # 選填; boolean
    file: # 選填; object
      name: "<string>" # 選填; string
    journald: # 選填; object
      identifier: "<string>" # 選填; string
    minLevel: "debug" # 選填; string; enum=["debug", "info", "warning", "error"]
    otlp: # 選填; object
      endpoint: "<string>" # 選填; string
      telemetryRef: "<string>" # 選填; string
    syslog: # 選填; object
      address: "<string>" # 選填; string
      facility: "kern" # 選填; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
      network: "" # 選填; string; enum=["", "unix", "unixgram", "tcp", "udp"]
      tag: "<string>" # 選填; string
    type: "syslog" # 必填; string; enum=["syslog", "otlp", "webhook", "file", "journald"]
    webhook: # 選填; object
      headers: # 選填; object
        "<key>": "<string>" # 選填; string
      timeout: "<string>" # 選填; string
      url: "<string>" # 選填; string
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
    attributes: # 選填; object
      "<key>": "<string>" # 選填; string
    otlp: # 必填; object
      endpoint: "<string>" # 必填; string
      insecure: true # 選填; boolean
    serviceNamespace: "<string>" # 選填; string
    signals: # 選填; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)


## ObservabilityPipeline

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ObservabilityPipeline
  metadata:
    name: "<name>"
  spec:
    attributes: # 選填; object
      "<key>": "<string>" # 選填; string
    logs: # 選填; object
      enabled: true # 選填; boolean
      sinks: # 選填; array
        - # object
          enabled: true # 選填; boolean
          kafka: # 選填; object
            brokers: # 選填; array
              - "<string>" # string
            topic: "<string>" # 選填; string
          labels: # 選填; object
            "<key>": "<string>" # 選填; string
          loki: # 選填; object
            headers: # 選填; object
              "<key>": "<string>" # 選填; string
            tenant: "<string>" # 選填; string
            url: "<string>" # 選填; string
          minLevel: "debug" # 選填; string; enum=["debug", "info", "warning", "error"]
          name: "<string>" # 選填; string
          syslog: # 選填; object
            address: "<string>" # 選填; string
            facility: "kern" # 選填; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
            network: "" # 選填; string; enum=["", "unix", "unixgram", "tcp", "udp"]
            tag: "<string>" # 選填; string
          type: "stdout" # 必填; string; enum=["stdout", "syslog", "loki", "kafka"]
    otlp: # 選填; object
      endpoint: "<string>" # 選填; string
      headers: # 選填; object
        "<key>": "<string>" # 選填; string
      insecure: true # 選填; boolean
      tls: # 選填; object
        caFile: "<string>" # 選填; string
        certFile: "<string>" # 選填; string
        insecureSkipVerify: true # 選填; boolean
        keyFile: "<string>" # 選填; string
    sampling: # 選填; object
      rate: 0 # 選填; number
    serviceNamespace: "<string>" # 選填; string
    signals: # 選填; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)


## LogRetention

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogRetention
  metadata:
    name: "<name>"
  spec:
    retention: "<string>" # 必填; string
    schedule: "" # 選填; string; enum=["", "daily"]
    signals: # 選填; array
      - "events" # string; enum=["events", "accessLogs", "pluginRuns", "dnsQueries", "trafficFlows", "firewallEvents", "dhcpFingerprints"]
    sinks: # 選填; array
      - "<string>" # string
    vacuum: true # 選填; boolean
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
    compare: "" # 選填; string; enum=["", "exact", "atLeast"]
    expectedValue: "<string>" # 選填; string
    key: "<string>" # 必填; string
    optional: true # 選填; boolean
    persistent: true # 選填; boolean
    runtime: true # 選填; boolean
    value: "<string>" # 必填; string
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
    overrides: # 選填; object
      "<key>": "<string>" # 選填; string
    persistent: true # 選填; boolean
    profile: "router-linux" # 必填; string; enum=["router-linux", "router-freebsd"]
    runtime: true # 選填; boolean
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
    packages: # 必填; array
      - # object
        manager: "" # 選填; string; enum=["", "apt", "dnf", "pkg"]
        names: # 必填; array
          - "<string>" # string
        optional: true # 選填; boolean
        os: "ubuntu" # 必填; string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"]
    state: "" # 選填; string; enum=["", "present"]
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
    fallbackServers: # 選填; array
      - "<string>" # string
    interface: "<string>" # 選填; string
    managed: true # 選填; boolean
    provider: "systemd-timesyncd" # 選填; string; enum=["systemd-timesyncd", "chrony", "ntpd"]
    serverFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    servers: # 選填; array
      - "<string>" # string
    source: "static" # 選填; string; enum=["static", "auto", "dhcp", "dhcpv6"]
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)


## NTPServer

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPServer
  metadata:
    name: "<name>"
  spec:
    allowCIDRFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    allowCIDRs: # 選填; array
      - "<string>" # string
    fallbackServers: # 選填; array
      - "<string>" # string
    listenAddressFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    listenAddresses: # 選填; array
      - "<string>" # string
    managed: true # 選填; boolean
    provider: "chrony" # 選填; string; enum=["chrony", "ntpd"]
    serverFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    servers: # 選填; array
      - "<string>" # string
    source: "static" # 選填; string; enum=["static", "auto", "dhcp", "dhcpv6"]
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
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
    basePath: "<string>" # 選填; string
    enabled: true # 選填; boolean
    links: # 選填; array
      - # object
        description: "<string>" # 選填; string
        label: "<string>" # 必填; string
        url: "<string>" # 必填; string
    listenAddress: "<string>" # 選填; string
    listenAddressFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    port: 1 # 選填; integer; minimum=1; maximum=65535
    title: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## ControlAPI

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    allowCIDRs: # 選填; array
      - "<string>" # string
    enabled: true # 選填; boolean
    listenAddress: "<string>" # 選填; string
    port: 1 # 選填; integer; minimum=1; maximum=65535
    tls: # 選填; object
      certFile: "<string>" # 選填; string
      clientCAFile: "<string>" # 選填; string
      keyFile: "<string>" # 選填; string
    tokenFrom: # 選填; object
      base64: true # 選填; boolean
      env: "<string>" # 選填; string
      file: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)


## RouterdCluster

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    identity: "<string>" # 選填; string
    leasePath: "<string>" # 選填; string
    leaseTTL: "<string>" # 選填; string
    peers: # 必填; array
      - "<string>" # string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)


## Interface

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    adminUp: true # 選填; boolean
    ifname: "<string>" # 必填; string
    managed: true # 必填; boolean
    mtu: 576 # 選填; integer; minimum=576; maximum=9216
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)


## PPPoESession

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    acName: "<string>" # 選填; string
    authMethod: "chap" # 選填; string; enum=["chap", "pap", "both"]
    defaultRoute: true # 選填; boolean
    enabled: true # 選填; boolean
    ifname: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    ipv6: true # 選填; boolean
    lcpEchoFailure: 0 # 選填; integer; minimum=0
    lcpEchoInterval: 0 # 選填; integer; minimum=0
    lcpFailure: 0 # 選填; integer; minimum=0
    lcpInterval: 0 # 選填; integer; minimum=0
    managed: true # 選填; boolean
    mru: 576 # 選填; integer; minimum=576; maximum=1500
    mtu: 576 # 選填; integer; minimum=576; maximum=1500
    password: "<string>" # 選填; string
    passwordFile: "<string>" # 選填; string
    persist: true # 選填; boolean
    secretEncoding: "plain" # 選填; string; enum=["plain"]
    serviceName: "<string>" # 選填; string
    usePeerDNS: true # 選填; boolean
    username: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)


## WireGuardInterface

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardInterface
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # 選填; string
    listenPort: 1 # 選填; integer; minimum=1; maximum=65535
    mtu: 576 # 選填; integer; minimum=576; maximum=9216
    peersFrom: # 選填; array
      - # object
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    privateKey: "<string>" # 選填; string
    privateKeyFile: "<string>" # 選填; string
    selfNodeRef: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## WireGuardPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs: # 必填; array
      - "<string>" # string
    endpoint: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    persistentKeepalive: 0 # 選填; integer; minimum=0; maximum=65535
    presharedKey: "<string>" # 選填; string
    presharedKeyFile: "<string>" # 選填; string
    publicKey: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)


## TailscaleNode

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TailscaleNode
  metadata:
    name: "<name>"
  spec:
    acceptDNS: true # 選填; boolean
    acceptRoutes: true # 選填; boolean
    advertiseExitNode: true # 選填; boolean
    advertiseRoutes: # 選填; array
      - "<string>" # string
    advertiseTags: # 選填; array
      - "<string>" # string
    authKey: "<string>" # 選填; string
    authKeyEnv: "<string>" # 選填; string
    authKeyFile: "<string>" # 選填; string
    hostname: "<string>" # 選填; string
    loginServer: "<string>" # 選填; string
    shieldsUp: true # 選填; boolean
    ssh: true # 選填; boolean
    state: "" # 選填; string; enum=["", "present", "absent"]
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/tailscale-exit-subnet.yaml)


## IPsecConnection

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    certificateRef: "<string>" # 選填; string
    cloudProviderHint: "" # 選填; string; enum=["", "aws", "azure", "gcp"]
    leftSubnet: "<string>" # 必填; string
    localAddress: "<string>" # 必填; string
    phase1Proposals: # 選填; array
      - "<string>" # string
    phase2Proposals: # 選填; array
      - "<string>" # string
    preSharedKey: "<string>" # 選填; string
    remoteAddress: "<string>" # 必填; string
    rightSubnet: "<string>" # 必填; string
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
    ifname: "<string>" # 選填; string
    members: # 選填; array
      - "<string>" # string
    routeTable: 1 # 必填; integer; minimum=1; maximum=4294967295
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/multi-instance-bgp.yaml)


## VXLANTunnel

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: "<name>"
  spec:
    bridge: "<string>" # 選填; string
    ifname: "<string>" # 選填; string
    localAddress: "<string>" # 必填; string
    mtu: 0 # 選填; integer; minimum=0
    outerDF: "" # 選填; string; enum=["", "inherit", "set", "unset"]
    peers: # 選填; array
      - "<string>" # string
    tcpMSSClamp: true # 選填; boolean
    udpPort: 1 # 選填; integer; minimum=1; maximum=65535
    underlayInterface: "<string>" # 必填; string
    vni: 1 # 必填; integer; minimum=1; maximum=16777215
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
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
    address: "<string>" # 必填; string
    allowOverlap: true # 選填; boolean
    allowOverlapReason: "<string>" # 選填; string
    exclusive: true # 選填; boolean
    interface: "<string>" # 必填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/basic-static.yaml)


## VirtualAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必填; string
    addressFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    externalDNS: true # 選填; boolean
    family: "ipv4" # 必填; string; enum=["ipv4", "ipv6"]
    gratuitousARP: true # 選填; boolean
    hostname: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    mode: "" # 選填; string; enum=["", "static", "vrrp"]
    track: # 選填; array
      - # object
        confirmConsecutiveHealthy: 1 # 選填; integer; minimum=1; maximum=255
        confirmConsecutiveUnhealthy: 1 # 選填; integer; minimum=1; maximum=255
        resource: "<string>" # 必填; string
        unhealthyPenalty: 0 # 選填; integer; minimum=0; maximum=254
    vrrp: # 選填; object
      additionalFailoverVMACs: # 選填; array
        - # object
          interface: "<string>" # 必填; string
          linkLocalAddress: "<string>" # 選填; string
          macAddress: "<string>" # 必填; string
          parentInterface: "<string>" # 必填; string
          withdrawRouterAdvertisement: true # 選填; boolean
      authentication: "<string>" # 選填; string
      authenticationFrom: # 選填; object
        base64: true # 選填; boolean
        env: "<string>" # 選填; string
        file: "<string>" # 選填; string
      failoverVMAC: # 選填; object
        interface: "<string>" # 必填; string
        linkLocalAddress: "<string>" # 選填; string
        macAddress: "<string>" # 必填; string
        parentInterface: "<string>" # 必填; string
        withdrawRouterAdvertisement: true # 選填; boolean
      gracefulActivation: # 選填; object
        readyWhen: # 必填; object
          state: # 選填; object
            "<key>": # 選填; object
              contains: "<string>" # 選填; string
              equals: "<string>" # 選填; string
              exists: true # 選填; boolean
              for: "<string>" # 選填; string
              in: # 選填; array
                - "<string>" # string
              maxAge: "<string>" # 選填; string
              status: "set" # 選填; string; enum=["set", "unset", "unknown"]
        timeout: "<string>" # 選填; string
      peers: # 選填; array
        - "<string>" # string
      preempt: true # 選填; boolean
      priority: 1 # 選填; integer; minimum=1; maximum=254
      virtualRouterID: 1 # 必填; integer; minimum=1; maximum=255
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## BGPRouter

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: 1 # 必填; integer; minimum=1
    backend: "" # 選填; string; enum=["", "gobgp"]
    communities: # 選填; object
      accept: # 選填; array
        - "<string>" # string
      send: "" # 選填; string; enum=["", "standard", "extended", "both"]
      set: # 選填; object
        in: # 選填; array
          - "<string>" # string
        out: # 選填; array
          - "<string>" # string
    convergenceProfile: "" # 選填; string; enum=["", "default", "fast", "stable"]
    exportPolicy: # 選填; object
      allowedPrefixes: # 選填; array
        - "<string>" # string
    gracefulRestart: # 選填; object
      enabled: true # 選填; boolean
      restartTime: "<string>" # 選填; string
      stalePathTime: "<string>" # 選填; string
    importPolicy: # 選填; object
      allowedPrefixLengthMax: 0 # 選填; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 選填; integer; minimum=0; maximum=128
      allowedPrefixes: # 選填; array
        - "<string>" # string
      forbiddenCommunities: # 選填; array
        - "<string>" # string
      localPreference: 0 # 選填; integer
      nextHopRewrite: "" # 選填; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 選填; array
        - "<string>" # string
    listen: # 選填; object
      address: "<string>" # 選填; string
      port: 1 # 選填; integer; minimum=1; maximum=65535
    redistribute: # 選填; object
      connected: # 選填; object
        allowedPrefixes: # 選填; array
          - "<string>" # string
      static: # 選填; object
        allowedPrefixes: # 選填; array
          - "<string>" # string
    routerID: "<string>" # 必填; string
    timers: # 選填; object
      profile: "" # 選填; string; enum=["", "default", "fast", "slow"]
    vrf: "<string>" # 選填; string
    watcher: # 選填; object
      maxPrefixes: 1 # 選填; integer; minimum=1; maximum=999999
      peerStateChangeThrottle: "<string>" # 選填; string
      pollInterval: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## BGPPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPPeer
  metadata:
    name: "<name>"
  spec:
    bfd: "<string>" # 選填; string
    communities: # 選填; object
      accept: # 選填; array
        - "<string>" # string
      send: "" # 選填; string; enum=["", "standard", "extended", "both"]
      set: # 選填; object
        in: # 選填; array
          - "<string>" # string
        out: # 選填; array
          - "<string>" # string
    convergenceProfile: "" # 選填; string; enum=["", "default", "fast", "stable"]
    ebgpMultihop: 0 # 選填; integer; minimum=0; maximum=255
    exportPolicy: # 選填; object
      allowedPrefixes: # 選填; array
        - "<string>" # string
    importPolicy: # 選填; object
      allowedPrefixLengthMax: 0 # 選填; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 選填; integer; minimum=0; maximum=128
      allowedPrefixes: # 選填; array
        - "<string>" # string
      forbiddenCommunities: # 選填; array
        - "<string>" # string
      localPreference: 0 # 選填; integer
      nextHopRewrite: "" # 選填; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 選填; array
        - "<string>" # string
    passiveMode: true # 選填; boolean
    password: "<string>" # 選填; string
    passwordFrom: # 選填; object
      base64: true # 選填; boolean
      env: "<string>" # 選填; string
      file: "<string>" # 選填; string
    peerASN: 1 # 必填; integer; minimum=1
    peers: # 選填; array
      - "<string>" # string
    routeReflectorClient: true # 選填; boolean
    routeReflectorClusterID: "<string>" # 選填; string
    routerRef: "<string>" # 必填; string
    timers: # 選填; object
      profile: "" # 選填; string; enum=["", "default", "fast", "slow"]
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## BGPDynamicPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPDynamicPeer
  metadata:
    name: "<name>"
  spec:
    ebgpMultihop: 0 # 選填; integer; minimum=0; maximum=255
    exportPolicy: # 選填; object
      allowedPrefixes: # 選填; array
        - "<string>" # string
    importPolicy: # 選填; object
      allowedPrefixLengthMax: 0 # 選填; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 選填; integer; minimum=0; maximum=128
      allowedPrefixes: # 選填; array
        - "<string>" # string
      forbiddenCommunities: # 選填; array
        - "<string>" # string
      localPreference: 0 # 選填; integer
      nextHopRewrite: "" # 選填; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 選填; array
        - "<string>" # string
    listen: # 必填; object
      sourcePrefixes: # 必填; array
        - "<string>" # string
    password: "<string>" # 選填; string
    passwordFrom: # 選填; object
      base64: true # 選填; boolean
      env: "<string>" # 選填; string
      file: "<string>" # 選填; string
    peerASN: 1 # 必填; integer; minimum=1
    routeReflectorClient: true # 選填; boolean
    routeReflectorClusterID: "<string>" # 選填; string
    routerRef: "<string>" # 必填; string
    timers: # 選填; object
      profile: "" # 選填; string; enum=["", "default", "fast", "slow"]
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## BFD

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BFD
  metadata:
    name: "<name>"
  spec:
    detectMultiplier: 1 # 選填; integer; minimum=1; maximum=50
    interface: "<string>" # 選填; string
    minRx: "<string>" # 選填; string
    minTx: "<string>" # 選填; string
    peer: "<string>" # 必填; string
    profile: "" # 選填; string; enum=["", "fast", "normal", "slow"]
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## DHCPv4Client

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: "<name>"
  spec:
    classID: "<string>" # 選填; string
    clientID: "<string>" # 選填; string
    hostname: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    requestedAddress: "<string>" # 選填; string
    routeMetric: 0 # 選填; integer; minimum=0
    useDNS: true # 選填; boolean
    useRoutes: true # 選填; boolean
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)


## ClusterNetworkRoute

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ClusterNetworkRoute
  metadata:
    name: "<name>"
  spec:
    pods: # 選填; object
      cidrs: # 選填; array
        - "<string>" # string
    services: # 選填; object
      cidrs: # 選填; array
        - "<string>" # string
    via: # 必填; array
      - # object
        interface: "<string>" # 必填; string
        nextHop: "<string>" # 必填; string
        weight: 0 # 選填; integer; minimum=0; maximum=999
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/k8s-cluster-routes.yaml)


## DHCPv4Server

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 選填; object
      end: "<string>" # 選填; string
      leaseTime: "<string>" # 選填; string
      start: "<string>" # 選填; string
    authoritative: true # 選填; boolean
    dns: # 選填; object
      cacheSize: 0 # 選填; integer; minimum=0
      enabled: true # 選填; boolean
      upstreamInterface: "<string>" # 選填; string
      upstreamServers: # 選填; array
        - "<string>" # string
      upstreamSource: "dhcpv4" # 選填; string; enum=["dhcpv4", "static", "system", "none"]
    dnsInterface: "<string>" # 選填; string
    dnsServerFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    dnsServers: # 選填; array
      - "<string>" # string
    dnsSource: "dhcpv4" # 選填; string; enum=["dhcpv4", "static", "self", "none"]
    domain: "<string>" # 選填; string
    domainFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    gateway: "<string>" # 選填; string
    gatewayFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    interface: "<string>" # 選填; string
    leaseFile: "<string>" # 選填; string
    leaseTime: "<string>" # 選填; string
    listenInterfaces: # 選填; array
      - "<string>" # string
    logDHCP: true # 選填; boolean
    managed: true # 選填; boolean
    ntpServerFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    ntpServers: # 選填; array
      - "<string>" # string
    options: # 選填; array
      - # object
        code: 1 # 選填; integer; minimum=1; maximum=65535
        name: "<string>" # 選填; string
        value: "<string>" # 選填; string
    profiles: # 選填; array
      - # object
        addressPool: # 必填; object
          end: "<string>" # 選填; string
          leaseTime: "<string>" # 選填; string
          start: "<string>" # 選填; string
        dnsServers: # 選填; array
          - "<string>" # string
        gateway: "<string>" # 選填; string
        name: "<string>" # 必填; string
        netmask: "<string>" # 選填; string
        ntpServers: # 選填; array
          - "<string>" # string
        options: # 選填; array
          - # object
            code: 1 # 選填; integer; minimum=1; maximum=65535
            name: "<string>" # 選填; string
            value: "<string>" # 選填; string
    rangeEnd: "<string>" # 選填; string
    rangeStart: "<string>" # 選填; string
    role: "server" # 選填; string; enum=["server", "transit"]
    router: "<string>" # 選填; string
    routerSource: "interfaceAddress" # 選填; string; enum=["interfaceAddress", "static", "none"]
    scopes: # 選填; array
      - # object
        addressPool: # 必填; object
          end: "<string>" # 選填; string
          leaseTime: "<string>" # 選填; string
          start: "<string>" # 選填; string
        dnsServers: # 選填; array
          - "<string>" # string
        gateway: "<string>" # 選填; string
        match: # 必填; object
          macAddresses: # 選填; array
            - "<string>" # string
          ouiPrefixes: # 選填; array
            - "<string>" # string
        name: "<string>" # 必填; string
        netmask: "<string>" # 選填; string
        ntpServers: # 選填; array
          - "<string>" # string
        options: # 選填; array
          - # object
            code: 1 # 選填; integer; minimum=1; maximum=65535
            name: "<string>" # 選填; string
            value: "<string>" # 選填; string
        profileRef: "<string>" # 選填; string
    server: "dnsmasq" # 選填; string; enum=["dnsmasq", "kea", "dhcpd"]
    stickyHoldDays: 0 # 選填; integer; minimum=0
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## DHCPv4Reservation

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # 選填; string
    ipAddress: "<string>" # 必填; string
    leaseTime: "<string>" # 選填; string
    macAddress: "<string>" # 必填; string
    options: # 選填; array
      - # object
        code: 1 # 選填; integer; minimum=1; maximum=65535
        name: "<string>" # 選填; string
        value: "<string>" # 選填; string
    scope: "<string>" # 選填; string
    server: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)


## DHCPv6Address

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Address
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    required: true # 選填; boolean
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
    interface: "<string>" # 必填; string
    managed: true # 選填; boolean
    required: true # 選填; boolean
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
    client: "<string>" # 選填; string
    clientDUID: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    prefixLength: 1 # 選填; integer; minimum=1; maximum=128
    profile: "default" # 選填; string; enum=["default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd"]
    required: true # 選填; boolean
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## IPv6DelegatedAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: "<name>"
  spec:
    addressSuffix: "<string>" # 必填; string
    announce: true # 選填; boolean
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    prefixDelegation: "<string>" # 必填; string
    prefixLength: 1 # 選填; integer; minimum=1; maximum=128
    sendRA: true # 選填; boolean
    subnetID: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DHCPv6Information

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    request: # 選填; array
      - "<string>" # string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
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
    configPath: "<string>" # 選填; string
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    dnssl: # 選填; array
      - "<string>" # string
    dnsslFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    mFlag: true # 選填; boolean
    mtu: 1280 # 選填; integer; minimum=1280; maximum=65535
    oFlag: true # 選填; boolean
    pidFile: "<string>" # 選填; string
    preferredLifetime: "<string>" # 選填; string
    prefix: "<string>" # 選填; string
    prefixFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    prfPreference: "" # 選填; string; enum=["", "low", "medium", "high"]
    rdnss: # 選填; array
      - "<string>" # string
    rdnssFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    validLifetime: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DHCPv6Server

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 選填; object
      end: "<string>" # 選填; string
      leaseTime: "<string>" # 選填; string
      start: "<string>" # 選填; string
    configPath: "<string>" # 選填; string
    defaultRoute: true # 選填; boolean
    delegatedAddress: "<string>" # 選填; string
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    dnsServerFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    dnsServers: # 選填; array
      - "<string>" # string
    dnsSource: "self" # 選填; string; enum=["self", "static", "none"]
    domainSearch: # 選填; array
      - "<string>" # string
    domainSearchFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    interface: "<string>" # 選填; string
    leaseFile: "<string>" # 選填; string
    leaseTime: "<string>" # 選填; string
    listenInterfaces: # 選填; array
      - "<string>" # string
    logDHCP: true # 選填; boolean
    managed: true # 選填; boolean
    mode: "stateless" # 選填; string; enum=["stateless", "stateful", "both", "ra-only"]
    pidFile: "<string>" # 選填; string
    rapidCommit: true # 選填; boolean
    role: "server" # 選填; string; enum=["server", "transit"]
    selfAddressPolicy: "<string>" # 選填; string
    server: "dnsmasq" # 選填; string; enum=["dnsmasq"]
    sntpServerFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    sntpServers: # 選填; array
      - "<string>" # string
    stickyHoldDays: 0 # 選填; integer; minimum=0
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## DHCPv4ServerLeaseSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # 選填; string
    interval: "<string>" # 選填; string
    source: # 選填; object
      resource: "<string>" # 必填; string
    targets: # 選填; array
      - # object
        host: "<string>" # 必填; string
        name: "<string>" # 選填; string
        options: # 選填; array
          - "<string>" # string
        sshOptions: # 選填; array
          - "<string>" # string
        user: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## DHCPv6ServerLeaseSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # 選填; string
    interval: "<string>" # 選填; string
    source: # 選填; object
      resource: "<string>" # 必填; string
    targets: # 選填; array
      - # object
        host: "<string>" # 必填; string
        name: "<string>" # 選填; string
        options: # 選填; array
          - "<string>" # string
        sshOptions: # 選填; array
          - "<string>" # string
        user: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
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
    command: "<string>" # 選填; string
    interval: "<string>" # 選填; string
    source: # 選填; object
      resource: "<string>" # 必填; string
    targets: # 選填; array
      - # object
        host: "<string>" # 必填; string
        name: "<string>" # 選填; string
        options: # 選填; array
          - "<string>" # string
        sshOptions: # 選填; array
          - "<string>" # string
        user: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
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
    interfaces: # 必填; array
      - "<string>" # string
    upstream: "<string>" # 必填; string
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
    addressFamily: "ipv4" # 必填; string; enum=["ipv4", "ipv6"]
    candidates: # 必填; array
      - # object
        address: "<string>" # 選填; string
        addressSuffix: "<string>" # 選填; string
        delegatedAddress: "<string>" # 選填; string
        interface: "<string>" # 選填; string
        matchSuffix: "<string>" # 選填; string
        ordinal: 1 # 選填; integer; minimum=1
        source: "delegatedAddress" # 必填; string; enum=["delegatedAddress", "interfaceAddress", "static"]
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
    dhcpDerived: # 選填; object
      ddns: true # 選填; boolean
      hostnameSuffix: "<string>" # 選填; string
      leaseFile: "<string>" # 選填; string
      sources: # 選填; array
        - "<string>" # string
      ttl: 0 # 選填; integer; minimum=0
    dnssec: # 選填; object
      enabled: true # 選填; boolean
      mode: "" # 選填; string; enum=["", "validate", "sign"]
    records: # 選填; array
      - # object
        hostname: "<string>" # 必填; string
        ipv4: "<string>" # 選填; string
        ipv4From: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        ipv6: "<string>" # 選填; string
        ipv6From: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        ttl: 0 # 選填; integer; minimum=0
    reverseZones: # 選填; array
      - # object
        name: "<string>" # 必填; string
    ttl: 0 # 選填; integer; minimum=0
    zone: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSResolver

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    cache: # 選填; object
      enabled: true # 選填; boolean
      maxEntries: 0 # 選填; integer; minimum=0
      maxTTL: "<string>" # 選填; string
      minTTL: "<string>" # 選填; string
      negativeTTL: "<string>" # 選填; string
    listen: # 必填; array
      - # object
        addressFrom: # 選填; array
          - # object
            field: "<string>" # 選填; string
            optional: true # 選填; boolean
            resource: "<string>" # 必填; string
        addresses: # 選填; array
          - "<string>" # string
        name: "<string>" # 選填; string
        port: 1 # 選填; integer; minimum=1; maximum=65535
        sources: # 選填; array
          - "<string>" # string
    metrics: # 選填; object
      perUpstream: true # 選填; boolean
    queryLog: # 選填; object
      enabled: true # 選填; boolean
      path: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSForwarder

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    dnssecValidate: true # 選填; boolean
    healthcheck: # 選填; object
      failThreshold: 1 # 選填; integer; minimum=1
      interval: "<string>" # 選填; string
      passThreshold: 1 # 選填; integer; minimum=1
      timeout: "<string>" # 選填; string
    match: # 必填; array
      - "<string>" # string
    queryTimeout: "<string>" # 選填; string
    resolver: "<string>" # 必填; string
    upstreams: # 選填; array
      - "<string>" # string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
    zoneRefs: # 選填; array
      - "<string>" # string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSUpstream

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 選填; string
    addressFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    bootstrap: # 選填; array
      - "<string>" # string
    bootstrapFrom: # 選填; array
      - # object
        field: "<string>" # 必填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    path: "<string>" # 選填; string
    port: 1 # 選填; integer; minimum=1; maximum=65535
    protocol: "udp" # 必填; string; enum=["udp", "tcp", "dot", "doh"]
    sourceInterface: "<string>" # 選填; string
    tlsName: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DSLiteTunnel

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    aftrAddressOrdinal: 1 # 選填; integer; minimum=1
    aftrAddressSelection: "ordinal" # 選填; string; enum=["ordinal", "ordinalModulo"]
    aftrDNSServers: # 選填; array
      - "<string>" # string
    aftrFQDN: "<string>" # 選填; string
    aftrFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    aftrIPv6: "<string>" # 選填; string
    defaultRoute: true # 選填; boolean
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    enabled: true # 選填; boolean
    encapsulationLimit: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    localAddress: "<string>" # 選填; string
    localAddressFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    localAddressSource: "interface" # 選填; string; enum=["interface", "static", "delegatedAddress"]
    localAddressSuffix: "<string>" # 選填; string
    localDelegatedAddress: "<string>" # 選填; string
    mtu: 1280 # 選填; integer; minimum=1280; maximum=65535
    remoteAddress: "<string>" # 選填; string
    routeMetric: 0 # 選填; integer; minimum=0
    tunnelName: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## TunnelInterface

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 選填; string
    encapDport: 1 # 選填; integer; minimum=1; maximum=65535
    encapSport: 1 # 選填; integer; minimum=1; maximum=65535
    key: 0 # 選填; integer; minimum=0; maximum=4294967295
    local: "<string>" # 選填; string
    localFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    mode: "ipip" # 必填; string; enum=["ipip", "gre", "fou", "gue"]
    mtu: 576 # 選填; integer; minimum=576; maximum=9216
    pathMTU: # 選填; object
      forceFragmentIPv4: true # 選填; boolean
    peerAddress: "<string>" # 選填; string
    remote: "<string>" # 選填; string
    remoteFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    trustedUnderlay: true # 必填; boolean
    ttl: 1 # 選填; integer; minimum=1; maximum=255
    underlayInterface: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## OverlayPeer

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: "<string>" # 必填; string
    pathMTU: # 選填; object
      forceFragmentIPv4: true # 選填; boolean
    remote: # 選填; object
      address: "<string>" # 選填; string
      nodeID: "<string>" # 選填; string
    role: "onprem" # 必填; string; enum=["onprem", "cloud"]
    underlay: # 必填; object
      address: "<string>" # 選填; string
      interface: "<string>" # 選填; string
      type: "wireguard" # 必填; string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## HybridRoute

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: HybridRoute
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # 必填; array
      - "<string>" # string
    healthCheckRef: "<string>" # 選填; string
    install: # 選填; object
      metric: 0 # 選填; integer; minimum=0
      table: "" # 選填; string; enum=["", "main"]
    peerRef: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## CloudProviderProfile

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: CloudProviderProfile
  metadata:
    name: "<name>"
  spec:
    auth: # 必填; object
      command: "<string>" # 選填; string
      mode: "external-command" # 必填; string; enum=["external-command"]
    capabilities: # 必填; array
      - "<string>" # string
    provider: "azure" # 必填; string; enum=["azure", "aws", "oci", "gcp"]
    resourceGroup: "<string>" # 選填; string
    subscriptionID: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## ProviderActionPolicy

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    allowUndo: true # 選填; boolean
    allowedActions: # 選填; array
      - "<string>" # string
    allowedCIDRs: # 選填; array
      - "<string>" # string
    allowedProviderRefs: # 選填; array
      - "<string>" # string
    allowedProviders: # 選填; array
      - "<string>" # string
    dryRunOnly: true # 選填; boolean
    enabled: true # 選填; boolean
    executionWindow: "<string>" # 選填; string
    maxActionsPerRun: 0 # 選填; integer
    requireApproval: true # 選填; boolean
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventGroup

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    auth: # 選填; object
      mode: "" # 選填; string; enum=["", "hmac"]
      secretFile: "<string>" # 選填; string
      secretRef: "<string>" # 選填; string
    listen: # 選填; object
      address: "<string>" # 選填; string
      port: 0 # 選填; integer; minimum=0; maximum=65535
    nodeName: "<string>" # 必填; string
    peersFrom: # 選填; array
      - # object
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    replayWindow: "<string>" # 選填; string
    retention: # 選填; object
      maxAge: "<string>" # 選填; string
      maxEvents: 0 # 選填; integer; minimum=0
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventPeer

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    direction: "push" # 選填; string; enum=["push"]
    endpoint: "<string>" # 選填; string
    groupRef: "<string>" # 必填; string
    nodeName: "<string>" # 必填; string
    subjectPrefixes: # 選填; array
      - "<string>" # string
    types: # 選填; array
      - "<string>" # string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventSubscription

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventSubscription
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必填; string
    match: # 必填; object
      payload: # 選填; object
        "<key>": "<string>" # 選填; string
      sourceNodes: # 選填; array
        - "<string>" # string
      subjectPrefixes: # 選填; array
        - "<string>" # string
      types: # 必填; array
        - "<string>" # string
    trigger: # 必填; object
      batchWindow: "<string>" # 選填; string
      debounce: "<string>" # 選填; string
      pluginRef: "<string>" # 必填; string
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
    delivery: # 選填; object
      expiresSoonSeconds: 0 # 選填; integer; minimum=0
      lagFailSeconds: 0 # 選填; integer; minimum=0
      lagWarnSeconds: 0 # 選填; integer; minimum=0
    groupRef: "<string>" # 必填; string
    subscription: # 選填; object
      maxFailedRuns: 0 # 選填; integer; minimum=0
      maxPendingRuns: 0 # 選填; integer; minimum=0
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
    groupRef: "<string>" # 必填; string
    members: # 選填; array
      - # object
        capture: # 選填; object
          activeWhen: # 選填; object
            type: "" # 選填; string; enum=["", "single-router", "vrrp-master"]
            virtualAddressRef: "<string>" # 選填; string
          captureStrategy: "" # 選填; string; enum=["", "route-table"]
          excludeAddresses: # 選填; array
            - "<string>" # string
          gratuitousARP: true # 選填; boolean
          interface: "<string>" # 選填; string
          nicRef: "<string>" # 選填; string
          providerRef: "<string>" # 選填; string
          sourceAddress: "<string>" # 選填; string
          sourceAddressFrom: # 選填; object
            field: "<string>" # 選填; string
            optional: true # 選填; boolean
            resource: "<string>" # 必填; string
          target: # 選填; object
            "<key>": "<string>" # 選填; string
          targetFrom: # 選填; object
            "<key>": "<string>" # 選填; string
          type: "provider-secondary-ip" # 選填; string; enum=["provider-secondary-ip", "proxy-arp"]
        nodeRef: "<string>" # 必填; string
        ownershipDiscovery: # 選填; object
          allowEmptyAfter: "<string>" # 選填; string
          leaseTTL: "<string>" # 選填; string
          mode: "" # 選填; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
          pluginRef: "<string>" # 選填; string
          providerRef: "<string>" # 選填; string
          scanInterval: "<string>" # 選填; string
          scope: # 選填; object
            excludeAddresses: # 選填; array
              - "<string>" # string
            includeAddresses: # 選填; array
              - "<string>" # string
          selector: # 選填; object
            tags: # 選填; object
              "<key>": "<string>" # 選填; string
          sources: # 選填; array
            - # object
              bridge: "<string>" # 選填; string
              interface: "<string>" # 選填; string
              leaseTTL: "<string>" # 選填; string
              network: "<string>" # 選填; string
              probeRetries: 0 # 選填; integer; minimum=0; maximum=20
              probeTimeout: "<string>" # 選填; string
              resource: "<string>" # 選填; string
              scanInterval: "<string>" # 選填; string
              sourceAddressFrom: # 選填; object
                field: "<string>" # 選填; string
                optional: true # 選填; boolean
                resource: "<string>" # 必填; string
              type: "dhcpv4-lease" # 必填; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
          stoppedInstancePolicy: "" # 選填; string; enum=["", "hold", "release"]
          subnetRef: "<string>" # 選填; string
          subnetRefFrom: "<string>" # 選填; string
        profileRef: "<string>" # 選填; string
        staticOwnedAddresses: # 選填; array
          - "<string>" # string
    membersFrom: # 必填; array; minItems=1
      - # object
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    prefix: "<string>" # 必填; string
    profiles: # 選填; object
      cloudCaptures: # 選填; object
        "<key>": # 選填; object
          capture: # 選填; object
            activeWhen: # 選填; object
              type: "" # 選填; string; enum=["", "single-router", "vrrp-master"]
              virtualAddressRef: "<string>" # 選填; string
            captureStrategy: "" # 選填; string; enum=["", "route-table"]
            excludeAddresses: # 選填; array
              - "<string>" # string
            gratuitousARP: true # 選填; boolean
            interface: "<string>" # 選填; string
            nicRef: "<string>" # 選填; string
            providerRef: "<string>" # 選填; string
            sourceAddress: "<string>" # 選填; string
            sourceAddressFrom: # 選填; object
              field: "<string>" # 選填; string
              optional: true # 選填; boolean
              resource: "<string>" # 必填; string
            target: # 選填; object
              "<key>": "<string>" # 選填; string
            targetFrom: # 選填; object
              "<key>": "<string>" # 選填; string
            type: "provider-secondary-ip" # 選填; string; enum=["provider-secondary-ip", "proxy-arp"]
          ownershipDiscovery: # 選填; object
            allowEmptyAfter: "<string>" # 選填; string
            leaseTTL: "<string>" # 選填; string
            mode: "" # 選填; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
            pluginRef: "<string>" # 選填; string
            providerRef: "<string>" # 選填; string
            scanInterval: "<string>" # 選填; string
            scope: # 選填; object
              excludeAddresses: # 選填; array
                - "<string>" # string
              includeAddresses: # 選填; array
                - "<string>" # string
            selector: # 選填; object
              tags: # 選填; object
                "<key>": "<string>" # 選填; string
            sources: # 選填; array
              - # object
                bridge: "<string>" # 選填; string
                interface: "<string>" # 選填; string
                leaseTTL: "<string>" # 選填; string
                network: "<string>" # 選填; string
                probeRetries: 0 # 選填; integer; minimum=0; maximum=20
                probeTimeout: "<string>" # 選填; string
                resource: "<string>" # 選填; string
                scanInterval: "<string>" # 選填; string
                sourceAddressFrom: # 選填; object
                  field: "<string>" # 選填; string
                  optional: true # 選填; boolean
                  resource: "<string>" # 必填; string
                type: "dhcpv4-lease" # 必填; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
            stoppedInstancePolicy: "" # 選填; string; enum=["", "hold", "release"]
            subnetRef: "<string>" # 選填; string
            subnetRefFrom: "<string>" # 選填; string
    staticHandovers: # 選填; array
      - # object
        address: "<string>" # 必填; string
        fromNodeRef: "<string>" # 必填; string
        toNodeRef: "<string>" # 必填; string
    values: # 選填; object
      "<key>": "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## SAMNodeSet

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes: # 必填; array
      - # object
        eventEndpoint: "<string>" # 選填; string
        macAddresses: # 選填; array
          - "<string>" # string; pattern="^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$"
        maintenance: # 選填; object
          drain: true # 選填; boolean
        maxSecondaryIPs: 0 # 選填; integer; minimum=0
        nodeRef: "<string>" # 必填; string
        placement: # 選填; object
          group: "<string>" # 選填; string
          priority: 0 # 選填; integer; minimum=0; maximum=1000000
        role: "" # 選填; string; enum=["", "onprem", "cloud"]
        routeReflector: true # 選填; boolean
        samEndpoint: "<string>" # 選填; string
        samEndpointFrom: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        site: "<string>" # 選填; string
        wireGuard: # 選填; object
          allowedIPs: # 選填; array
            - "<string>" # string
          endpoint: "<string>" # 選填; string
          persistentKeepalive: 0 # 選填; integer; minimum=0; maximum=65535
          publicKey: "<string>" # 選填; string
          transportEndpoint: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)


## SAMEnrollmentPolicy

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentPolicy
  metadata:
    name: "<name>"
  spec:
    allowedLeafIDs: # 選填; object
      pattern: "<string>" # 選填; string
    directMesh: # 選填; object
      peerGroupRef: "<string>" # 選填; string
    endpointPrefixes: # 選填; array
      - "<string>" # string
    joinAudience: "<string>" # 選填; string
    joinTokenFrom: # 選填; object
      base64: true # 選填; boolean
      env: "<string>" # 選填; string
      file: "<string>" # 選填; string
    mobilityPoolRefs: # 選填; array
      - "<string>" # string
    mobilityPrefixes: # 選填; array
      - "<string>" # string
    rrNodeSetRef: "<string>" # 選填; string
    rrSetRef: "<string>" # 選填; string
    transportProfileRef: "<string>" # 必填; string
    ttl: "<string>" # 選填; string
    tunnelAddressPrefixes: # 必填; array
      - "<string>" # string
    wireGuard: # 選填; object
      allowedExtraIPPrefixes: # 選填; array
        - "<string>" # string
      endpointPrefixes: # 選填; array
        - "<string>" # string
      interface: "<string>" # 必填; string
      persistentKeepalive: 0 # 選填; integer; minimum=0; maximum=65535
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)


## SAMEnrollmentClaim

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClaim
  metadata:
    name: "<name>"
  spec:
    bgp: # 選填; object
      asn: 1 # 選填; integer; minimum=1
      routerID: "<string>" # 選填; string
    directMesh: true # 選填; boolean
    endpoint: "<string>" # 選填; string
    expiresAt: "<string>" # 選填; string
    joinAudience: "<string>" # 選填; string
    joinHMAC: "<string>" # 選填; string
    joinNonce: "<string>" # 選填; string
    joinTimestamp: "<string>" # 選填; string
    leafID: "<string>" # 必填; string
    mobility: # 選填; object
      ownedAddresses: # 選填; array
        - "<string>" # string
    policyRef: "<string>" # 必填; string
    revoked: true # 選填; boolean
    rrSetRef: "<string>" # 選填; string
    tunnelAddress: "<string>" # 必填; string
    wireGuard: # 選填; object
      allowedIPs: # 選填; array
        - "<string>" # string
      endpoint: "<string>" # 選填; string
      persistentKeepalive: 0 # 選填; integer; minimum=0; maximum=65535
      publicKey: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)


## SAMEnrollmentClient

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClient
  metadata:
    name: "<name>"
  spec:
    bootstrapEndpoints: # 選填; array
      - "<string>" # string
    claimRef: "<string>" # 必填; string
    controlAPITLS: # 選填; object
      caFile: "<string>" # 選填; string
      certFile: "<string>" # 選填; string
      insecureSkipVerify: true # 選填; boolean
      keyFile: "<string>" # 選填; string
      serverName: "<string>" # 選填; string
    controlAPITokenFrom: # 選填; object
      base64: true # 選填; boolean
      env: "<string>" # 選填; string
      file: "<string>" # 選填; string
    retryBackoff: # 選填; object
      max: "<string>" # 選填; string
      min: "<string>" # 選填; string
    rrSocket: "<string>" # 選填; string
    stateTTLRefreshBefore: "<string>" # 選填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)


## SAMTransportProfile

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMTransportProfile
  metadata:
    name: "<name>"
  spec:
    addressingMode: "" # 選填; string; enum=["", "edge-index", "pair-stable"]
    bgp: # 必填; object
      bfd: # 選填; object
        detectMultiplier: 1 # 選填; integer; minimum=1; maximum=50
        enabled: true # 選填; boolean
        interface: "<string>" # 選填; string
        minRx: "<string>" # 選填; string
        minTx: "<string>" # 選填; string
        profile: "" # 選填; string; enum=["", "fast", "normal", "slow"]
      convergenceProfile: "" # 選填; string; enum=["", "default", "fast", "stable"]
      directLocalPreference: 0 # 選填; integer
      ebgpMultihop: 0 # 選填; integer; minimum=0; maximum=255
      exportPolicy: # 選填; object
        allowedPrefixes: # 選填; array
          - "<string>" # string
      generatePeers: true # 選填; boolean
      importPolicy: # 選填; object
        allowedPrefixLengthMax: 0 # 選填; integer; minimum=0; maximum=128
        allowedPrefixLengthMin: 0 # 選填; integer; minimum=0; maximum=128
        allowedPrefixes: # 選填; array
          - "<string>" # string
        forbiddenCommunities: # 選填; array
          - "<string>" # string
        localPreference: 0 # 選填; integer
        nextHopRewrite: "" # 選填; string; enum=["", "peer-address", "unchanged"]
        requiredCommunities: # 選填; array
          - "<string>" # string
      peerASN: 1 # 必填; integer; minimum=1
      routeReflectorClient: true # 選填; boolean
      routeReflectorClusterID: "<string>" # 選填; string
      routerRef: "<string>" # 必填; string
      timers: # 選填; object
        profile: "" # 選填; string; enum=["", "default", "fast", "slow"]
      timersPreset: "" # 選填; string; enum=["", "default", "fast", "slow"]
    encapDport: 1 # 選填; integer; minimum=1; maximum=65535
    encapSport: 1 # 選填; integer; minimum=1; maximum=65535
    encryption: "" # 選填; string; enum=["", "none", "wireguard"]
    innerPrefix: "<string>" # 必填; string
    localEndpoint: "<string>" # 選填; string
    localEndpointFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    mode: "ipip" # 必填; string; enum=["ipip", "gre", "fou", "gue"]
    peersFrom: # 選填; array
      - # object
        direct: true # 選填; boolean
        nodeRefs: # 選填; array
          - "<string>" # string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    publishPeerGroup: true # 選填; boolean
    selfNodeRef: "<string>" # 必填; string
    underlayInterface: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## SAMSubnetPolicy

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMSubnetPolicy
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必填; string
    poolRef: "<string>" # 必填; string
    shards: # 必填; array
      - # object
        assignedNodes: # 必填; array
          - "<string>" # string
        prefix: "<string>" # 必填; string
    sourcePrefix: "<string>" # 必填; string
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
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    destination: "<string>" # 必填; string
    device: "<string>" # 選填; string
    deviceFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    gateway: "<string>" # 選填; string
    gatewayFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    metric: 0 # 選填; integer; minimum=0
    preferredSource: "<string>" # 選填; string
    type: "unicast" # 選填; string; enum=["unicast", "blackhole"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)


## HealthCheck

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # 選填; string; enum=["ipv4", "ipv6"]
    enabled: true # 選填; boolean
    healthyThreshold: 1 # 選填; integer; minimum=1
    interface: "<string>" # 選填; string
    interval: "<string>" # 選填; string
    port: 0 # 選填; integer; minimum=0; maximum=65535
    protocol: "" # 選填; string; enum=["", "icmp", "tcp", "dns", "http"]
    role: "link" # 選填; string; enum=["link", "next-hop", "internet", "service", "policy"]
    target: "<string>" # 選填; string
    targetSource: "auto" # 選填; string; enum=["auto", "static", "defaultGateway", "dsliteRemote"]
    timeout: "<string>" # 選填; string
    type: "ping" # 選填; string; enum=["ping"]
    unhealthyThreshold: 1 # 選填; integer; minimum=1
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## EgressRoutePolicy

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates: # 必填; array
      - # object
        dependsOn: # 選填; array
          - # object
            equals: "<string>" # 選填; string
            field: "<string>" # 選填; string
            notEmpty: true # 選填; boolean
            optional: true # 選填; boolean
            phase: "<string>" # 選填; string
            resource: "<string>" # 必填; string
        device: "<string>" # 選填; string
        deviceFrom: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        enabled: true # 選填; boolean
        gateway: "<string>" # 選填; string
        gatewayFrom: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        gatewaySource: "" # 選填; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
        healthCheck: "<string>" # 選填; string
        interface: "<string>" # 選填; string
        mark: 0 # 選填; integer; minimum=0; maximum=4294967295
        metric: 0 # 選填; integer; minimum=0
        name: "<string>" # 選填; string
        preferredSource: "" # 選填; string; enum=["", "interface"]
        priority: 0 # 選填; integer; minimum=0; maximum=32765
        routeMetric: 0 # 選填; integer; minimum=0
        routeTable: 0 # 選填; integer; minimum=0; maximum=4294967295
        source: "<string>" # 選填; string
        table: 0 # 選填; integer; minimum=0; maximum=4294967295
        targets: # 選填; array
          - # object
            gateway: "<string>" # 選填; string
            gatewayFrom: # 選填; object
              field: "<string>" # 選填; string
              optional: true # 選填; boolean
              resource: "<string>" # 必填; string
            gatewaySource: "" # 選填; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
            healthCheck: "<string>" # 選填; string
            interface: "<string>" # 選填; string
            mark: 0 # 選填; integer; minimum=0; maximum=4294967295
            metric: 0 # 選填; integer; minimum=0
            name: "<string>" # 選填; string
            outboundInterface: "<string>" # 選填; string
            priority: 0 # 選填; integer; minimum=0; maximum=32765
            routeMetric: 0 # 選填; integer; minimum=0
            routeTable: 0 # 選填; integer; minimum=0; maximum=4294967295
            table: 0 # 選填; integer; minimum=0; maximum=4294967295
        weight: 0 # 選填; integer; minimum=0
        when: # 選填; object
          state: # 選填; object
            "<key>": # 選填; object
              contains: "<string>" # 選填; string
              equals: "<string>" # 選填; string
              exists: true # 選填; boolean
              for: "<string>" # 選填; string
              in: # 選填; array
                - "<string>" # string
              maxAge: "<string>" # 選填; string
              status: "set" # 選填; string; enum=["set", "unset", "unknown"]
    destinationCIDRs: # 選填; array
      - "<string>" # string
    destinationSetRefs: # 選填; array
      - "<string>" # string
    excludeDestinationCIDRs: # 選填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 選填; array
      - "<string>" # string
    family: "ipv4" # 選填; string; enum=["ipv4", "ipv6"]
    hashFields: # 選填; array
      - "<string>" # string
    hostTraffic: true # 選填; boolean
    hysteresis: "<string>" # 選填; string
    mode: "priority" # 選填; string; enum=["priority", "mark", "hash"]
    selection: "highest-weight-ready" # 選填; string; enum=["highest-weight-ready", "weighted-ecmp"]
    sourceCIDRs: # 選填; array
      - "<string>" # string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## EventRule

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EventRule
  metadata:
    name: "<name>"
  spec:
    emit: # 必填; object
      attributes: # 選填; object
        "<key>": "<string>" # 選填; string
      topic: "<string>" # 必填; string
    pattern: # 必填; object
      allow_missing_correlation: true # 選填; boolean
      correlate_by: "<string>" # 選填; string
      duration: "<string>" # 選填; string
      expected: "<string>" # 選填; string
      interval: "<string>" # 選填; string
      operator: "all_of" # 必填; string; enum=["all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"]
      quiet: "<string>" # 選填; string
      rate: 0 # 選填; integer; minimum=0
      strict: true # 選填; boolean
      threshold: 0 # 選填; integer; minimum=0
      topic: "<string>" # 選填; string
      topics: # 選填; array
        - "<string>" # string
      trigger: "<string>" # 選填; string
      window: "<string>" # 選填; string
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
    emitInitial: true # 選填; boolean
    emitWhen: "all_true" # 選填; string; enum=["all_true", "any_true"]
    hysteresis: "<string>" # 選填; string
    inputs: # 必填; array
      - # object
        any_of: # 選填; array
          - # array
            - # object
              equals: "<string>" # 選填; string
              field: "<string>" # 選填; string
              not_empty: true # 選填; boolean
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        not_empty: true # 選填; boolean
    retractWhen: "any_false" # 選填; string; enum=["any_false", "all_false"]
    topic: "<string>" # 必填; string
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
    destinationCIDRs: # 選填; array
      - "<string>" # string
    destinationSetRefs: # 選填; array
      - "<string>" # string
    egressInterface: "<string>" # 選填; string
    egressPolicyRef: "<string>" # 選填; string
    excludeDestinationCIDRs: # 選填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 選填; array
      - "<string>" # string
    outboundInterface: "<string>" # 選填; string
    snatAddress: "<string>" # 選填; string
    snatAddressFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    sourceCIDRs: # 選填; array
      - "<string>" # string
    sourceRanges: # 選填; array
      - "<string>" # string
    translation: # 選填; object
      address: "<string>" # 選填; string
      addresses: # 選填; array
        - "<string>" # string
      portMapping: # 選填; object
        end: 1 # 選填; integer; minimum=1; maximum=65535
        start: 1 # 選填; integer; minimum=1; maximum=65535
        type: "auto" # 選填; string; enum=["auto", "preserve", "range"]
      type: "interfaceAddress" # 必填; string; enum=["interfaceAddress", "address", "pool"]
    type: "masquerade" # 選填; string; enum=["masquerade", "snat"]
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)


## NAT44FlowDNATPinhole

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44FlowDNATPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # 選填; object
      key: "" # 選填; string; enum=["", "localPort"]
    fromInterfaceRefs: # 選填; array
      - "<string>" # string
    inbound: # 必填; object
      sourcePorts: # 必填; array
        - "<string>" # string
    outbound: # 必填; object
      destinationCIDRs: # 選填; array
        - "<string>" # string
      destinationPorts: # 必填; array
        - "<string>" # string
      destinationSetRefs: # 選填; array
        - "<string>" # string
      protocol: "udp" # 必填; string; enum=["udp"]
      sourceCIDRs: # 選填; array
        - "<string>" # string
      sourceSetRef: "<string>" # 選填; string
    timeout: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
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
    conntrackCommand: "<string>" # 選填; string
    conntrackd: # 選填; object
      ignoreIPv4: # 選填; array
        - "<string>" # string
      interface: "<string>" # 必填; string
      localAddress: "<string>" # 必填; string
      peerAddress: "<string>" # 必填; string
      port: 1 # 選填; integer; minimum=1; maximum=65535
    excludeNatRules: # 選填; array
      - "<string>" # string
    interval: "<string>" # 選填; string
    mode: "event-stream" # 選填; string; enum=["event-stream", "conntrackd"]
    natRules: # 選填; array
      - "<string>" # string
    snatAddresses: # 選填; array
      - "<string>" # string
    targets: # 選填; array
      - # object
        host: "<string>" # 必填; string
        name: "<string>" # 選填; string
        restoreCommand: # 選填; array
          - "<string>" # string
        sshOptions: # 選填; array
          - "<string>" # string
        user: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
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
    allowSourceCIDRs: # 選填; array
      - "<string>" # string
    interfaces: # 必填; array
      - "<string>" # string
    requireWebConsoleBound: true # 選填; boolean
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## PortForward

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    hairpin: # 選填; object
      enabled: true # 選填; boolean
      interfaces: # 選填; array
        - "<string>" # string
      mode: "" # 選填; string; enum=["", "auto", "manual", "off"]
    listen: # 必填; object
      address: "<string>" # 選填; string
      addressFrom: # 選填; object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
      interface: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必填; string; enum=["tcp", "udp"]
    target: # 必填; object
      address: "<string>" # 選填; string
      addressFrom: # 選填; object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)


## IngressService

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends: # 必填; array
      - # object
        address: "<string>" # 選填; string
        addressFrom: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        name: "<string>" # 選填; string
        port: 1 # 必填; integer; minimum=1; maximum=65535
        weight: 0 # 選填; integer; minimum=0
    externalDNS: true # 選填; boolean
    hairpin: # 選填; object
      enabled: true # 選填; boolean
      interfaces: # 選填; array
        - "<string>" # string
      mode: "" # 選填; string; enum=["", "auto", "manual", "off"]
    healthCheck: # 選填; object
      expectedBody: "<string>" # 選填; string
      expectedStatus: # 選填; array
        - 0 # integer
      healthyThreshold: 1 # 選填; integer; minimum=1
      host: "<string>" # 選填; string
      interval: "<string>" # 選填; string
      path: "<string>" # 選填; string
      protocol: "" # 選填; string; enum=["", "tcp", "http", "https"]
      timeout: "<string>" # 選填; string
      tlsSkipVerify: true # 選填; boolean
      unhealthyThreshold: 1 # 選填; integer; minimum=1
    hostname: "<string>" # 選填; string
    listen: # 必填; object
      address: "<string>" # 選填; string
      addressFrom: # 選填; object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
      interface: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必填; string; enum=["tcp", "udp"]
    policy: # 選填; object
      onNoHealthyBackends: "" # 選填; string; enum=["", "drop", "reject"]
      selection: "" # 選填; string; enum=["", "failover", "sourceHash", "random"]
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)


## IPAddressSet

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPAddressSet
  metadata:
    name: "<name>"
  spec:
    addresses: # 選填; array
      - "<string>" # string
    names: # 選填; array
      - "<string>" # string
    refreshInterval: "<string>" # 選填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)


## LocalServiceRedirect

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: LocalServiceRedirect
  metadata:
    name: "<name>"
  spec:
    interface: "<string>" # 必填; string
    rules: # 必填; array
      - # object
        destinationPort: 1 # 必填; integer; minimum=1; maximum=65535
        destinationSetRef: "<string>" # 必填; string
        name: "<string>" # 選填; string
        protocols: # 必填; array
          - "<string>" # string
        redirectPort: 1 # 必填; integer; minimum=1; maximum=65535
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)


## FirewallFlowPinhole

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallFlowPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # 選填; object
      key: "" # 選填; string; enum=["", "remoteAddress", "localEndpoint"]
    fromZone: "<string>" # 必填; string
    inbound: # 必填; object
      sourcePorts: # 必填; array
        - "<string>" # string
    outbound: # 必填; object
      destinationCIDRs: # 選填; array
        - "<string>" # string
      destinationPorts: # 必填; array
        - "<string>" # string
      destinationSetRefs: # 選填; array
        - "<string>" # string
      protocol: "udp" # 必填; string; enum=["udp"]
      sourceCIDRs: # 選填; array
        - "<string>" # string
      sourceSetRef: "<string>" # 選填; string
    timeout: "<string>" # 選填; string
    toZone: "<string>" # 必填; string
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
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
    enabled: true # 選填; boolean
    includeApplicationLayer: true # 選填; boolean
    includeTLSSNI: true # 選填; boolean
    path: "<string>" # 選填; string
    source: "" # 選填; string; enum=["", "conntrack"]
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
    interfaces: # 必填; array
      - "<string>" # string
    role: "untrust" # 必填; string; enum=["untrust", "trust", "mgmt"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)


## FirewallPolicy

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: "<name>"
  spec:
    logDeny: true # 選填; boolean
    sameRoleAccept: true # 選填; boolean
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)


## ClientPolicy

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    classification: # 選填; array
      - # object
        ipv4Reservation: "<string>" # 選填; string
        ipv6Addresses: # 選填; array
          - "<string>" # string
        match: # 必填; object
          dhcpFingerprints: # 選填; array
            - "<string>" # string
          hostnamePatterns: # 選填; array
            - "<string>" # string
          macs: # 選填; array
            - "<string>" # string
          ouiPrefixes: # 選填; array
            - "<string>" # string
        mode: "trusted" # 必填; string; enum=["trusted", "guest", "isolated"]
        name: "<string>" # 選填; string
    guestEgressAllow: # 選填; array
      - "<string>" # string
    guestEgressDeny: # 選填; array
      - "<string>" # string
    guestServices: # 選填; array
      - "<string>" # string
    interfaces: # 選填; array
      - "<string>" # string
    isolation: # 選填; object
      lanInternet: "" # 選填; string; enum=["", "allow", "deny"]
      lanLAN: "" # 選填; string; enum=["", "allow", "deny"]
      lanMgmt: "" # 選填; string; enum=["", "allow", "deny"]
      mDNSBroadcast: "" # 選填; string; enum=["", "allow", "deny"]
    macs: # 選填; array
      - "<string>" # string
    mode: "include" # 必填; string; enum=["include", "exclude"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)


## FirewallRule

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: "<name>"
  spec:
    action: "accept" # 必填; string; enum=["accept", "drop", "reject"]
    connLimit: # 選填; object
      log: true # 選填; boolean
      maxPerSource: 0 # 選填; integer; minimum=0
    destinationCIDRs: # 選填; array
      - "<string>" # string
    destinationPorts: # 選填; array
      - "<string>" # string
    destinationSetRefs: # 選填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 選填; array
      - "<string>" # string
    fromZone: "<string>" # 必填; string
    icmpType: "<string>" # 選填; string
    icmpv6Type: "<string>" # 選填; string
    log: true # 選填; boolean
    port: 0 # 選填; integer; minimum=0; maximum=65535
    protocol: "" # 選填; string; enum=["", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip"]
    rateLimit: # 選填; object
      burst: 0 # 選填; integer; minimum=0
      log: true # 選填; boolean
      packetsPerSecond: 0 # 選填; integer; minimum=0
      per: "" # 選填; string; enum=["", "second", "minute"]
      rate: 0 # 選填; integer; minimum=0
      unit: "" # 選填; string; enum=["", "packet", "byte", "kilobyte", "megabyte"]
    sourcePorts: # 選填; array
      - "<string>" # string
    srcCIDRs: # 選填; array
      - "<string>" # string
    toZone: "<string>" # 必填; string
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)


## FirewallEventLog

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定義

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # 選填; boolean
    events: # 選填; array
      - "deny" # string; enum=["deny", "allow", "rateLimit", "connLimit"]
    fromZones: # 選填; array
      - "<string>" # string
    log: # 選填; object
      acceptSampleRate: 0 # 選填; integer; minimum=0
      copyRange: 0 # 選填; integer; minimum=0
      dropSampleRate: 0 # 選填; integer; minimum=0
      reject: true # 選填; boolean
    nflogGroup: 0 # 選填; integer; minimum=0; maximum=65535
    path: "<string>" # 選填; string
    retention: "<string>" # 選填; string
    rules: # 選填; array
      - "<string>" # string
    sampleRate: 0 # 選填; integer; minimum=0
    sinks: # 選填; array
      - "<string>" # string
    toZones: # 選填; array
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
    hostname: "<string>" # 必填; string
    managed: true # 選填; boolean
```
