# 各资源的配置

资源写在Router.spec.resources内。示例中的名称、接口、地址、凭据及对端配置必须按环境调整。必填表示在所属对象内必须指定。跨资源引用与语义限制还须通过routerd validate确认。

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；只有default注记表示默认值。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

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

### YAML定义

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: Plugin
  metadata:
    name: "<name>"
  spec:
    capabilities: # 选填; array
      - "observe.cloud" # string; enum=["observe.cloud", "observe.providerPrivateIPs", "propose.dynamicConfig", "propose.providerAction", "execute.providerAction"]
    context: # 选填; object
      resources: # 选填; array
        - # object
          apiVersion: "<string>" # 必填; string
          kind: "<string>" # 必填; string
          name: "<string>" # 必填; string
    env: # 选填; object
      "<key>": "<string>" # 选填; string
    executable: "<string>" # 必填; string
    timeout: "<string>" # 选填; string
    triggers: # 选填; array
      - # object
        every: "<string>" # 选填; string
        topic: "<string>" # 选填; string
        type: "interval" # 必填; string; enum=["interval", "event"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## DynamicConfigSource

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: DynamicConfigSource
  metadata:
    name: "<name>"
  spec:
    mergePolicy: # 选填; object
      conflict: "" # 选填; string; enum=["", "reject"]
    pluginRef: "<string>" # 必填; string
    triggers: # 选填; array
      - # object
        every: "<string>" # 选填; string
        topic: "<string>" # 选填; string
        type: "interval" # 必填; string; enum=["interval", "event"]
    ttl: "<string>" # 选填; string
```


## DynamicOverridePolicy

`apiVersion: config.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

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

### YAML定义

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogSink
  metadata:
    name: "<name>"
  spec:
    enabled: true # 选填; boolean
    file: # 选填; object
      name: "<string>" # 选填; string
    journald: # 选填; object
      identifier: "<string>" # 选填; string
    minLevel: "debug" # 选填; string; enum=["debug", "info", "warning", "error"]
    otlp: # 选填; object
      endpoint: "<string>" # 选填; string
      telemetryRef: "<string>" # 选填; string
    syslog: # 选填; object
      address: "<string>" # 选填; string
      facility: "kern" # 选填; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
      network: "" # 选填; string; enum=["", "unix", "unixgram", "tcp", "udp"]
      tag: "<string>" # 选填; string
    type: "syslog" # 必填; string; enum=["syslog", "otlp", "webhook", "file", "journald"]
    webhook: # 选填; object
      headers: # 选填; object
        "<key>": "<string>" # 选填; string
      timeout: "<string>" # 选填; string
      url: "<string>" # 选填; string
```


## Telemetry

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: "<name>"
  spec:
    attributes: # 选填; object
      "<key>": "<string>" # 选填; string
    otlp: # 必填; object
      endpoint: "<string>" # 必填; string
      insecure: true # 选填; boolean
    serviceNamespace: "<string>" # 选填; string
    signals: # 选填; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)


## ObservabilityPipeline

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ObservabilityPipeline
  metadata:
    name: "<name>"
  spec:
    attributes: # 选填; object
      "<key>": "<string>" # 选填; string
    logs: # 选填; object
      enabled: true # 选填; boolean
      sinks: # 选填; array
        - # object
          enabled: true # 选填; boolean
          kafka: # 选填; object
            brokers: # 选填; array
              - "<string>" # string
            topic: "<string>" # 选填; string
          labels: # 选填; object
            "<key>": "<string>" # 选填; string
          loki: # 选填; object
            headers: # 选填; object
              "<key>": "<string>" # 选填; string
            tenant: "<string>" # 选填; string
            url: "<string>" # 选填; string
          minLevel: "debug" # 选填; string; enum=["debug", "info", "warning", "error"]
          name: "<string>" # 选填; string
          syslog: # 选填; object
            address: "<string>" # 选填; string
            facility: "kern" # 选填; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
            network: "" # 选填; string; enum=["", "unix", "unixgram", "tcp", "udp"]
            tag: "<string>" # 选填; string
          type: "stdout" # 必填; string; enum=["stdout", "syslog", "loki", "kafka"]
    otlp: # 选填; object
      endpoint: "<string>" # 选填; string
      headers: # 选填; object
        "<key>": "<string>" # 选填; string
      insecure: true # 选填; boolean
      tls: # 选填; object
        caFile: "<string>" # 选填; string
        certFile: "<string>" # 选填; string
        insecureSkipVerify: true # 选填; boolean
        keyFile: "<string>" # 选填; string
    sampling: # 选填; object
      rate: 0 # 选填; number
    serviceNamespace: "<string>" # 选填; string
    signals: # 选填; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)


## LogRetention

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogRetention
  metadata:
    name: "<name>"
  spec:
    retention: "<string>" # 必填; string
    schedule: "" # 选填; string; enum=["", "daily"]
    signals: # 选填; array
      - "events" # string; enum=["events", "accessLogs", "pluginRuns", "dnsQueries", "trafficFlows", "firewallEvents", "dhcpFingerprints"]
    sinks: # 选填; array
      - "<string>" # string
    vacuum: true # 选填; boolean
```


## Sysctl

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Sysctl
  metadata:
    name: "<name>"
  spec:
    compare: "" # 选填; string; enum=["", "exact", "atLeast"]
    expectedValue: "<string>" # 选填; string
    key: "<string>" # 必填; string
    optional: true # 选填; boolean
    persistent: true # 选填; boolean
    runtime: true # 选填; boolean
    value: "<string>" # 必填; string
```


## SysctlProfile

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: SysctlProfile
  metadata:
    name: "<name>"
  spec:
    overrides: # 选填; object
      "<key>": "<string>" # 选填; string
    persistent: true # 选填; boolean
    profile: "router-linux" # 必填; string; enum=["router-linux", "router-freebsd"]
    runtime: true # 选填; boolean
```


## Package

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Package
  metadata:
    name: "<name>"
  spec:
    packages: # 必填; array
      - # object
        manager: "" # 选填; string; enum=["", "apt", "dnf", "pkg"]
        names: # 必填; array
          - "<string>" # string
        optional: true # 选填; boolean
        os: "ubuntu" # 必填; string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"]
    state: "" # 选填; string; enum=["", "present"]
```


## NTPClient

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata:
    name: "<name>"
  spec:
    fallbackServers: # 选填; array
      - "<string>" # string
    interface: "<string>" # 选填; string
    managed: true # 选填; boolean
    provider: "systemd-timesyncd" # 选填; string; enum=["systemd-timesyncd", "chrony", "ntpd"]
    serverFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    servers: # 选填; array
      - "<string>" # string
    source: "static" # 选填; string; enum=["static", "auto", "dhcp", "dhcpv6"]
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)


## NTPServer

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPServer
  metadata:
    name: "<name>"
  spec:
    allowCIDRFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    allowCIDRs: # 选填; array
      - "<string>" # string
    fallbackServers: # 选填; array
      - "<string>" # string
    listenAddressFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    listenAddresses: # 选填; array
      - "<string>" # string
    managed: true # 选填; boolean
    provider: "chrony" # 选填; string; enum=["chrony", "ntpd"]
    serverFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    servers: # 选填; array
      - "<string>" # string
    source: "static" # 选填; string; enum=["static", "auto", "dhcp", "dhcpv6"]
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```


## WebConsole

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    basePath: "<string>" # 选填; string
    enabled: true # 选填; boolean
    links: # 选填; array
      - # object
        description: "<string>" # 选填; string
        label: "<string>" # 必填; string
        url: "<string>" # 必填; string
    listenAddress: "<string>" # 选填; string
    listenAddressFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    port: 1 # 选填; integer; minimum=1; maximum=65535
    title: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## ControlAPI

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    allowCIDRs: # 选填; array
      - "<string>" # string
    enabled: true # 选填; boolean
    listenAddress: "<string>" # 选填; string
    port: 1 # 选填; integer; minimum=1; maximum=65535
    tls: # 选填; object
      certFile: "<string>" # 选填; string
      clientCAFile: "<string>" # 选填; string
      keyFile: "<string>" # 选填; string
    tokenFrom: # 选填; object
      base64: true # 选填; boolean
      env: "<string>" # 选填; string
      file: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)


## RouterdCluster

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    identity: "<string>" # 选填; string
    leasePath: "<string>" # 选填; string
    leaseTTL: "<string>" # 选填; string
    peers: # 必填; array
      - "<string>" # string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)


## Interface

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    adminUp: true # 选填; boolean
    ifname: "<string>" # 必填; string
    managed: true # 必填; boolean
    mtu: 576 # 选填; integer; minimum=576; maximum=9216
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)


## PPPoESession

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    acName: "<string>" # 选填; string
    authMethod: "chap" # 选填; string; enum=["chap", "pap", "both"]
    defaultRoute: true # 选填; boolean
    enabled: true # 选填; boolean
    ifname: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    ipv6: true # 选填; boolean
    lcpEchoFailure: 0 # 选填; integer; minimum=0
    lcpEchoInterval: 0 # 选填; integer; minimum=0
    lcpFailure: 0 # 选填; integer; minimum=0
    lcpInterval: 0 # 选填; integer; minimum=0
    managed: true # 选填; boolean
    mru: 576 # 选填; integer; minimum=576; maximum=1500
    mtu: 576 # 选填; integer; minimum=576; maximum=1500
    password: "<string>" # 选填; string
    passwordFile: "<string>" # 选填; string
    persist: true # 选填; boolean
    secretEncoding: "plain" # 选填; string; enum=["plain"]
    serviceName: "<string>" # 选填; string
    usePeerDNS: true # 选填; boolean
    username: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)


## WireGuardInterface

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardInterface
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # 选填; string
    listenPort: 1 # 选填; integer; minimum=1; maximum=65535
    mtu: 576 # 选填; integer; minimum=576; maximum=9216
    peersFrom: # 选填; array
      - # object
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    privateKey: "<string>" # 选填; string
    privateKeyFile: "<string>" # 选填; string
    selfNodeRef: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## WireGuardPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs: # 必填; array
      - "<string>" # string
    endpoint: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    persistentKeepalive: 0 # 选填; integer; minimum=0; maximum=65535
    presharedKey: "<string>" # 选填; string
    presharedKeyFile: "<string>" # 选填; string
    publicKey: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)


## TailscaleNode

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TailscaleNode
  metadata:
    name: "<name>"
  spec:
    acceptDNS: true # 选填; boolean
    acceptRoutes: true # 选填; boolean
    advertiseExitNode: true # 选填; boolean
    advertiseRoutes: # 选填; array
      - "<string>" # string
    advertiseTags: # 选填; array
      - "<string>" # string
    authKey: "<string>" # 选填; string
    authKeyEnv: "<string>" # 选填; string
    authKeyFile: "<string>" # 选填; string
    hostname: "<string>" # 选填; string
    loginServer: "<string>" # 选填; string
    shieldsUp: true # 选填; boolean
    ssh: true # 选填; boolean
    state: "" # 选填; string; enum=["", "present", "absent"]
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/tailscale-exit-subnet.yaml)


## IPsecConnection

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    certificateRef: "<string>" # 选填; string
    cloudProviderHint: "" # 选填; string; enum=["", "aws", "azure", "gcp"]
    leftSubnet: "<string>" # 必填; string
    localAddress: "<string>" # 必填; string
    phase1Proposals: # 选填; array
      - "<string>" # string
    phase2Proposals: # 选填; array
      - "<string>" # string
    preSharedKey: "<string>" # 选填; string
    remoteAddress: "<string>" # 必填; string
    rightSubnet: "<string>" # 必填; string
```


## VRF

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VRF
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # 选填; string
    members: # 选填; array
      - "<string>" # string
    routeTable: 1 # 必填; integer; minimum=1; maximum=4294967295
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/multi-instance-bgp.yaml)


## VXLANTunnel

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: "<name>"
  spec:
    bridge: "<string>" # 选填; string
    ifname: "<string>" # 选填; string
    localAddress: "<string>" # 必填; string
    mtu: 0 # 选填; integer; minimum=0
    outerDF: "" # 选填; string; enum=["", "inherit", "set", "unset"]
    peers: # 选填; array
      - "<string>" # string
    tcpMSSClamp: true # 选填; boolean
    udpPort: 1 # 选填; integer; minimum=1; maximum=65535
    underlayInterface: "<string>" # 必填; string
    vni: 1 # 必填; integer; minimum=1; maximum=16777215
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```


## IPv4StaticAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必填; string
    allowOverlap: true # 选填; boolean
    allowOverlapReason: "<string>" # 选填; string
    exclusive: true # 选填; boolean
    interface: "<string>" # 必填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/basic-static.yaml)


## VirtualAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必填; string
    addressFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    externalDNS: true # 选填; boolean
    family: "ipv4" # 必填; string; enum=["ipv4", "ipv6"]
    gratuitousARP: true # 选填; boolean
    hostname: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    mode: "" # 选填; string; enum=["", "static", "vrrp"]
    track: # 选填; array
      - # object
        confirmConsecutiveHealthy: 1 # 选填; integer; minimum=1; maximum=255
        confirmConsecutiveUnhealthy: 1 # 选填; integer; minimum=1; maximum=255
        resource: "<string>" # 必填; string
        unhealthyPenalty: 0 # 选填; integer; minimum=0; maximum=254
    vrrp: # 选填; object
      additionalFailoverVMACs: # 选填; array
        - # object
          interface: "<string>" # 必填; string
          linkLocalAddress: "<string>" # 选填; string
          macAddress: "<string>" # 必填; string
          parentInterface: "<string>" # 必填; string
          withdrawRouterAdvertisement: true # 选填; boolean
      authentication: "<string>" # 选填; string
      authenticationFrom: # 选填; object
        base64: true # 选填; boolean
        env: "<string>" # 选填; string
        file: "<string>" # 选填; string
      failoverVMAC: # 选填; object
        interface: "<string>" # 必填; string
        linkLocalAddress: "<string>" # 选填; string
        macAddress: "<string>" # 必填; string
        parentInterface: "<string>" # 必填; string
        withdrawRouterAdvertisement: true # 选填; boolean
      gracefulActivation: # 选填; object
        readyWhen: # 必填; object
          state: # 选填; object
            "<key>": # 选填; object
              contains: "<string>" # 选填; string
              equals: "<string>" # 选填; string
              exists: true # 选填; boolean
              for: "<string>" # 选填; string
              in: # 选填; array
                - "<string>" # string
              maxAge: "<string>" # 选填; string
              status: "set" # 选填; string; enum=["set", "unset", "unknown"]
        timeout: "<string>" # 选填; string
      peers: # 选填; array
        - "<string>" # string
      preempt: true # 选填; boolean
      priority: 1 # 选填; integer; minimum=1; maximum=254
      virtualRouterID: 1 # 必填; integer; minimum=1; maximum=255
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## BGPRouter

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: 1 # 必填; integer; minimum=1
    backend: "" # 选填; string; enum=["", "gobgp"]
    communities: # 选填; object
      accept: # 选填; array
        - "<string>" # string
      send: "" # 选填; string; enum=["", "standard", "extended", "both"]
      set: # 选填; object
        in: # 选填; array
          - "<string>" # string
        out: # 选填; array
          - "<string>" # string
    convergenceProfile: "" # 选填; string; enum=["", "default", "fast", "stable"]
    exportPolicy: # 选填; object
      allowedPrefixes: # 选填; array
        - "<string>" # string
    gracefulRestart: # 选填; object
      enabled: true # 选填; boolean
      restartTime: "<string>" # 选填; string
      stalePathTime: "<string>" # 选填; string
    importPolicy: # 选填; object
      allowedPrefixLengthMax: 0 # 选填; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 选填; integer; minimum=0; maximum=128
      allowedPrefixes: # 选填; array
        - "<string>" # string
      forbiddenCommunities: # 选填; array
        - "<string>" # string
      localPreference: 0 # 选填; integer
      nextHopRewrite: "" # 选填; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 选填; array
        - "<string>" # string
    listen: # 选填; object
      address: "<string>" # 选填; string
      port: 1 # 选填; integer; minimum=1; maximum=65535
    redistribute: # 选填; object
      connected: # 选填; object
        allowedPrefixes: # 选填; array
          - "<string>" # string
      static: # 选填; object
        allowedPrefixes: # 选填; array
          - "<string>" # string
    routerID: "<string>" # 必填; string
    timers: # 选填; object
      profile: "" # 选填; string; enum=["", "default", "fast", "slow"]
    vrf: "<string>" # 选填; string
    watcher: # 选填; object
      maxPrefixes: 1 # 选填; integer; minimum=1; maximum=999999
      peerStateChangeThrottle: "<string>" # 选填; string
      pollInterval: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## BGPPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPPeer
  metadata:
    name: "<name>"
  spec:
    bfd: "<string>" # 选填; string
    communities: # 选填; object
      accept: # 选填; array
        - "<string>" # string
      send: "" # 选填; string; enum=["", "standard", "extended", "both"]
      set: # 选填; object
        in: # 选填; array
          - "<string>" # string
        out: # 选填; array
          - "<string>" # string
    convergenceProfile: "" # 选填; string; enum=["", "default", "fast", "stable"]
    ebgpMultihop: 0 # 选填; integer; minimum=0; maximum=255
    exportPolicy: # 选填; object
      allowedPrefixes: # 选填; array
        - "<string>" # string
    importPolicy: # 选填; object
      allowedPrefixLengthMax: 0 # 选填; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 选填; integer; minimum=0; maximum=128
      allowedPrefixes: # 选填; array
        - "<string>" # string
      forbiddenCommunities: # 选填; array
        - "<string>" # string
      localPreference: 0 # 选填; integer
      nextHopRewrite: "" # 选填; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 选填; array
        - "<string>" # string
    passiveMode: true # 选填; boolean
    password: "<string>" # 选填; string
    passwordFrom: # 选填; object
      base64: true # 选填; boolean
      env: "<string>" # 选填; string
      file: "<string>" # 选填; string
    peerASN: 1 # 必填; integer; minimum=1
    peers: # 选填; array
      - "<string>" # string
    routeReflectorClient: true # 选填; boolean
    routeReflectorClusterID: "<string>" # 选填; string
    routerRef: "<string>" # 必填; string
    timers: # 选填; object
      profile: "" # 选填; string; enum=["", "default", "fast", "slow"]
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## BGPDynamicPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPDynamicPeer
  metadata:
    name: "<name>"
  spec:
    ebgpMultihop: 0 # 选填; integer; minimum=0; maximum=255
    exportPolicy: # 选填; object
      allowedPrefixes: # 选填; array
        - "<string>" # string
    importPolicy: # 选填; object
      allowedPrefixLengthMax: 0 # 选填; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # 选填; integer; minimum=0; maximum=128
      allowedPrefixes: # 选填; array
        - "<string>" # string
      forbiddenCommunities: # 选填; array
        - "<string>" # string
      localPreference: 0 # 选填; integer
      nextHopRewrite: "" # 选填; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # 选填; array
        - "<string>" # string
    listen: # 必填; object
      sourcePrefixes: # 必填; array
        - "<string>" # string
    password: "<string>" # 选填; string
    passwordFrom: # 选填; object
      base64: true # 选填; boolean
      env: "<string>" # 选填; string
      file: "<string>" # 选填; string
    peerASN: 1 # 必填; integer; minimum=1
    routeReflectorClient: true # 选填; boolean
    routeReflectorClusterID: "<string>" # 选填; string
    routerRef: "<string>" # 必填; string
    timers: # 选填; object
      profile: "" # 选填; string; enum=["", "default", "fast", "slow"]
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## BFD

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BFD
  metadata:
    name: "<name>"
  spec:
    detectMultiplier: 1 # 选填; integer; minimum=1; maximum=50
    interface: "<string>" # 选填; string
    minRx: "<string>" # 选填; string
    minTx: "<string>" # 选填; string
    peer: "<string>" # 必填; string
    profile: "" # 选填; string; enum=["", "fast", "normal", "slow"]
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## DHCPv4Client

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: "<name>"
  spec:
    classID: "<string>" # 选填; string
    clientID: "<string>" # 选填; string
    hostname: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    requestedAddress: "<string>" # 选填; string
    routeMetric: 0 # 选填; integer; minimum=0
    useDNS: true # 选填; boolean
    useRoutes: true # 选填; boolean
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)


## ClusterNetworkRoute

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ClusterNetworkRoute
  metadata:
    name: "<name>"
  spec:
    pods: # 选填; object
      cidrs: # 选填; array
        - "<string>" # string
    services: # 选填; object
      cidrs: # 选填; array
        - "<string>" # string
    via: # 必填; array
      - # object
        interface: "<string>" # 必填; string
        nextHop: "<string>" # 必填; string
        weight: 0 # 选填; integer; minimum=0; maximum=999
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/k8s-cluster-routes.yaml)


## DHCPv4Server

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 选填; object
      end: "<string>" # 选填; string
      leaseTime: "<string>" # 选填; string
      start: "<string>" # 选填; string
    authoritative: true # 选填; boolean
    dns: # 选填; object
      cacheSize: 0 # 选填; integer; minimum=0
      enabled: true # 选填; boolean
      upstreamInterface: "<string>" # 选填; string
      upstreamServers: # 选填; array
        - "<string>" # string
      upstreamSource: "dhcpv4" # 选填; string; enum=["dhcpv4", "static", "system", "none"]
    dnsInterface: "<string>" # 选填; string
    dnsServerFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    dnsServers: # 选填; array
      - "<string>" # string
    dnsSource: "dhcpv4" # 选填; string; enum=["dhcpv4", "static", "self", "none"]
    domain: "<string>" # 选填; string
    domainFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    gateway: "<string>" # 选填; string
    gatewayFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    interface: "<string>" # 选填; string
    leaseFile: "<string>" # 选填; string
    leaseTime: "<string>" # 选填; string
    listenInterfaces: # 选填; array
      - "<string>" # string
    logDHCP: true # 选填; boolean
    managed: true # 选填; boolean
    ntpServerFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    ntpServers: # 选填; array
      - "<string>" # string
    options: # 选填; array
      - # object
        code: 1 # 选填; integer; minimum=1; maximum=65535
        name: "<string>" # 选填; string
        value: "<string>" # 选填; string
    profiles: # 选填; array
      - # object
        addressPool: # 必填; object
          end: "<string>" # 选填; string
          leaseTime: "<string>" # 选填; string
          start: "<string>" # 选填; string
        dnsServers: # 选填; array
          - "<string>" # string
        gateway: "<string>" # 选填; string
        name: "<string>" # 必填; string
        netmask: "<string>" # 选填; string
        ntpServers: # 选填; array
          - "<string>" # string
        options: # 选填; array
          - # object
            code: 1 # 选填; integer; minimum=1; maximum=65535
            name: "<string>" # 选填; string
            value: "<string>" # 选填; string
    rangeEnd: "<string>" # 选填; string
    rangeStart: "<string>" # 选填; string
    role: "server" # 选填; string; enum=["server", "transit"]
    router: "<string>" # 选填; string
    routerSource: "interfaceAddress" # 选填; string; enum=["interfaceAddress", "static", "none"]
    scopes: # 选填; array
      - # object
        addressPool: # 必填; object
          end: "<string>" # 选填; string
          leaseTime: "<string>" # 选填; string
          start: "<string>" # 选填; string
        dnsServers: # 选填; array
          - "<string>" # string
        gateway: "<string>" # 选填; string
        match: # 必填; object
          macAddresses: # 选填; array
            - "<string>" # string
          ouiPrefixes: # 选填; array
            - "<string>" # string
        name: "<string>" # 必填; string
        netmask: "<string>" # 选填; string
        ntpServers: # 选填; array
          - "<string>" # string
        options: # 选填; array
          - # object
            code: 1 # 选填; integer; minimum=1; maximum=65535
            name: "<string>" # 选填; string
            value: "<string>" # 选填; string
        profileRef: "<string>" # 选填; string
    server: "dnsmasq" # 选填; string; enum=["dnsmasq", "kea", "dhcpd"]
    stickyHoldDays: 0 # 选填; integer; minimum=0
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## DHCPv4Reservation

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # 选填; string
    ipAddress: "<string>" # 必填; string
    leaseTime: "<string>" # 选填; string
    macAddress: "<string>" # 必填; string
    options: # 选填; array
      - # object
        code: 1 # 选填; integer; minimum=1; maximum=65535
        name: "<string>" # 选填; string
        value: "<string>" # 选填; string
    scope: "<string>" # 选填; string
    server: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)


## DHCPv6Address

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Address
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    required: true # 选填; boolean
```


## IPv6RAAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RAAddress
  metadata:
    name: "<name>"
  spec:
    interface: "<string>" # 必填; string
    managed: true # 选填; boolean
    required: true # 选填; boolean
```


## DHCPv6PrefixDelegation

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # 选填; string
    clientDUID: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    prefixLength: 1 # 选填; integer; minimum=1; maximum=128
    profile: "default" # 选填; string; enum=["default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd"]
    required: true # 选填; boolean
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## IPv6DelegatedAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: "<name>"
  spec:
    addressSuffix: "<string>" # 必填; string
    announce: true # 选填; boolean
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    prefixDelegation: "<string>" # 必填; string
    prefixLength: 1 # 选填; integer; minimum=1; maximum=128
    sendRA: true # 选填; boolean
    subnetID: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DHCPv6Information

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    request: # 选填; array
      - "<string>" # string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```


## IPv6RouterAdvertisement

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: "<name>"
  spec:
    configPath: "<string>" # 选填; string
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    dnssl: # 选填; array
      - "<string>" # string
    dnsslFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    mFlag: true # 选填; boolean
    mtu: 1280 # 选填; integer; minimum=1280; maximum=65535
    oFlag: true # 选填; boolean
    pidFile: "<string>" # 选填; string
    preferredLifetime: "<string>" # 选填; string
    prefix: "<string>" # 选填; string
    prefixFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    prfPreference: "" # 选填; string; enum=["", "low", "medium", "high"]
    rdnss: # 选填; array
      - "<string>" # string
    rdnssFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    validLifetime: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DHCPv6Server

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 选填; object
      end: "<string>" # 选填; string
      leaseTime: "<string>" # 选填; string
      start: "<string>" # 选填; string
    configPath: "<string>" # 选填; string
    defaultRoute: true # 选填; boolean
    delegatedAddress: "<string>" # 选填; string
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    dnsServerFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    dnsServers: # 选填; array
      - "<string>" # string
    dnsSource: "self" # 选填; string; enum=["self", "static", "none"]
    domainSearch: # 选填; array
      - "<string>" # string
    domainSearchFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    interface: "<string>" # 选填; string
    leaseFile: "<string>" # 选填; string
    leaseTime: "<string>" # 选填; string
    listenInterfaces: # 选填; array
      - "<string>" # string
    logDHCP: true # 选填; boolean
    managed: true # 选填; boolean
    mode: "stateless" # 选填; string; enum=["stateless", "stateful", "both", "ra-only"]
    pidFile: "<string>" # 选填; string
    rapidCommit: true # 选填; boolean
    role: "server" # 选填; string; enum=["server", "transit"]
    selfAddressPolicy: "<string>" # 选填; string
    server: "dnsmasq" # 选填; string; enum=["dnsmasq"]
    sntpServerFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    sntpServers: # 选填; array
      - "<string>" # string
    stickyHoldDays: 0 # 选填; integer; minimum=0
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## DHCPv4ServerLeaseSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # 选填; string
    interval: "<string>" # 选填; string
    source: # 选填; object
      resource: "<string>" # 必填; string
    targets: # 选填; array
      - # object
        host: "<string>" # 必填; string
        name: "<string>" # 选填; string
        options: # 选填; array
          - "<string>" # string
        sshOptions: # 选填; array
          - "<string>" # string
        user: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## DHCPv6ServerLeaseSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # 选填; string
    interval: "<string>" # 选填; string
    source: # 选填; object
      resource: "<string>" # 必填; string
    targets: # 选填; array
      - # object
        host: "<string>" # 必填; string
        name: "<string>" # 选填; string
        options: # 选填; array
          - "<string>" # string
        sshOptions: # 选填; array
          - "<string>" # string
        user: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```


## DHCPv6PrefixDelegationLeaseSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegationLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # 选填; string
    interval: "<string>" # 选填; string
    source: # 选填; object
      resource: "<string>" # 必填; string
    targets: # 选填; array
      - # object
        host: "<string>" # 必填; string
        name: "<string>" # 选填; string
        options: # 选填; array
          - "<string>" # string
        sshOptions: # 选填; array
          - "<string>" # string
        user: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```


## DHCPv4Relay

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

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

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: SelfAddressPolicy
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # 必填; string; enum=["ipv4", "ipv6"]
    candidates: # 必填; array
      - # object
        address: "<string>" # 选填; string
        addressSuffix: "<string>" # 选填; string
        delegatedAddress: "<string>" # 选填; string
        interface: "<string>" # 选填; string
        matchSuffix: "<string>" # 选填; string
        ordinal: 1 # 选填; integer; minimum=1
        source: "delegatedAddress" # 必填; string; enum=["delegatedAddress", "interfaceAddress", "static"]
```


## DNSZone

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: "<name>"
  spec:
    dhcpDerived: # 选填; object
      ddns: true # 选填; boolean
      hostnameSuffix: "<string>" # 选填; string
      leaseFile: "<string>" # 选填; string
      sources: # 选填; array
        - "<string>" # string
      ttl: 0 # 选填; integer; minimum=0
    dnssec: # 选填; object
      enabled: true # 选填; boolean
      mode: "" # 选填; string; enum=["", "validate", "sign"]
    records: # 选填; array
      - # object
        hostname: "<string>" # 必填; string
        ipv4: "<string>" # 选填; string
        ipv4From: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        ipv6: "<string>" # 选填; string
        ipv6From: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        ttl: 0 # 选填; integer; minimum=0
    reverseZones: # 选填; array
      - # object
        name: "<string>" # 必填; string
    ttl: 0 # 选填; integer; minimum=0
    zone: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSResolver

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    cache: # 选填; object
      enabled: true # 选填; boolean
      maxEntries: 0 # 选填; integer; minimum=0
      maxTTL: "<string>" # 选填; string
      minTTL: "<string>" # 选填; string
      negativeTTL: "<string>" # 选填; string
    listen: # 必填; array
      - # object
        addressFrom: # 选填; array
          - # object
            field: "<string>" # 选填; string
            optional: true # 选填; boolean
            resource: "<string>" # 必填; string
        addresses: # 选填; array
          - "<string>" # string
        name: "<string>" # 选填; string
        port: 1 # 选填; integer; minimum=1; maximum=65535
        sources: # 选填; array
          - "<string>" # string
    metrics: # 选填; object
      perUpstream: true # 选填; boolean
    queryLog: # 选填; object
      enabled: true # 选填; boolean
      path: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSForwarder

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    dnssecValidate: true # 选填; boolean
    healthcheck: # 选填; object
      failThreshold: 1 # 选填; integer; minimum=1
      interval: "<string>" # 选填; string
      passThreshold: 1 # 选填; integer; minimum=1
      timeout: "<string>" # 选填; string
    match: # 必填; array
      - "<string>" # string
    queryTimeout: "<string>" # 选填; string
    resolver: "<string>" # 必填; string
    upstreams: # 选填; array
      - "<string>" # string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
    zoneRefs: # 选填; array
      - "<string>" # string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSUpstream

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 选填; string
    addressFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    bootstrap: # 选填; array
      - "<string>" # string
    bootstrapFrom: # 选填; array
      - # object
        field: "<string>" # 必填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    path: "<string>" # 选填; string
    port: 1 # 选填; integer; minimum=1; maximum=65535
    protocol: "udp" # 必填; string; enum=["udp", "tcp", "dot", "doh"]
    sourceInterface: "<string>" # 选填; string
    tlsName: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DSLiteTunnel

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    aftrAddressOrdinal: 1 # 选填; integer; minimum=1
    aftrAddressSelection: "ordinal" # 选填; string; enum=["ordinal", "ordinalModulo"]
    aftrDNSServers: # 选填; array
      - "<string>" # string
    aftrFQDN: "<string>" # 选填; string
    aftrFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    aftrIPv6: "<string>" # 选填; string
    defaultRoute: true # 选填; boolean
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    enabled: true # 选填; boolean
    encapsulationLimit: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    localAddress: "<string>" # 选填; string
    localAddressFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    localAddressSource: "interface" # 选填; string; enum=["interface", "static", "delegatedAddress"]
    localAddressSuffix: "<string>" # 选填; string
    localDelegatedAddress: "<string>" # 选填; string
    mtu: 1280 # 选填; integer; minimum=1280; maximum=65535
    remoteAddress: "<string>" # 选填; string
    routeMetric: 0 # 选填; integer; minimum=0
    tunnelName: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## TunnelInterface

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 选填; string
    encapDport: 1 # 选填; integer; minimum=1; maximum=65535
    encapSport: 1 # 选填; integer; minimum=1; maximum=65535
    key: 0 # 选填; integer; minimum=0; maximum=4294967295
    local: "<string>" # 选填; string
    localFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    mode: "ipip" # 必填; string; enum=["ipip", "gre", "fou", "gue"]
    mtu: 576 # 选填; integer; minimum=576; maximum=9216
    pathMTU: # 选填; object
      forceFragmentIPv4: true # 选填; boolean
    peerAddress: "<string>" # 选填; string
    remote: "<string>" # 选填; string
    remoteFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    trustedUnderlay: true # 必填; boolean
    ttl: 1 # 选填; integer; minimum=1; maximum=255
    underlayInterface: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## OverlayPeer

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: "<string>" # 必填; string
    pathMTU: # 选填; object
      forceFragmentIPv4: true # 选填; boolean
    remote: # 选填; object
      address: "<string>" # 选填; string
      nodeID: "<string>" # 选填; string
    role: "onprem" # 必填; string; enum=["onprem", "cloud"]
    underlay: # 必填; object
      address: "<string>" # 选填; string
      interface: "<string>" # 选填; string
      type: "wireguard" # 必填; string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## HybridRoute

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: HybridRoute
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # 必填; array
      - "<string>" # string
    healthCheckRef: "<string>" # 选填; string
    install: # 选填; object
      metric: 0 # 选填; integer; minimum=0
      table: "" # 选填; string; enum=["", "main"]
    peerRef: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## CloudProviderProfile

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: CloudProviderProfile
  metadata:
    name: "<name>"
  spec:
    auth: # 必填; object
      command: "<string>" # 选填; string
      mode: "external-command" # 必填; string; enum=["external-command"]
    capabilities: # 必填; array
      - "<string>" # string
    provider: "azure" # 必填; string; enum=["azure", "aws", "oci", "gcp"]
    resourceGroup: "<string>" # 选填; string
    subscriptionID: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## ProviderActionPolicy

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    allowUndo: true # 选填; boolean
    allowedActions: # 选填; array
      - "<string>" # string
    allowedCIDRs: # 选填; array
      - "<string>" # string
    allowedProviderRefs: # 选填; array
      - "<string>" # string
    allowedProviders: # 选填; array
      - "<string>" # string
    dryRunOnly: true # 选填; boolean
    enabled: true # 选填; boolean
    executionWindow: "<string>" # 选填; string
    maxActionsPerRun: 0 # 选填; integer
    requireApproval: true # 选填; boolean
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventGroup

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    auth: # 选填; object
      mode: "" # 选填; string; enum=["", "hmac"]
      secretFile: "<string>" # 选填; string
      secretRef: "<string>" # 选填; string
    listen: # 选填; object
      address: "<string>" # 选填; string
      port: 0 # 选填; integer; minimum=0; maximum=65535
    nodeName: "<string>" # 必填; string
    peersFrom: # 选填; array
      - # object
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    replayWindow: "<string>" # 选填; string
    retention: # 选填; object
      maxAge: "<string>" # 选填; string
      maxEvents: 0 # 选填; integer; minimum=0
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventPeer

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    direction: "push" # 选填; string; enum=["push"]
    endpoint: "<string>" # 选填; string
    groupRef: "<string>" # 必填; string
    nodeName: "<string>" # 必填; string
    subjectPrefixes: # 选填; array
      - "<string>" # string
    types: # 选填; array
      - "<string>" # string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventSubscription

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventSubscription
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必填; string
    match: # 必填; object
      payload: # 选填; object
        "<key>": "<string>" # 选填; string
      sourceNodes: # 选填; array
        - "<string>" # string
      subjectPrefixes: # 选填; array
        - "<string>" # string
      types: # 必填; array
        - "<string>" # string
    trigger: # 必填; object
      batchWindow: "<string>" # 选填; string
      debounce: "<string>" # 选填; string
      pluginRef: "<string>" # 必填; string
```


## FederationSLO

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    delivery: # 选填; object
      expiresSoonSeconds: 0 # 选填; integer; minimum=0
      lagFailSeconds: 0 # 选填; integer; minimum=0
      lagWarnSeconds: 0 # 选填; integer; minimum=0
    groupRef: "<string>" # 必填; string
    subscription: # 选填; object
      maxFailedRuns: 0 # 选填; integer; minimum=0
      maxPendingRuns: 0 # 选填; integer; minimum=0
```


## MobilityPool

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必填; string
    members: # 选填; array
      - # object
        capture: # 选填; object
          activeWhen: # 选填; object
            type: "" # 选填; string; enum=["", "single-router", "vrrp-master"]
            virtualAddressRef: "<string>" # 选填; string
          captureStrategy: "" # 选填; string; enum=["", "route-table"]
          excludeAddresses: # 选填; array
            - "<string>" # string
          gratuitousARP: true # 选填; boolean
          interface: "<string>" # 选填; string
          nicRef: "<string>" # 选填; string
          providerRef: "<string>" # 选填; string
          sourceAddress: "<string>" # 选填; string
          sourceAddressFrom: # 选填; object
            field: "<string>" # 选填; string
            optional: true # 选填; boolean
            resource: "<string>" # 必填; string
          target: # 选填; object
            "<key>": "<string>" # 选填; string
          targetFrom: # 选填; object
            "<key>": "<string>" # 选填; string
          type: "provider-secondary-ip" # 选填; string; enum=["provider-secondary-ip", "proxy-arp"]
        nodeRef: "<string>" # 必填; string
        ownershipDiscovery: # 选填; object
          allowEmptyAfter: "<string>" # 选填; string
          leaseTTL: "<string>" # 选填; string
          mode: "" # 选填; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
          pluginRef: "<string>" # 选填; string
          providerRef: "<string>" # 选填; string
          scanInterval: "<string>" # 选填; string
          scope: # 选填; object
            excludeAddresses: # 选填; array
              - "<string>" # string
            includeAddresses: # 选填; array
              - "<string>" # string
          selector: # 选填; object
            tags: # 选填; object
              "<key>": "<string>" # 选填; string
          sources: # 选填; array
            - # object
              bridge: "<string>" # 选填; string
              interface: "<string>" # 选填; string
              leaseTTL: "<string>" # 选填; string
              network: "<string>" # 选填; string
              probeRetries: 0 # 选填; integer; minimum=0; maximum=20
              probeTimeout: "<string>" # 选填; string
              resource: "<string>" # 选填; string
              scanInterval: "<string>" # 选填; string
              sourceAddressFrom: # 选填; object
                field: "<string>" # 选填; string
                optional: true # 选填; boolean
                resource: "<string>" # 必填; string
              type: "dhcpv4-lease" # 必填; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
          stoppedInstancePolicy: "" # 选填; string; enum=["", "hold", "release"]
          subnetRef: "<string>" # 选填; string
          subnetRefFrom: "<string>" # 选填; string
        profileRef: "<string>" # 选填; string
        staticOwnedAddresses: # 选填; array
          - "<string>" # string
    membersFrom: # 必填; array; minItems=1
      - # object
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    prefix: "<string>" # 必填; string
    profiles: # 选填; object
      cloudCaptures: # 选填; object
        "<key>": # 选填; object
          capture: # 选填; object
            activeWhen: # 选填; object
              type: "" # 选填; string; enum=["", "single-router", "vrrp-master"]
              virtualAddressRef: "<string>" # 选填; string
            captureStrategy: "" # 选填; string; enum=["", "route-table"]
            excludeAddresses: # 选填; array
              - "<string>" # string
            gratuitousARP: true # 选填; boolean
            interface: "<string>" # 选填; string
            nicRef: "<string>" # 选填; string
            providerRef: "<string>" # 选填; string
            sourceAddress: "<string>" # 选填; string
            sourceAddressFrom: # 选填; object
              field: "<string>" # 选填; string
              optional: true # 选填; boolean
              resource: "<string>" # 必填; string
            target: # 选填; object
              "<key>": "<string>" # 选填; string
            targetFrom: # 选填; object
              "<key>": "<string>" # 选填; string
            type: "provider-secondary-ip" # 选填; string; enum=["provider-secondary-ip", "proxy-arp"]
          ownershipDiscovery: # 选填; object
            allowEmptyAfter: "<string>" # 选填; string
            leaseTTL: "<string>" # 选填; string
            mode: "" # 选填; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
            pluginRef: "<string>" # 选填; string
            providerRef: "<string>" # 选填; string
            scanInterval: "<string>" # 选填; string
            scope: # 选填; object
              excludeAddresses: # 选填; array
                - "<string>" # string
              includeAddresses: # 选填; array
                - "<string>" # string
            selector: # 选填; object
              tags: # 选填; object
                "<key>": "<string>" # 选填; string
            sources: # 选填; array
              - # object
                bridge: "<string>" # 选填; string
                interface: "<string>" # 选填; string
                leaseTTL: "<string>" # 选填; string
                network: "<string>" # 选填; string
                probeRetries: 0 # 选填; integer; minimum=0; maximum=20
                probeTimeout: "<string>" # 选填; string
                resource: "<string>" # 选填; string
                scanInterval: "<string>" # 选填; string
                sourceAddressFrom: # 选填; object
                  field: "<string>" # 选填; string
                  optional: true # 选填; boolean
                  resource: "<string>" # 必填; string
                type: "dhcpv4-lease" # 必填; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
            stoppedInstancePolicy: "" # 选填; string; enum=["", "hold", "release"]
            subnetRef: "<string>" # 选填; string
            subnetRefFrom: "<string>" # 选填; string
    staticHandovers: # 选填; array
      - # object
        address: "<string>" # 必填; string
        fromNodeRef: "<string>" # 必填; string
        toNodeRef: "<string>" # 必填; string
    values: # 选填; object
      "<key>": "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## SAMNodeSet

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes: # 必填; array
      - # object
        eventEndpoint: "<string>" # 选填; string
        macAddresses: # 选填; array
          - "<string>" # string; pattern="^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$"
        maintenance: # 选填; object
          drain: true # 选填; boolean
        maxSecondaryIPs: 0 # 选填; integer; minimum=0
        nodeRef: "<string>" # 必填; string
        placement: # 选填; object
          group: "<string>" # 选填; string
          priority: 0 # 选填; integer; minimum=0; maximum=1000000
        role: "" # 选填; string; enum=["", "onprem", "cloud"]
        routeReflector: true # 选填; boolean
        samEndpoint: "<string>" # 选填; string
        samEndpointFrom: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        site: "<string>" # 选填; string
        wireGuard: # 选填; object
          allowedIPs: # 选填; array
            - "<string>" # string
          endpoint: "<string>" # 选填; string
          persistentKeepalive: 0 # 选填; integer; minimum=0; maximum=65535
          publicKey: "<string>" # 选填; string
          transportEndpoint: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)


## SAMEnrollmentPolicy

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentPolicy
  metadata:
    name: "<name>"
  spec:
    allowedLeafIDs: # 选填; object
      pattern: "<string>" # 选填; string
    directMesh: # 选填; object
      peerGroupRef: "<string>" # 选填; string
    endpointPrefixes: # 选填; array
      - "<string>" # string
    joinAudience: "<string>" # 选填; string
    joinTokenFrom: # 选填; object
      base64: true # 选填; boolean
      env: "<string>" # 选填; string
      file: "<string>" # 选填; string
    mobilityPoolRefs: # 选填; array
      - "<string>" # string
    mobilityPrefixes: # 选填; array
      - "<string>" # string
    rrNodeSetRef: "<string>" # 选填; string
    rrSetRef: "<string>" # 选填; string
    transportProfileRef: "<string>" # 必填; string
    ttl: "<string>" # 选填; string
    tunnelAddressPrefixes: # 必填; array
      - "<string>" # string
    wireGuard: # 选填; object
      allowedExtraIPPrefixes: # 选填; array
        - "<string>" # string
      endpointPrefixes: # 选填; array
        - "<string>" # string
      interface: "<string>" # 必填; string
      persistentKeepalive: 0 # 选填; integer; minimum=0; maximum=65535
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)


## SAMEnrollmentClaim

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClaim
  metadata:
    name: "<name>"
  spec:
    bgp: # 选填; object
      asn: 1 # 选填; integer; minimum=1
      routerID: "<string>" # 选填; string
    directMesh: true # 选填; boolean
    endpoint: "<string>" # 选填; string
    expiresAt: "<string>" # 选填; string
    joinAudience: "<string>" # 选填; string
    joinHMAC: "<string>" # 选填; string
    joinNonce: "<string>" # 选填; string
    joinTimestamp: "<string>" # 选填; string
    leafID: "<string>" # 必填; string
    mobility: # 选填; object
      ownedAddresses: # 选填; array
        - "<string>" # string
    policyRef: "<string>" # 必填; string
    revoked: true # 选填; boolean
    rrSetRef: "<string>" # 选填; string
    tunnelAddress: "<string>" # 必填; string
    wireGuard: # 选填; object
      allowedIPs: # 选填; array
        - "<string>" # string
      endpoint: "<string>" # 选填; string
      persistentKeepalive: 0 # 选填; integer; minimum=0; maximum=65535
      publicKey: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)


## SAMEnrollmentClient

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClient
  metadata:
    name: "<name>"
  spec:
    bootstrapEndpoints: # 选填; array
      - "<string>" # string
    claimRef: "<string>" # 必填; string
    controlAPITLS: # 选填; object
      caFile: "<string>" # 选填; string
      certFile: "<string>" # 选填; string
      insecureSkipVerify: true # 选填; boolean
      keyFile: "<string>" # 选填; string
      serverName: "<string>" # 选填; string
    controlAPITokenFrom: # 选填; object
      base64: true # 选填; boolean
      env: "<string>" # 选填; string
      file: "<string>" # 选填; string
    retryBackoff: # 选填; object
      max: "<string>" # 选填; string
      min: "<string>" # 选填; string
    rrSocket: "<string>" # 选填; string
    stateTTLRefreshBefore: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)


## SAMTransportProfile

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMTransportProfile
  metadata:
    name: "<name>"
  spec:
    addressingMode: "" # 选填; string; enum=["", "edge-index", "pair-stable"]
    bgp: # 必填; object
      bfd: # 选填; object
        detectMultiplier: 1 # 选填; integer; minimum=1; maximum=50
        enabled: true # 选填; boolean
        interface: "<string>" # 选填; string
        minRx: "<string>" # 选填; string
        minTx: "<string>" # 选填; string
        profile: "" # 选填; string; enum=["", "fast", "normal", "slow"]
      convergenceProfile: "" # 选填; string; enum=["", "default", "fast", "stable"]
      directLocalPreference: 0 # 选填; integer
      ebgpMultihop: 0 # 选填; integer; minimum=0; maximum=255
      exportPolicy: # 选填; object
        allowedPrefixes: # 选填; array
          - "<string>" # string
      generatePeers: true # 选填; boolean
      importPolicy: # 选填; object
        allowedPrefixLengthMax: 0 # 选填; integer; minimum=0; maximum=128
        allowedPrefixLengthMin: 0 # 选填; integer; minimum=0; maximum=128
        allowedPrefixes: # 选填; array
          - "<string>" # string
        forbiddenCommunities: # 选填; array
          - "<string>" # string
        localPreference: 0 # 选填; integer
        nextHopRewrite: "" # 选填; string; enum=["", "peer-address", "unchanged"]
        requiredCommunities: # 选填; array
          - "<string>" # string
      peerASN: 1 # 必填; integer; minimum=1
      routeReflectorClient: true # 选填; boolean
      routeReflectorClusterID: "<string>" # 选填; string
      routerRef: "<string>" # 必填; string
      timers: # 选填; object
        profile: "" # 选填; string; enum=["", "default", "fast", "slow"]
      timersPreset: "" # 选填; string; enum=["", "default", "fast", "slow"]
    encapDport: 1 # 选填; integer; minimum=1; maximum=65535
    encapSport: 1 # 选填; integer; minimum=1; maximum=65535
    encryption: "" # 选填; string; enum=["", "none", "wireguard"]
    innerPrefix: "<string>" # 必填; string
    localEndpoint: "<string>" # 选填; string
    localEndpointFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    mode: "ipip" # 必填; string; enum=["ipip", "gre", "fou", "gue"]
    peersFrom: # 选填; array
      - # object
        direct: true # 选填; boolean
        nodeRefs: # 选填; array
          - "<string>" # string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    publishPeerGroup: true # 选填; boolean
    selfNodeRef: "<string>" # 必填; string
    underlayInterface: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## SAMSubnetPolicy

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

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

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4Route
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    destination: "<string>" # 必填; string
    device: "<string>" # 选填; string
    deviceFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    gateway: "<string>" # 选填; string
    gatewayFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    metric: 0 # 选填; integer; minimum=0
    preferredSource: "<string>" # 选填; string
    type: "unicast" # 选填; string; enum=["unicast", "blackhole"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)


## HealthCheck

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # 选填; string; enum=["ipv4", "ipv6"]
    enabled: true # 选填; boolean
    healthyThreshold: 1 # 选填; integer; minimum=1
    interface: "<string>" # 选填; string
    interval: "<string>" # 选填; string
    port: 0 # 选填; integer; minimum=0; maximum=65535
    protocol: "" # 选填; string; enum=["", "icmp", "tcp", "dns", "http"]
    role: "link" # 选填; string; enum=["link", "next-hop", "internet", "service", "policy"]
    target: "<string>" # 选填; string
    targetSource: "auto" # 选填; string; enum=["auto", "static", "defaultGateway", "dsliteRemote"]
    timeout: "<string>" # 选填; string
    type: "ping" # 选填; string; enum=["ping"]
    unhealthyThreshold: 1 # 选填; integer; minimum=1
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## EgressRoutePolicy

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates: # 必填; array
      - # object
        dependsOn: # 选填; array
          - # object
            equals: "<string>" # 选填; string
            field: "<string>" # 选填; string
            notEmpty: true # 选填; boolean
            optional: true # 选填; boolean
            phase: "<string>" # 选填; string
            resource: "<string>" # 必填; string
        device: "<string>" # 选填; string
        deviceFrom: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        enabled: true # 选填; boolean
        gateway: "<string>" # 选填; string
        gatewayFrom: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        gatewaySource: "" # 选填; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
        healthCheck: "<string>" # 选填; string
        interface: "<string>" # 选填; string
        mark: 0 # 选填; integer; minimum=0; maximum=4294967295
        metric: 0 # 选填; integer; minimum=0
        name: "<string>" # 选填; string
        preferredSource: "" # 选填; string; enum=["", "interface"]
        priority: 0 # 选填; integer; minimum=0; maximum=32765
        routeMetric: 0 # 选填; integer; minimum=0
        routeTable: 0 # 选填; integer; minimum=0; maximum=4294967295
        source: "<string>" # 选填; string
        table: 0 # 选填; integer; minimum=0; maximum=4294967295
        targets: # 选填; array
          - # object
            gateway: "<string>" # 选填; string
            gatewayFrom: # 选填; object
              field: "<string>" # 选填; string
              optional: true # 选填; boolean
              resource: "<string>" # 必填; string
            gatewaySource: "" # 选填; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
            healthCheck: "<string>" # 选填; string
            interface: "<string>" # 选填; string
            mark: 0 # 选填; integer; minimum=0; maximum=4294967295
            metric: 0 # 选填; integer; minimum=0
            name: "<string>" # 选填; string
            outboundInterface: "<string>" # 选填; string
            priority: 0 # 选填; integer; minimum=0; maximum=32765
            routeMetric: 0 # 选填; integer; minimum=0
            routeTable: 0 # 选填; integer; minimum=0; maximum=4294967295
            table: 0 # 选填; integer; minimum=0; maximum=4294967295
        weight: 0 # 选填; integer; minimum=0
        when: # 选填; object
          state: # 选填; object
            "<key>": # 选填; object
              contains: "<string>" # 选填; string
              equals: "<string>" # 选填; string
              exists: true # 选填; boolean
              for: "<string>" # 选填; string
              in: # 选填; array
                - "<string>" # string
              maxAge: "<string>" # 选填; string
              status: "set" # 选填; string; enum=["set", "unset", "unknown"]
    destinationCIDRs: # 选填; array
      - "<string>" # string
    destinationSetRefs: # 选填; array
      - "<string>" # string
    excludeDestinationCIDRs: # 选填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 选填; array
      - "<string>" # string
    family: "ipv4" # 选填; string; enum=["ipv4", "ipv6"]
    hashFields: # 选填; array
      - "<string>" # string
    hostTraffic: true # 选填; boolean
    hysteresis: "<string>" # 选填; string
    mode: "priority" # 选填; string; enum=["priority", "mark", "hash"]
    selection: "highest-weight-ready" # 选填; string; enum=["highest-weight-ready", "weighted-ecmp"]
    sourceCIDRs: # 选填; array
      - "<string>" # string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## EventRule

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EventRule
  metadata:
    name: "<name>"
  spec:
    emit: # 必填; object
      attributes: # 选填; object
        "<key>": "<string>" # 选填; string
      topic: "<string>" # 必填; string
    pattern: # 必填; object
      allow_missing_correlation: true # 选填; boolean
      correlate_by: "<string>" # 选填; string
      duration: "<string>" # 选填; string
      expected: "<string>" # 选填; string
      interval: "<string>" # 选填; string
      operator: "all_of" # 必填; string; enum=["all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"]
      quiet: "<string>" # 选填; string
      rate: 0 # 选填; integer; minimum=0
      strict: true # 选填; boolean
      threshold: 0 # 选填; integer; minimum=0
      topic: "<string>" # 选填; string
      topics: # 选填; array
        - "<string>" # string
      trigger: "<string>" # 选填; string
      window: "<string>" # 选填; string
```


## DerivedEvent

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DerivedEvent
  metadata:
    name: "<name>"
  spec:
    emitInitial: true # 选填; boolean
    emitWhen: "all_true" # 选填; string; enum=["all_true", "any_true"]
    hysteresis: "<string>" # 选填; string
    inputs: # 必填; array
      - # object
        any_of: # 选填; array
          - # array
            - # object
              equals: "<string>" # 选填; string
              field: "<string>" # 选填; string
              not_empty: true # 选填; boolean
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        not_empty: true # 选填; boolean
    retractWhen: "any_false" # 选填; string; enum=["any_false", "all_false"]
    topic: "<string>" # 必填; string
```


## NAT44Rule

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # 选填; array
      - "<string>" # string
    destinationSetRefs: # 选填; array
      - "<string>" # string
    egressInterface: "<string>" # 选填; string
    egressPolicyRef: "<string>" # 选填; string
    excludeDestinationCIDRs: # 选填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 选填; array
      - "<string>" # string
    outboundInterface: "<string>" # 选填; string
    snatAddress: "<string>" # 选填; string
    snatAddressFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    sourceCIDRs: # 选填; array
      - "<string>" # string
    sourceRanges: # 选填; array
      - "<string>" # string
    translation: # 选填; object
      address: "<string>" # 选填; string
      addresses: # 选填; array
        - "<string>" # string
      portMapping: # 选填; object
        end: 1 # 选填; integer; minimum=1; maximum=65535
        start: 1 # 选填; integer; minimum=1; maximum=65535
        type: "auto" # 选填; string; enum=["auto", "preserve", "range"]
      type: "interfaceAddress" # 必填; string; enum=["interfaceAddress", "address", "pool"]
    type: "masquerade" # 选填; string; enum=["masquerade", "snat"]
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)


## NAT44FlowDNATPinhole

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44FlowDNATPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # 选填; object
      key: "" # 选填; string; enum=["", "localPort"]
    fromInterfaceRefs: # 选填; array
      - "<string>" # string
    inbound: # 必填; object
      sourcePorts: # 必填; array
        - "<string>" # string
    outbound: # 必填; object
      destinationCIDRs: # 选填; array
        - "<string>" # string
      destinationPorts: # 必填; array
        - "<string>" # string
      destinationSetRefs: # 选填; array
        - "<string>" # string
      protocol: "udp" # 必填; string; enum=["udp"]
      sourceCIDRs: # 选填; array
        - "<string>" # string
      sourceSetRef: "<string>" # 选填; string
    timeout: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```


## NAT44SessionSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: "<name>"
  spec:
    conntrackCommand: "<string>" # 选填; string
    conntrackd: # 选填; object
      ignoreIPv4: # 选填; array
        - "<string>" # string
      interface: "<string>" # 必填; string
      localAddress: "<string>" # 必填; string
      peerAddress: "<string>" # 必填; string
      port: 1 # 选填; integer; minimum=1; maximum=65535
    excludeNatRules: # 选填; array
      - "<string>" # string
    interval: "<string>" # 选填; string
    mode: "event-stream" # 选填; string; enum=["event-stream", "conntrackd"]
    natRules: # 选填; array
      - "<string>" # string
    snatAddresses: # 选填; array
      - "<string>" # string
    targets: # 选填; array
      - # object
        host: "<string>" # 必填; string
        name: "<string>" # 选填; string
        restoreCommand: # 选填; array
          - "<string>" # string
        sshOptions: # 选填; array
          - "<string>" # string
        user: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```


## ManagementAccess

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ManagementAccess
  metadata:
    name: "<name>"
  spec:
    allowSourceCIDRs: # 选填; array
      - "<string>" # string
    interfaces: # 必填; array
      - "<string>" # string
    requireWebConsoleBound: true # 选填; boolean
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## PortForward

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    hairpin: # 选填; object
      enabled: true # 选填; boolean
      interfaces: # 选填; array
        - "<string>" # string
      mode: "" # 选填; string; enum=["", "auto", "manual", "off"]
    listen: # 必填; object
      address: "<string>" # 选填; string
      addressFrom: # 选填; object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
      interface: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必填; string; enum=["tcp", "udp"]
    target: # 必填; object
      address: "<string>" # 选填; string
      addressFrom: # 选填; object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)


## IngressService

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends: # 必填; array
      - # object
        address: "<string>" # 选填; string
        addressFrom: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        name: "<string>" # 选填; string
        port: 1 # 必填; integer; minimum=1; maximum=65535
        weight: 0 # 选填; integer; minimum=0
    externalDNS: true # 选填; boolean
    hairpin: # 选填; object
      enabled: true # 选填; boolean
      interfaces: # 选填; array
        - "<string>" # string
      mode: "" # 选填; string; enum=["", "auto", "manual", "off"]
    healthCheck: # 选填; object
      expectedBody: "<string>" # 选填; string
      expectedStatus: # 选填; array
        - 0 # integer
      healthyThreshold: 1 # 选填; integer; minimum=1
      host: "<string>" # 选填; string
      interval: "<string>" # 选填; string
      path: "<string>" # 选填; string
      protocol: "" # 选填; string; enum=["", "tcp", "http", "https"]
      timeout: "<string>" # 选填; string
      tlsSkipVerify: true # 选填; boolean
      unhealthyThreshold: 1 # 选填; integer; minimum=1
    hostname: "<string>" # 选填; string
    listen: # 必填; object
      address: "<string>" # 选填; string
      addressFrom: # 选填; object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
      interface: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必填; string; enum=["tcp", "udp"]
    policy: # 选填; object
      onNoHealthyBackends: "" # 选填; string; enum=["", "drop", "reject"]
      selection: "" # 选填; string; enum=["", "failover", "sourceHash", "random"]
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)


## IPAddressSet

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPAddressSet
  metadata:
    name: "<name>"
  spec:
    addresses: # 选填; array
      - "<string>" # string
    names: # 选填; array
      - "<string>" # string
    refreshInterval: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)


## LocalServiceRedirect

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

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
        name: "<string>" # 选填; string
        protocols: # 必填; array
          - "<string>" # string
        redirectPort: 1 # 必填; integer; minimum=1; maximum=65535
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)


## FirewallFlowPinhole

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallFlowPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # 选填; object
      key: "" # 选填; string; enum=["", "remoteAddress", "localEndpoint"]
    fromZone: "<string>" # 必填; string
    inbound: # 必填; object
      sourcePorts: # 必填; array
        - "<string>" # string
    outbound: # 必填; object
      destinationCIDRs: # 选填; array
        - "<string>" # string
      destinationPorts: # 必填; array
        - "<string>" # string
      destinationSetRefs: # 选填; array
        - "<string>" # string
      protocol: "udp" # 必填; string; enum=["udp"]
      sourceCIDRs: # 选填; array
        - "<string>" # string
      sourceSetRef: "<string>" # 选填; string
    timeout: "<string>" # 选填; string
    toZone: "<string>" # 必填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```


## TrafficFlowLog

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TrafficFlowLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # 选填; boolean
    includeApplicationLayer: true # 选填; boolean
    includeTLSSNI: true # 选填; boolean
    path: "<string>" # 选填; string
    source: "" # 选填; string; enum=["", "conntrack"]
```


## FirewallZone

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

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

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)


## FirewallPolicy

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: "<name>"
  spec:
    logDeny: true # 选填; boolean
    sameRoleAccept: true # 选填; boolean
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)


## ClientPolicy

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    classification: # 选填; array
      - # object
        ipv4Reservation: "<string>" # 选填; string
        ipv6Addresses: # 选填; array
          - "<string>" # string
        match: # 必填; object
          dhcpFingerprints: # 选填; array
            - "<string>" # string
          hostnamePatterns: # 选填; array
            - "<string>" # string
          macs: # 选填; array
            - "<string>" # string
          ouiPrefixes: # 选填; array
            - "<string>" # string
        mode: "trusted" # 必填; string; enum=["trusted", "guest", "isolated"]
        name: "<string>" # 选填; string
    guestEgressAllow: # 选填; array
      - "<string>" # string
    guestEgressDeny: # 选填; array
      - "<string>" # string
    guestServices: # 选填; array
      - "<string>" # string
    interfaces: # 选填; array
      - "<string>" # string
    isolation: # 选填; object
      lanInternet: "" # 选填; string; enum=["", "allow", "deny"]
      lanLAN: "" # 选填; string; enum=["", "allow", "deny"]
      lanMgmt: "" # 选填; string; enum=["", "allow", "deny"]
      mDNSBroadcast: "" # 选填; string; enum=["", "allow", "deny"]
    macs: # 选填; array
      - "<string>" # string
    mode: "include" # 必填; string; enum=["include", "exclude"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)


## FirewallRule

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: "<name>"
  spec:
    action: "accept" # 必填; string; enum=["accept", "drop", "reject"]
    connLimit: # 选填; object
      log: true # 选填; boolean
      maxPerSource: 0 # 选填; integer; minimum=0
    destinationCIDRs: # 选填; array
      - "<string>" # string
    destinationPorts: # 选填; array
      - "<string>" # string
    destinationSetRefs: # 选填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 选填; array
      - "<string>" # string
    fromZone: "<string>" # 必填; string
    icmpType: "<string>" # 选填; string
    icmpv6Type: "<string>" # 选填; string
    log: true # 选填; boolean
    port: 0 # 选填; integer; minimum=0; maximum=65535
    protocol: "" # 选填; string; enum=["", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip"]
    rateLimit: # 选填; object
      burst: 0 # 选填; integer; minimum=0
      log: true # 选填; boolean
      packetsPerSecond: 0 # 选填; integer; minimum=0
      per: "" # 选填; string; enum=["", "second", "minute"]
      rate: 0 # 选填; integer; minimum=0
      unit: "" # 选填; string; enum=["", "packet", "byte", "kilobyte", "megabyte"]
    sourcePorts: # 选填; array
      - "<string>" # string
    srcCIDRs: # 选填; array
      - "<string>" # string
    toZone: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)


## FirewallEventLog

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # 选填; boolean
    events: # 选填; array
      - "deny" # string; enum=["deny", "allow", "rateLimit", "connLimit"]
    fromZones: # 选填; array
      - "<string>" # string
    log: # 选填; object
      acceptSampleRate: 0 # 选填; integer; minimum=0
      copyRange: 0 # 选填; integer; minimum=0
      dropSampleRate: 0 # 选填; integer; minimum=0
      reject: true # 选填; boolean
    nflogGroup: 0 # 选填; integer; minimum=0; maximum=65535
    path: "<string>" # 选填; string
    retention: "<string>" # 选填; string
    rules: # 选填; array
      - "<string>" # string
    sampleRate: 0 # 选填; integer; minimum=0
    sinks: # 选填; array
      - "<string>" # string
    toZones: # 选填; array
      - "<string>" # string
```


## Hostname

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML定义

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Hostname
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # 必填; string
    managed: true # 选填; boolean
```
