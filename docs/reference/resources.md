# Resource configuration reference

Place resources inside Router.spec.resources. Names, interfaces, addresses, credentials and peer settings in examples must be adapted. A schema-required field is required within its parent object; cross-resource and semantic constraints are checked by routerd validate, not by the schema alone.

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; only a default annotation specifies a schema default. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

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

### YAML definition

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: Plugin
  metadata:
    name: "<name>"
  spec:
    capabilities: # optional; array
      - "observe.cloud" # string; enum=["observe.cloud", "observe.providerPrivateIPs", "propose.dynamicConfig", "propose.providerAction", "execute.providerAction"]
    context: # optional; object
      resources: # optional; array
        - # object
          apiVersion: "<string>" # Required; string
          kind: "<string>" # Required; string
          name: "<string>" # Required; string
    env: # optional; object
      "<key>": "<string>" # optional; string
    executable: "<string>" # Required; string
    timeout: "<string>" # optional; string
    triggers: # optional; array
      - # object
        every: "<string>" # optional; string
        topic: "<string>" # optional; string
        type: "interval" # Required; string; enum=["interval", "event"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## DynamicConfigSource

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: plugin.routerd.net/v1alpha1
  kind: DynamicConfigSource
  metadata:
    name: "<name>"
  spec:
    mergePolicy: # optional; object
      conflict: "" # optional; string; enum=["", "reject"]
    pluginRef: "<string>" # Required; string
    triggers: # optional; array
      - # object
        every: "<string>" # optional; string
        topic: "<string>" # optional; string
        type: "interval" # Required; string; enum=["interval", "event"]
    ttl: "<string>" # optional; string
```


## DynamicOverridePolicy

`apiVersion: config.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: config.routerd.net/v1alpha1
  kind: DynamicOverridePolicy
  metadata:
    name: "<name>"
  spec:
    allow: # Required; array
      - # object
        operations: # Required; array
          - "<string>" # string
        source: "<string>" # Required; string
        targets: # Required; array
          - # object
            apiVersion: "<string>" # Required; string
            kind: "<string>" # Required; string
            name: "<string>" # Required; string
```


## LogSink

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogSink
  metadata:
    name: "<name>"
  spec:
    enabled: true # optional; boolean
    file: # optional; object
      name: "<string>" # optional; string
    journald: # optional; object
      identifier: "<string>" # optional; string
    minLevel: "debug" # optional; string; enum=["debug", "info", "warning", "error"]
    otlp: # optional; object
      endpoint: "<string>" # optional; string
      telemetryRef: "<string>" # optional; string
    syslog: # optional; object
      address: "<string>" # optional; string
      facility: "kern" # optional; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
      network: "" # optional; string; enum=["", "unix", "unixgram", "tcp", "udp"]
      tag: "<string>" # optional; string
    type: "syslog" # Required; string; enum=["syslog", "otlp", "webhook", "file", "journald"]
    webhook: # optional; object
      headers: # optional; object
        "<key>": "<string>" # optional; string
      timeout: "<string>" # optional; string
      url: "<string>" # optional; string
```


## Telemetry

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: "<name>"
  spec:
    attributes: # optional; object
      "<key>": "<string>" # optional; string
    otlp: # Required; object
      endpoint: "<string>" # Required; string
      insecure: true # optional; boolean
    serviceNamespace: "<string>" # optional; string
    signals: # optional; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)


## ObservabilityPipeline

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ObservabilityPipeline
  metadata:
    name: "<name>"
  spec:
    attributes: # optional; object
      "<key>": "<string>" # optional; string
    logs: # optional; object
      enabled: true # optional; boolean
      sinks: # optional; array
        - # object
          enabled: true # optional; boolean
          kafka: # optional; object
            brokers: # optional; array
              - "<string>" # string
            topic: "<string>" # optional; string
          labels: # optional; object
            "<key>": "<string>" # optional; string
          loki: # optional; object
            headers: # optional; object
              "<key>": "<string>" # optional; string
            tenant: "<string>" # optional; string
            url: "<string>" # optional; string
          minLevel: "debug" # optional; string; enum=["debug", "info", "warning", "error"]
          name: "<string>" # optional; string
          syslog: # optional; object
            address: "<string>" # optional; string
            facility: "kern" # optional; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
            network: "" # optional; string; enum=["", "unix", "unixgram", "tcp", "udp"]
            tag: "<string>" # optional; string
          type: "stdout" # Required; string; enum=["stdout", "syslog", "loki", "kafka"]
    otlp: # optional; object
      endpoint: "<string>" # optional; string
      headers: # optional; object
        "<key>": "<string>" # optional; string
      insecure: true # optional; boolean
      tls: # optional; object
        caFile: "<string>" # optional; string
        certFile: "<string>" # optional; string
        insecureSkipVerify: true # optional; boolean
        keyFile: "<string>" # optional; string
    sampling: # optional; object
      rate: 0 # optional; number
    serviceNamespace: "<string>" # optional; string
    signals: # optional; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)


## LogRetention

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogRetention
  metadata:
    name: "<name>"
  spec:
    retention: "<string>" # Required; string
    schedule: "" # optional; string; enum=["", "daily"]
    signals: # optional; array
      - "events" # string; enum=["events", "accessLogs", "pluginRuns", "dnsQueries", "trafficFlows", "firewallEvents", "dhcpFingerprints"]
    sinks: # optional; array
      - "<string>" # string
    vacuum: true # optional; boolean
```


## Sysctl

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Sysctl
  metadata:
    name: "<name>"
  spec:
    compare: "" # optional; string; enum=["", "exact", "atLeast"]
    expectedValue: "<string>" # optional; string
    key: "<string>" # Required; string
    optional: true # optional; boolean
    persistent: true # optional; boolean
    runtime: true # optional; boolean
    value: "<string>" # Required; string
```


## SysctlProfile

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: SysctlProfile
  metadata:
    name: "<name>"
  spec:
    overrides: # optional; object
      "<key>": "<string>" # optional; string
    persistent: true # optional; boolean
    profile: "router-linux" # Required; string; enum=["router-linux", "router-freebsd"]
    runtime: true # optional; boolean
```


## Package

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Package
  metadata:
    name: "<name>"
  spec:
    packages: # Required; array
      - # object
        manager: "" # optional; string; enum=["", "apt", "dnf", "pkg"]
        names: # Required; array
          - "<string>" # string
        optional: true # optional; boolean
        os: "ubuntu" # Required; string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"]
    state: "" # optional; string; enum=["", "present"]
```


## NTPClient

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata:
    name: "<name>"
  spec:
    fallbackServers: # optional; array
      - "<string>" # string
    interface: "<string>" # optional; string
    managed: true # optional; boolean
    provider: "systemd-timesyncd" # optional; string; enum=["systemd-timesyncd", "chrony", "ntpd"]
    serverFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    servers: # optional; array
      - "<string>" # string
    source: "static" # optional; string; enum=["static", "auto", "dhcp", "dhcpv6"]
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)


## NTPServer

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPServer
  metadata:
    name: "<name>"
  spec:
    allowCIDRFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    allowCIDRs: # optional; array
      - "<string>" # string
    fallbackServers: # optional; array
      - "<string>" # string
    listenAddressFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    listenAddresses: # optional; array
      - "<string>" # string
    managed: true # optional; boolean
    provider: "chrony" # optional; string; enum=["chrony", "ntpd"]
    serverFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    servers: # optional; array
      - "<string>" # string
    source: "static" # optional; string; enum=["static", "auto", "dhcp", "dhcpv6"]
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```


## WebConsole

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    basePath: "<string>" # optional; string
    enabled: true # optional; boolean
    links: # optional; array
      - # object
        description: "<string>" # optional; string
        label: "<string>" # Required; string
        url: "<string>" # Required; string
    listenAddress: "<string>" # optional; string
    listenAddressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    port: 1 # optional; integer; minimum=1; maximum=65535
    title: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## ControlAPI

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    allowCIDRs: # optional; array
      - "<string>" # string
    enabled: true # optional; boolean
    listenAddress: "<string>" # optional; string
    port: 1 # optional; integer; minimum=1; maximum=65535
    tls: # optional; object
      certFile: "<string>" # optional; string
      clientCAFile: "<string>" # optional; string
      keyFile: "<string>" # optional; string
    tokenFrom: # optional; object
      base64: true # optional; boolean
      env: "<string>" # optional; string
      file: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)


## RouterdCluster

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    identity: "<string>" # optional; string
    leasePath: "<string>" # optional; string
    leaseTTL: "<string>" # optional; string
    peers: # Required; array
      - "<string>" # string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)


## Interface

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    adminUp: true # optional; boolean
    ifname: "<string>" # Required; string
    managed: true # Required; boolean
    mtu: 576 # optional; integer; minimum=576; maximum=9216
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)


## PPPoESession

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    acName: "<string>" # optional; string
    authMethod: "chap" # optional; string; enum=["chap", "pap", "both"]
    defaultRoute: true # optional; boolean
    enabled: true # optional; boolean
    ifname: "<string>" # optional; string
    interface: "<string>" # Required; string
    ipv6: true # optional; boolean
    lcpEchoFailure: 0 # optional; integer; minimum=0
    lcpEchoInterval: 0 # optional; integer; minimum=0
    lcpFailure: 0 # optional; integer; minimum=0
    lcpInterval: 0 # optional; integer; minimum=0
    managed: true # optional; boolean
    mru: 576 # optional; integer; minimum=576; maximum=1500
    mtu: 576 # optional; integer; minimum=576; maximum=1500
    password: "<string>" # optional; string
    passwordFile: "<string>" # optional; string
    persist: true # optional; boolean
    secretEncoding: "plain" # optional; string; enum=["plain"]
    serviceName: "<string>" # optional; string
    usePeerDNS: true # optional; boolean
    username: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)


## WireGuardInterface

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardInterface
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # optional; string
    listenPort: 1 # optional; integer; minimum=1; maximum=65535
    mtu: 576 # optional; integer; minimum=576; maximum=9216
    peersFrom: # optional; array
      - # object
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    privateKey: "<string>" # optional; string
    privateKeyFile: "<string>" # optional; string
    selfNodeRef: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## WireGuardPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs: # Required; array
      - "<string>" # string
    endpoint: "<string>" # optional; string
    interface: "<string>" # Required; string
    persistentKeepalive: 0 # optional; integer; minimum=0; maximum=65535
    presharedKey: "<string>" # optional; string
    presharedKeyFile: "<string>" # optional; string
    publicKey: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)


## TailscaleNode

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TailscaleNode
  metadata:
    name: "<name>"
  spec:
    acceptDNS: true # optional; boolean
    acceptRoutes: true # optional; boolean
    advertiseExitNode: true # optional; boolean
    advertiseRoutes: # optional; array
      - "<string>" # string
    advertiseTags: # optional; array
      - "<string>" # string
    authKey: "<string>" # optional; string
    authKeyEnv: "<string>" # optional; string
    authKeyFile: "<string>" # optional; string
    hostname: "<string>" # optional; string
    loginServer: "<string>" # optional; string
    shieldsUp: true # optional; boolean
    ssh: true # optional; boolean
    state: "" # optional; string; enum=["", "present", "absent"]
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/tailscale-exit-subnet.yaml)


## IPsecConnection

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    certificateRef: "<string>" # optional; string
    cloudProviderHint: "" # optional; string; enum=["", "aws", "azure", "gcp"]
    leftSubnet: "<string>" # Required; string
    localAddress: "<string>" # Required; string
    phase1Proposals: # optional; array
      - "<string>" # string
    phase2Proposals: # optional; array
      - "<string>" # string
    preSharedKey: "<string>" # optional; string
    remoteAddress: "<string>" # Required; string
    rightSubnet: "<string>" # Required; string
```


## VRF

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VRF
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # optional; string
    members: # optional; array
      - "<string>" # string
    routeTable: 1 # Required; integer; minimum=1; maximum=4294967295
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/multi-instance-bgp.yaml)


## VXLANTunnel

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: "<name>"
  spec:
    bridge: "<string>" # optional; string
    ifname: "<string>" # optional; string
    localAddress: "<string>" # Required; string
    mtu: 0 # optional; integer; minimum=0
    outerDF: "" # optional; string; enum=["", "inherit", "set", "unset"]
    peers: # optional; array
      - "<string>" # string
    tcpMSSClamp: true # optional; boolean
    udpPort: 1 # optional; integer; minimum=1; maximum=65535
    underlayInterface: "<string>" # Required; string
    vni: 1 # Required; integer; minimum=1; maximum=16777215
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```


## IPv4StaticAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # Required; string
    allowOverlap: true # optional; boolean
    allowOverlapReason: "<string>" # optional; string
    exclusive: true # optional; boolean
    interface: "<string>" # Required; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/basic-static.yaml)


## VirtualAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # Required; string
    addressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    externalDNS: true # optional; boolean
    family: "ipv4" # Required; string; enum=["ipv4", "ipv6"]
    gratuitousARP: true # optional; boolean
    hostname: "<string>" # optional; string
    interface: "<string>" # Required; string
    mode: "" # optional; string; enum=["", "static", "vrrp"]
    track: # optional; array
      - # object
        confirmConsecutiveHealthy: 1 # optional; integer; minimum=1; maximum=255
        confirmConsecutiveUnhealthy: 1 # optional; integer; minimum=1; maximum=255
        resource: "<string>" # Required; string
        unhealthyPenalty: 0 # optional; integer; minimum=0; maximum=254
    vrrp: # optional; object
      additionalFailoverVMACs: # optional; array
        - # object
          interface: "<string>" # Required; string
          linkLocalAddress: "<string>" # optional; string
          macAddress: "<string>" # Required; string
          parentInterface: "<string>" # Required; string
          withdrawRouterAdvertisement: true # optional; boolean
      authentication: "<string>" # optional; string
      authenticationFrom: # optional; object
        base64: true # optional; boolean
        env: "<string>" # optional; string
        file: "<string>" # optional; string
      failoverVMAC: # optional; object
        interface: "<string>" # Required; string
        linkLocalAddress: "<string>" # optional; string
        macAddress: "<string>" # Required; string
        parentInterface: "<string>" # Required; string
        withdrawRouterAdvertisement: true # optional; boolean
      gracefulActivation: # optional; object
        readyWhen: # Required; object
          state: # optional; object
            "<key>": # optional; object
              contains: "<string>" # optional; string
              equals: "<string>" # optional; string
              exists: true # optional; boolean
              for: "<string>" # optional; string
              in: # optional; array
                - "<string>" # string
              maxAge: "<string>" # optional; string
              status: "set" # optional; string; enum=["set", "unset", "unknown"]
        timeout: "<string>" # optional; string
      peers: # optional; array
        - "<string>" # string
      preempt: true # optional; boolean
      priority: 1 # optional; integer; minimum=1; maximum=254
      virtualRouterID: 1 # Required; integer; minimum=1; maximum=255
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## BGPRouter

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: 1 # Required; integer; minimum=1
    backend: "" # optional; string; enum=["", "gobgp"]
    communities: # optional; object
      accept: # optional; array
        - "<string>" # string
      send: "" # optional; string; enum=["", "standard", "extended", "both"]
      set: # optional; object
        in: # optional; array
          - "<string>" # string
        out: # optional; array
          - "<string>" # string
    convergenceProfile: "" # optional; string; enum=["", "default", "fast", "stable"]
    exportPolicy: # optional; object
      allowedPrefixes: # optional; array
        - "<string>" # string
    gracefulRestart: # optional; object
      enabled: true # optional; boolean
      restartTime: "<string>" # optional; string
      stalePathTime: "<string>" # optional; string
    importPolicy: # optional; object
      allowedPrefixLengthMax: 0 # optional; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # optional; integer; minimum=0; maximum=128
      allowedPrefixes: # optional; array
        - "<string>" # string
      forbiddenCommunities: # optional; array
        - "<string>" # string
      localPreference: 0 # optional; integer
      nextHopRewrite: "" # optional; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # optional; array
        - "<string>" # string
    listen: # optional; object
      address: "<string>" # optional; string
      port: 1 # optional; integer; minimum=1; maximum=65535
    redistribute: # optional; object
      connected: # optional; object
        allowedPrefixes: # optional; array
          - "<string>" # string
      static: # optional; object
        allowedPrefixes: # optional; array
          - "<string>" # string
    routerID: "<string>" # Required; string
    timers: # optional; object
      profile: "" # optional; string; enum=["", "default", "fast", "slow"]
    vrf: "<string>" # optional; string
    watcher: # optional; object
      maxPrefixes: 1 # optional; integer; minimum=1; maximum=999999
      peerStateChangeThrottle: "<string>" # optional; string
      pollInterval: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## BGPPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPPeer
  metadata:
    name: "<name>"
  spec:
    bfd: "<string>" # optional; string
    communities: # optional; object
      accept: # optional; array
        - "<string>" # string
      send: "" # optional; string; enum=["", "standard", "extended", "both"]
      set: # optional; object
        in: # optional; array
          - "<string>" # string
        out: # optional; array
          - "<string>" # string
    convergenceProfile: "" # optional; string; enum=["", "default", "fast", "stable"]
    ebgpMultihop: 0 # optional; integer; minimum=0; maximum=255
    exportPolicy: # optional; object
      allowedPrefixes: # optional; array
        - "<string>" # string
    importPolicy: # optional; object
      allowedPrefixLengthMax: 0 # optional; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # optional; integer; minimum=0; maximum=128
      allowedPrefixes: # optional; array
        - "<string>" # string
      forbiddenCommunities: # optional; array
        - "<string>" # string
      localPreference: 0 # optional; integer
      nextHopRewrite: "" # optional; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # optional; array
        - "<string>" # string
    passiveMode: true # optional; boolean
    password: "<string>" # optional; string
    passwordFrom: # optional; object
      base64: true # optional; boolean
      env: "<string>" # optional; string
      file: "<string>" # optional; string
    peerASN: 1 # Required; integer; minimum=1
    peers: # optional; array
      - "<string>" # string
    routeReflectorClient: true # optional; boolean
    routeReflectorClusterID: "<string>" # optional; string
    routerRef: "<string>" # Required; string
    timers: # optional; object
      profile: "" # optional; string; enum=["", "default", "fast", "slow"]
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## BGPDynamicPeer

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPDynamicPeer
  metadata:
    name: "<name>"
  spec:
    ebgpMultihop: 0 # optional; integer; minimum=0; maximum=255
    exportPolicy: # optional; object
      allowedPrefixes: # optional; array
        - "<string>" # string
    importPolicy: # optional; object
      allowedPrefixLengthMax: 0 # optional; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # optional; integer; minimum=0; maximum=128
      allowedPrefixes: # optional; array
        - "<string>" # string
      forbiddenCommunities: # optional; array
        - "<string>" # string
      localPreference: 0 # optional; integer
      nextHopRewrite: "" # optional; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # optional; array
        - "<string>" # string
    listen: # Required; object
      sourcePrefixes: # Required; array
        - "<string>" # string
    password: "<string>" # optional; string
    passwordFrom: # optional; object
      base64: true # optional; boolean
      env: "<string>" # optional; string
      file: "<string>" # optional; string
    peerASN: 1 # Required; integer; minimum=1
    routeReflectorClient: true # optional; boolean
    routeReflectorClusterID: "<string>" # optional; string
    routerRef: "<string>" # Required; string
    timers: # optional; object
      profile: "" # optional; string; enum=["", "default", "fast", "slow"]
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## BFD

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BFD
  metadata:
    name: "<name>"
  spec:
    detectMultiplier: 1 # optional; integer; minimum=1; maximum=50
    interface: "<string>" # optional; string
    minRx: "<string>" # optional; string
    minTx: "<string>" # optional; string
    peer: "<string>" # Required; string
    profile: "" # optional; string; enum=["", "fast", "normal", "slow"]
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)


## DHCPv4Client

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: "<name>"
  spec:
    classID: "<string>" # optional; string
    clientID: "<string>" # optional; string
    hostname: "<string>" # optional; string
    interface: "<string>" # Required; string
    requestedAddress: "<string>" # optional; string
    routeMetric: 0 # optional; integer; minimum=0
    useDNS: true # optional; boolean
    useRoutes: true # optional; boolean
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)


## ClusterNetworkRoute

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ClusterNetworkRoute
  metadata:
    name: "<name>"
  spec:
    pods: # optional; object
      cidrs: # optional; array
        - "<string>" # string
    services: # optional; object
      cidrs: # optional; array
        - "<string>" # string
    via: # Required; array
      - # object
        interface: "<string>" # Required; string
        nextHop: "<string>" # Required; string
        weight: 0 # optional; integer; minimum=0; maximum=999
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/k8s-cluster-routes.yaml)


## DHCPv4Server

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # optional; object
      end: "<string>" # optional; string
      leaseTime: "<string>" # optional; string
      start: "<string>" # optional; string
    authoritative: true # optional; boolean
    dns: # optional; object
      cacheSize: 0 # optional; integer; minimum=0
      enabled: true # optional; boolean
      upstreamInterface: "<string>" # optional; string
      upstreamServers: # optional; array
        - "<string>" # string
      upstreamSource: "dhcpv4" # optional; string; enum=["dhcpv4", "static", "system", "none"]
    dnsInterface: "<string>" # optional; string
    dnsServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    dnsServers: # optional; array
      - "<string>" # string
    dnsSource: "dhcpv4" # optional; string; enum=["dhcpv4", "static", "self", "none"]
    domain: "<string>" # optional; string
    domainFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    gateway: "<string>" # optional; string
    gatewayFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    interface: "<string>" # optional; string
    leaseFile: "<string>" # optional; string
    leaseTime: "<string>" # optional; string
    listenInterfaces: # optional; array
      - "<string>" # string
    logDHCP: true # optional; boolean
    managed: true # optional; boolean
    ntpServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    ntpServers: # optional; array
      - "<string>" # string
    options: # optional; array
      - # object
        code: 1 # optional; integer; minimum=1; maximum=65535
        name: "<string>" # optional; string
        value: "<string>" # optional; string
    profiles: # optional; array
      - # object
        addressPool: # Required; object
          end: "<string>" # optional; string
          leaseTime: "<string>" # optional; string
          start: "<string>" # optional; string
        dnsServers: # optional; array
          - "<string>" # string
        gateway: "<string>" # optional; string
        name: "<string>" # Required; string
        netmask: "<string>" # optional; string
        ntpServers: # optional; array
          - "<string>" # string
        options: # optional; array
          - # object
            code: 1 # optional; integer; minimum=1; maximum=65535
            name: "<string>" # optional; string
            value: "<string>" # optional; string
    rangeEnd: "<string>" # optional; string
    rangeStart: "<string>" # optional; string
    role: "server" # optional; string; enum=["server", "transit"]
    router: "<string>" # optional; string
    routerSource: "interfaceAddress" # optional; string; enum=["interfaceAddress", "static", "none"]
    scopes: # optional; array
      - # object
        addressPool: # Required; object
          end: "<string>" # optional; string
          leaseTime: "<string>" # optional; string
          start: "<string>" # optional; string
        dnsServers: # optional; array
          - "<string>" # string
        gateway: "<string>" # optional; string
        match: # Required; object
          macAddresses: # optional; array
            - "<string>" # string
          ouiPrefixes: # optional; array
            - "<string>" # string
        name: "<string>" # Required; string
        netmask: "<string>" # optional; string
        ntpServers: # optional; array
          - "<string>" # string
        options: # optional; array
          - # object
            code: 1 # optional; integer; minimum=1; maximum=65535
            name: "<string>" # optional; string
            value: "<string>" # optional; string
        profileRef: "<string>" # optional; string
    server: "dnsmasq" # optional; string; enum=["dnsmasq", "kea", "dhcpd"]
    stickyHoldDays: 0 # optional; integer; minimum=0
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## DHCPv4Reservation

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # optional; string
    ipAddress: "<string>" # Required; string
    leaseTime: "<string>" # optional; string
    macAddress: "<string>" # Required; string
    options: # optional; array
      - # object
        code: 1 # optional; integer; minimum=1; maximum=65535
        name: "<string>" # optional; string
        value: "<string>" # optional; string
    scope: "<string>" # optional; string
    server: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-lan-dns-dhcp.yaml)


## DHCPv6Address

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Address
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # optional; string
    interface: "<string>" # Required; string
    required: true # optional; boolean
```


## IPv6RAAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RAAddress
  metadata:
    name: "<name>"
  spec:
    interface: "<string>" # Required; string
    managed: true # optional; boolean
    required: true # optional; boolean
```


## DHCPv6PrefixDelegation

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # optional; string
    clientDUID: "<string>" # optional; string
    interface: "<string>" # Required; string
    prefixLength: 1 # optional; integer; minimum=1; maximum=128
    profile: "default" # optional; string; enum=["default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd"]
    required: true # optional; boolean
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## IPv6DelegatedAddress

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: "<name>"
  spec:
    addressSuffix: "<string>" # Required; string
    announce: true # optional; boolean
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    interface: "<string>" # Required; string
    prefixDelegation: "<string>" # Required; string
    prefixLength: 1 # optional; integer; minimum=1; maximum=128
    sendRA: true # optional; boolean
    subnetID: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DHCPv6Information

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata:
    name: "<name>"
  spec:
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    interface: "<string>" # Required; string
    request: # optional; array
      - "<string>" # string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```


## IPv6RouterAdvertisement

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: "<name>"
  spec:
    configPath: "<string>" # optional; string
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    dnssl: # optional; array
      - "<string>" # string
    dnsslFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    interface: "<string>" # Required; string
    mFlag: true # optional; boolean
    mtu: 1280 # optional; integer; minimum=1280; maximum=65535
    oFlag: true # optional; boolean
    pidFile: "<string>" # optional; string
    preferredLifetime: "<string>" # optional; string
    prefix: "<string>" # optional; string
    prefixFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    prfPreference: "" # optional; string; enum=["", "low", "medium", "high"]
    rdnss: # optional; array
      - "<string>" # string
    rdnssFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    validLifetime: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DHCPv6Server

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # optional; object
      end: "<string>" # optional; string
      leaseTime: "<string>" # optional; string
      start: "<string>" # optional; string
    configPath: "<string>" # optional; string
    defaultRoute: true # optional; boolean
    delegatedAddress: "<string>" # optional; string
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    dnsServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    dnsServers: # optional; array
      - "<string>" # string
    dnsSource: "self" # optional; string; enum=["self", "static", "none"]
    domainSearch: # optional; array
      - "<string>" # string
    domainSearchFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    interface: "<string>" # optional; string
    leaseFile: "<string>" # optional; string
    leaseTime: "<string>" # optional; string
    listenInterfaces: # optional; array
      - "<string>" # string
    logDHCP: true # optional; boolean
    managed: true # optional; boolean
    mode: "stateless" # optional; string; enum=["stateless", "stateful", "both", "ra-only"]
    pidFile: "<string>" # optional; string
    rapidCommit: true # optional; boolean
    role: "server" # optional; string; enum=["server", "transit"]
    selfAddressPolicy: "<string>" # optional; string
    server: "dnsmasq" # optional; string; enum=["dnsmasq"]
    sntpServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    sntpServers: # optional; array
      - "<string>" # string
    stickyHoldDays: 0 # optional; integer; minimum=0
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## DHCPv4ServerLeaseSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # optional; string
    interval: "<string>" # optional; string
    source: # optional; object
      resource: "<string>" # Required; string
    targets: # optional; array
      - # object
        host: "<string>" # Required; string
        name: "<string>" # optional; string
        options: # optional; array
          - "<string>" # string
        sshOptions: # optional; array
          - "<string>" # string
        user: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)


## DHCPv6ServerLeaseSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # optional; string
    interval: "<string>" # optional; string
    source: # optional; object
      resource: "<string>" # Required; string
    targets: # optional; array
      - # object
        host: "<string>" # Required; string
        name: "<string>" # optional; string
        options: # optional; array
          - "<string>" # string
        sshOptions: # optional; array
          - "<string>" # string
        user: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```


## DHCPv6PrefixDelegationLeaseSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegationLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # optional; string
    interval: "<string>" # optional; string
    source: # optional; object
      resource: "<string>" # Required; string
    targets: # optional; array
      - # object
        host: "<string>" # Required; string
        name: "<string>" # optional; string
        options: # optional; array
          - "<string>" # string
        sshOptions: # optional; array
          - "<string>" # string
        user: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```


## DHCPv4Relay

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Relay
  metadata:
    name: "<name>"
  spec:
    interfaces: # Required; array
      - "<string>" # string
    upstream: "<string>" # Required; string
```


## SelfAddressPolicy

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: SelfAddressPolicy
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # Required; string; enum=["ipv4", "ipv6"]
    candidates: # Required; array
      - # object
        address: "<string>" # optional; string
        addressSuffix: "<string>" # optional; string
        delegatedAddress: "<string>" # optional; string
        interface: "<string>" # optional; string
        matchSuffix: "<string>" # optional; string
        ordinal: 1 # optional; integer; minimum=1
        source: "delegatedAddress" # Required; string; enum=["delegatedAddress", "interfaceAddress", "static"]
```


## DNSZone

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: "<name>"
  spec:
    dhcpDerived: # optional; object
      ddns: true # optional; boolean
      hostnameSuffix: "<string>" # optional; string
      leaseFile: "<string>" # optional; string
      sources: # optional; array
        - "<string>" # string
      ttl: 0 # optional; integer; minimum=0
    dnssec: # optional; object
      enabled: true # optional; boolean
      mode: "" # optional; string; enum=["", "validate", "sign"]
    records: # optional; array
      - # object
        hostname: "<string>" # Required; string
        ipv4: "<string>" # optional; string
        ipv4From: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        ipv6: "<string>" # optional; string
        ipv6From: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        ttl: 0 # optional; integer; minimum=0
    reverseZones: # optional; array
      - # object
        name: "<string>" # Required; string
    ttl: 0 # optional; integer; minimum=0
    zone: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSResolver

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: "<name>"
  spec:
    cache: # optional; object
      enabled: true # optional; boolean
      maxEntries: 0 # optional; integer; minimum=0
      maxTTL: "<string>" # optional; string
      minTTL: "<string>" # optional; string
      negativeTTL: "<string>" # optional; string
    listen: # Required; array
      - # object
        addressFrom: # optional; array
          - # object
            field: "<string>" # optional; string
            optional: true # optional; boolean
            resource: "<string>" # Required; string
        addresses: # optional; array
          - "<string>" # string
        name: "<string>" # optional; string
        port: 1 # optional; integer; minimum=1; maximum=65535
        sources: # optional; array
          - "<string>" # string
    metrics: # optional; object
      perUpstream: true # optional; boolean
    queryLog: # optional; object
      enabled: true # optional; boolean
      path: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSForwarder

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: "<name>"
  spec:
    dnssecValidate: true # optional; boolean
    healthcheck: # optional; object
      failThreshold: 1 # optional; integer; minimum=1
      interval: "<string>" # optional; string
      passThreshold: 1 # optional; integer; minimum=1
      timeout: "<string>" # optional; string
    match: # Required; array
      - "<string>" # string
    queryTimeout: "<string>" # optional; string
    resolver: "<string>" # Required; string
    upstreams: # optional; array
      - "<string>" # string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
    zoneRefs: # optional; array
      - "<string>" # string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DNSUpstream

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # optional; string
    addressFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    bootstrap: # optional; array
      - "<string>" # string
    bootstrapFrom: # optional; array
      - # object
        field: "<string>" # Required; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    path: "<string>" # optional; string
    port: 1 # optional; integer; minimum=1; maximum=65535
    protocol: "udp" # Required; string; enum=["udp", "tcp", "dot", "doh"]
    sourceInterface: "<string>" # optional; string
    tlsName: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## DSLiteTunnel

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    aftrAddressOrdinal: 1 # optional; integer; minimum=1
    aftrAddressSelection: "ordinal" # optional; string; enum=["ordinal", "ordinalModulo"]
    aftrDNSServers: # optional; array
      - "<string>" # string
    aftrFQDN: "<string>" # optional; string
    aftrFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    aftrIPv6: "<string>" # optional; string
    defaultRoute: true # optional; boolean
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    enabled: true # optional; boolean
    encapsulationLimit: "<string>" # optional; string
    interface: "<string>" # Required; string
    localAddress: "<string>" # optional; string
    localAddressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    localAddressSource: "interface" # optional; string; enum=["interface", "static", "delegatedAddress"]
    localAddressSuffix: "<string>" # optional; string
    localDelegatedAddress: "<string>" # optional; string
    mtu: 1280 # optional; integer; minimum=1280; maximum=65535
    remoteAddress: "<string>" # optional; string
    routeMetric: 0 # optional; integer; minimum=0
    tunnelName: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)


## TunnelInterface

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # optional; string
    encapDport: 1 # optional; integer; minimum=1; maximum=65535
    encapSport: 1 # optional; integer; minimum=1; maximum=65535
    key: 0 # optional; integer; minimum=0; maximum=4294967295
    local: "<string>" # optional; string
    localFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    mode: "ipip" # Required; string; enum=["ipip", "gre", "fou", "gue"]
    mtu: 576 # optional; integer; minimum=576; maximum=9216
    pathMTU: # optional; object
      forceFragmentIPv4: true # optional; boolean
    peerAddress: "<string>" # optional; string
    remote: "<string>" # optional; string
    remoteFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    trustedUnderlay: true # Required; boolean
    ttl: 1 # optional; integer; minimum=1; maximum=255
    underlayInterface: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## OverlayPeer

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: "<string>" # Required; string
    pathMTU: # optional; object
      forceFragmentIPv4: true # optional; boolean
    remote: # optional; object
      address: "<string>" # optional; string
      nodeID: "<string>" # optional; string
    role: "onprem" # Required; string; enum=["onprem", "cloud"]
    underlay: # Required; object
      address: "<string>" # optional; string
      interface: "<string>" # optional; string
      type: "wireguard" # Required; string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## HybridRoute

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: HybridRoute
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # Required; array
      - "<string>" # string
    healthCheckRef: "<string>" # optional; string
    install: # optional; object
      metric: 0 # optional; integer; minimum=0
      table: "" # optional; string; enum=["", "main"]
    peerRef: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)


## CloudProviderProfile

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: CloudProviderProfile
  metadata:
    name: "<name>"
  spec:
    auth: # Required; object
      command: "<string>" # optional; string
      mode: "external-command" # Required; string; enum=["external-command"]
    capabilities: # Required; array
      - "<string>" # string
    provider: "azure" # Required; string; enum=["azure", "aws", "oci", "gcp"]
    resourceGroup: "<string>" # optional; string
    subscriptionID: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## ProviderActionPolicy

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    allowUndo: true # optional; boolean
    allowedActions: # optional; array
      - "<string>" # string
    allowedCIDRs: # optional; array
      - "<string>" # string
    allowedProviderRefs: # optional; array
      - "<string>" # string
    allowedProviders: # optional; array
      - "<string>" # string
    dryRunOnly: true # optional; boolean
    enabled: true # optional; boolean
    executionWindow: "<string>" # optional; string
    maxActionsPerRun: 0 # optional; integer
    requireApproval: true # optional; boolean
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventGroup

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    auth: # optional; object
      mode: "" # optional; string; enum=["", "hmac"]
      secretFile: "<string>" # optional; string
      secretRef: "<string>" # optional; string
    listen: # optional; object
      address: "<string>" # optional; string
      port: 0 # optional; integer; minimum=0; maximum=65535
    nodeName: "<string>" # Required; string
    peersFrom: # optional; array
      - # object
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    replayWindow: "<string>" # optional; string
    retention: # optional; object
      maxAge: "<string>" # optional; string
      maxEvents: 0 # optional; integer; minimum=0
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventPeer

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    direction: "push" # optional; string; enum=["push"]
    endpoint: "<string>" # optional; string
    groupRef: "<string>" # Required; string
    nodeName: "<string>" # Required; string
    subjectPrefixes: # optional; array
      - "<string>" # string
    types: # optional; array
      - "<string>" # string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## EventSubscription

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventSubscription
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # Required; string
    match: # Required; object
      payload: # optional; object
        "<key>": "<string>" # optional; string
      sourceNodes: # optional; array
        - "<string>" # string
      subjectPrefixes: # optional; array
        - "<string>" # string
      types: # Required; array
        - "<string>" # string
    trigger: # Required; object
      batchWindow: "<string>" # optional; string
      debounce: "<string>" # optional; string
      pluginRef: "<string>" # Required; string
```


## FederationSLO

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    delivery: # optional; object
      expiresSoonSeconds: 0 # optional; integer; minimum=0
      lagFailSeconds: 0 # optional; integer; minimum=0
      lagWarnSeconds: 0 # optional; integer; minimum=0
    groupRef: "<string>" # Required; string
    subscription: # optional; object
      maxFailedRuns: 0 # optional; integer; minimum=0
      maxPendingRuns: 0 # optional; integer; minimum=0
```


## MobilityPool

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # Required; string
    members: # optional; array
      - # object
        capture: # optional; object
          activeWhen: # optional; object
            type: "" # optional; string; enum=["", "single-router", "vrrp-master"]
            virtualAddressRef: "<string>" # optional; string
          captureStrategy: "" # optional; string; enum=["", "route-table"]
          excludeAddresses: # optional; array
            - "<string>" # string
          gratuitousARP: true # optional; boolean
          interface: "<string>" # optional; string
          nicRef: "<string>" # optional; string
          providerRef: "<string>" # optional; string
          sourceAddress: "<string>" # optional; string
          sourceAddressFrom: # optional; object
            field: "<string>" # optional; string
            optional: true # optional; boolean
            resource: "<string>" # Required; string
          target: # optional; object
            "<key>": "<string>" # optional; string
          targetFrom: # optional; object
            "<key>": "<string>" # optional; string
          type: "provider-secondary-ip" # optional; string; enum=["provider-secondary-ip", "proxy-arp"]
        nodeRef: "<string>" # Required; string
        ownershipDiscovery: # optional; object
          allowEmptyAfter: "<string>" # optional; string
          leaseTTL: "<string>" # optional; string
          mode: "" # optional; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
          pluginRef: "<string>" # optional; string
          providerRef: "<string>" # optional; string
          scanInterval: "<string>" # optional; string
          scope: # optional; object
            excludeAddresses: # optional; array
              - "<string>" # string
            includeAddresses: # optional; array
              - "<string>" # string
          selector: # optional; object
            tags: # optional; object
              "<key>": "<string>" # optional; string
          sources: # optional; array
            - # object
              bridge: "<string>" # optional; string
              interface: "<string>" # optional; string
              leaseTTL: "<string>" # optional; string
              network: "<string>" # optional; string
              probeRetries: 0 # optional; integer; minimum=0; maximum=20
              probeTimeout: "<string>" # optional; string
              resource: "<string>" # optional; string
              scanInterval: "<string>" # optional; string
              sourceAddressFrom: # optional; object
                field: "<string>" # optional; string
                optional: true # optional; boolean
                resource: "<string>" # Required; string
              type: "dhcpv4-lease" # Required; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
          stoppedInstancePolicy: "" # optional; string; enum=["", "hold", "release"]
          subnetRef: "<string>" # optional; string
          subnetRefFrom: "<string>" # optional; string
        profileRef: "<string>" # optional; string
        staticOwnedAddresses: # optional; array
          - "<string>" # string
    membersFrom: # Required; array; minItems=1
      - # object
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    prefix: "<string>" # Required; string
    profiles: # optional; object
      cloudCaptures: # optional; object
        "<key>": # optional; object
          capture: # optional; object
            activeWhen: # optional; object
              type: "" # optional; string; enum=["", "single-router", "vrrp-master"]
              virtualAddressRef: "<string>" # optional; string
            captureStrategy: "" # optional; string; enum=["", "route-table"]
            excludeAddresses: # optional; array
              - "<string>" # string
            gratuitousARP: true # optional; boolean
            interface: "<string>" # optional; string
            nicRef: "<string>" # optional; string
            providerRef: "<string>" # optional; string
            sourceAddress: "<string>" # optional; string
            sourceAddressFrom: # optional; object
              field: "<string>" # optional; string
              optional: true # optional; boolean
              resource: "<string>" # Required; string
            target: # optional; object
              "<key>": "<string>" # optional; string
            targetFrom: # optional; object
              "<key>": "<string>" # optional; string
            type: "provider-secondary-ip" # optional; string; enum=["provider-secondary-ip", "proxy-arp"]
          ownershipDiscovery: # optional; object
            allowEmptyAfter: "<string>" # optional; string
            leaseTTL: "<string>" # optional; string
            mode: "" # optional; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
            pluginRef: "<string>" # optional; string
            providerRef: "<string>" # optional; string
            scanInterval: "<string>" # optional; string
            scope: # optional; object
              excludeAddresses: # optional; array
                - "<string>" # string
              includeAddresses: # optional; array
                - "<string>" # string
            selector: # optional; object
              tags: # optional; object
                "<key>": "<string>" # optional; string
            sources: # optional; array
              - # object
                bridge: "<string>" # optional; string
                interface: "<string>" # optional; string
                leaseTTL: "<string>" # optional; string
                network: "<string>" # optional; string
                probeRetries: 0 # optional; integer; minimum=0; maximum=20
                probeTimeout: "<string>" # optional; string
                resource: "<string>" # optional; string
                scanInterval: "<string>" # optional; string
                sourceAddressFrom: # optional; object
                  field: "<string>" # optional; string
                  optional: true # optional; boolean
                  resource: "<string>" # Required; string
                type: "dhcpv4-lease" # Required; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
            stoppedInstancePolicy: "" # optional; string; enum=["", "hold", "release"]
            subnetRef: "<string>" # optional; string
            subnetRefFrom: "<string>" # optional; string
    staticHandovers: # optional; array
      - # object
        address: "<string>" # Required; string
        fromNodeRef: "<string>" # Required; string
        toNodeRef: "<string>" # Required; string
    values: # optional; object
      "<key>": "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)


## SAMNodeSet

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes: # Required; array
      - # object
        eventEndpoint: "<string>" # optional; string
        macAddresses: # optional; array
          - "<string>" # string; pattern="^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$"
        maintenance: # optional; object
          drain: true # optional; boolean
        maxSecondaryIPs: 0 # optional; integer; minimum=0
        nodeRef: "<string>" # Required; string
        placement: # optional; object
          group: "<string>" # optional; string
          priority: 0 # optional; integer; minimum=0; maximum=1000000
        role: "" # optional; string; enum=["", "onprem", "cloud"]
        routeReflector: true # optional; boolean
        samEndpoint: "<string>" # optional; string
        samEndpointFrom: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        site: "<string>" # optional; string
        wireGuard: # optional; object
          allowedIPs: # optional; array
            - "<string>" # string
          endpoint: "<string>" # optional; string
          persistentKeepalive: 0 # optional; integer; minimum=0; maximum=65535
          publicKey: "<string>" # optional; string
          transportEndpoint: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)


## SAMEnrollmentPolicy

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentPolicy
  metadata:
    name: "<name>"
  spec:
    allowedLeafIDs: # optional; object
      pattern: "<string>" # optional; string
    directMesh: # optional; object
      peerGroupRef: "<string>" # optional; string
    endpointPrefixes: # optional; array
      - "<string>" # string
    joinAudience: "<string>" # optional; string
    joinTokenFrom: # optional; object
      base64: true # optional; boolean
      env: "<string>" # optional; string
      file: "<string>" # optional; string
    mobilityPoolRefs: # optional; array
      - "<string>" # string
    mobilityPrefixes: # optional; array
      - "<string>" # string
    rrNodeSetRef: "<string>" # optional; string
    rrSetRef: "<string>" # optional; string
    transportProfileRef: "<string>" # Required; string
    ttl: "<string>" # optional; string
    tunnelAddressPrefixes: # Required; array
      - "<string>" # string
    wireGuard: # optional; object
      allowedExtraIPPrefixes: # optional; array
        - "<string>" # string
      endpointPrefixes: # optional; array
        - "<string>" # string
      interface: "<string>" # Required; string
      persistentKeepalive: 0 # optional; integer; minimum=0; maximum=65535
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)


## SAMEnrollmentClaim

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClaim
  metadata:
    name: "<name>"
  spec:
    bgp: # optional; object
      asn: 1 # optional; integer; minimum=1
      routerID: "<string>" # optional; string
    directMesh: true # optional; boolean
    endpoint: "<string>" # optional; string
    expiresAt: "<string>" # optional; string
    joinAudience: "<string>" # optional; string
    joinHMAC: "<string>" # optional; string
    joinNonce: "<string>" # optional; string
    joinTimestamp: "<string>" # optional; string
    leafID: "<string>" # Required; string
    mobility: # optional; object
      ownedAddresses: # optional; array
        - "<string>" # string
    policyRef: "<string>" # Required; string
    revoked: true # optional; boolean
    rrSetRef: "<string>" # optional; string
    tunnelAddress: "<string>" # Required; string
    wireGuard: # optional; object
      allowedIPs: # optional; array
        - "<string>" # string
      endpoint: "<string>" # optional; string
      persistentKeepalive: 0 # optional; integer; minimum=0; maximum=65535
      publicKey: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)


## SAMEnrollmentClient

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClient
  metadata:
    name: "<name>"
  spec:
    bootstrapEndpoints: # optional; array
      - "<string>" # string
    claimRef: "<string>" # Required; string
    controlAPITLS: # optional; object
      caFile: "<string>" # optional; string
      certFile: "<string>" # optional; string
      insecureSkipVerify: true # optional; boolean
      keyFile: "<string>" # optional; string
      serverName: "<string>" # optional; string
    controlAPITokenFrom: # optional; object
      base64: true # optional; boolean
      env: "<string>" # optional; string
      file: "<string>" # optional; string
    retryBackoff: # optional; object
      max: "<string>" # optional; string
      min: "<string>" # optional; string
    rrSocket: "<string>" # optional; string
    stateTTLRefreshBefore: "<string>" # optional; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)


## SAMTransportProfile

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMTransportProfile
  metadata:
    name: "<name>"
  spec:
    addressingMode: "" # optional; string; enum=["", "edge-index", "pair-stable"]
    bgp: # Required; object
      bfd: # optional; object
        detectMultiplier: 1 # optional; integer; minimum=1; maximum=50
        enabled: true # optional; boolean
        interface: "<string>" # optional; string
        minRx: "<string>" # optional; string
        minTx: "<string>" # optional; string
        profile: "" # optional; string; enum=["", "fast", "normal", "slow"]
      convergenceProfile: "" # optional; string; enum=["", "default", "fast", "stable"]
      directLocalPreference: 0 # optional; integer
      ebgpMultihop: 0 # optional; integer; minimum=0; maximum=255
      exportPolicy: # optional; object
        allowedPrefixes: # optional; array
          - "<string>" # string
      generatePeers: true # optional; boolean
      importPolicy: # optional; object
        allowedPrefixLengthMax: 0 # optional; integer; minimum=0; maximum=128
        allowedPrefixLengthMin: 0 # optional; integer; minimum=0; maximum=128
        allowedPrefixes: # optional; array
          - "<string>" # string
        forbiddenCommunities: # optional; array
          - "<string>" # string
        localPreference: 0 # optional; integer
        nextHopRewrite: "" # optional; string; enum=["", "peer-address", "unchanged"]
        requiredCommunities: # optional; array
          - "<string>" # string
      peerASN: 1 # Required; integer; minimum=1
      routeReflectorClient: true # optional; boolean
      routeReflectorClusterID: "<string>" # optional; string
      routerRef: "<string>" # Required; string
      timers: # optional; object
        profile: "" # optional; string; enum=["", "default", "fast", "slow"]
      timersPreset: "" # optional; string; enum=["", "default", "fast", "slow"]
    encapDport: 1 # optional; integer; minimum=1; maximum=65535
    encapSport: 1 # optional; integer; minimum=1; maximum=65535
    encryption: "" # optional; string; enum=["", "none", "wireguard"]
    innerPrefix: "<string>" # Required; string
    localEndpoint: "<string>" # optional; string
    localEndpointFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    mode: "ipip" # Required; string; enum=["ipip", "gre", "fou", "gue"]
    peersFrom: # optional; array
      - # object
        direct: true # optional; boolean
        nodeRefs: # optional; array
          - "<string>" # string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    publishPeerGroup: true # optional; boolean
    selfNodeRef: "<string>" # Required; string
    underlayInterface: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)


## SAMSubnetPolicy

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMSubnetPolicy
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # Required; string
    poolRef: "<string>" # Required; string
    shards: # Required; array
      - # object
        assignedNodes: # Required; array
          - "<string>" # string
        prefix: "<string>" # Required; string
    sourcePrefix: "<string>" # Required; string
```


## IPv4Route

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4Route
  metadata:
    name: "<name>"
  spec:
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    destination: "<string>" # Required; string
    device: "<string>" # optional; string
    deviceFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    gateway: "<string>" # optional; string
    gatewayFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    metric: 0 # optional; integer; minimum=0
    preferredSource: "<string>" # optional; string
    type: "unicast" # optional; string; enum=["unicast", "blackhole"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)


## HealthCheck

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # optional; string; enum=["ipv4", "ipv6"]
    enabled: true # optional; boolean
    healthyThreshold: 1 # optional; integer; minimum=1
    interface: "<string>" # optional; string
    interval: "<string>" # optional; string
    port: 0 # optional; integer; minimum=0; maximum=65535
    protocol: "" # optional; string; enum=["", "icmp", "tcp", "dns", "http"]
    role: "link" # optional; string; enum=["link", "next-hop", "internet", "service", "policy"]
    target: "<string>" # optional; string
    targetSource: "auto" # optional; string; enum=["auto", "static", "defaultGateway", "dsliteRemote"]
    timeout: "<string>" # optional; string
    type: "ping" # optional; string; enum=["ping"]
    unhealthyThreshold: 1 # optional; integer; minimum=1
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## EgressRoutePolicy

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates: # Required; array
      - # object
        dependsOn: # optional; array
          - # object
            equals: "<string>" # optional; string
            field: "<string>" # optional; string
            notEmpty: true # optional; boolean
            optional: true # optional; boolean
            phase: "<string>" # optional; string
            resource: "<string>" # Required; string
        device: "<string>" # optional; string
        deviceFrom: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        enabled: true # optional; boolean
        gateway: "<string>" # optional; string
        gatewayFrom: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        gatewaySource: "" # optional; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
        healthCheck: "<string>" # optional; string
        interface: "<string>" # optional; string
        mark: 0 # optional; integer; minimum=0; maximum=4294967295
        metric: 0 # optional; integer; minimum=0
        name: "<string>" # optional; string
        preferredSource: "" # optional; string; enum=["", "interface"]
        priority: 0 # optional; integer; minimum=0; maximum=32765
        routeMetric: 0 # optional; integer; minimum=0
        routeTable: 0 # optional; integer; minimum=0; maximum=4294967295
        source: "<string>" # optional; string
        table: 0 # optional; integer; minimum=0; maximum=4294967295
        targets: # optional; array
          - # object
            gateway: "<string>" # optional; string
            gatewayFrom: # optional; object
              field: "<string>" # optional; string
              optional: true # optional; boolean
              resource: "<string>" # Required; string
            gatewaySource: "" # optional; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
            healthCheck: "<string>" # optional; string
            interface: "<string>" # optional; string
            mark: 0 # optional; integer; minimum=0; maximum=4294967295
            metric: 0 # optional; integer; minimum=0
            name: "<string>" # optional; string
            outboundInterface: "<string>" # optional; string
            priority: 0 # optional; integer; minimum=0; maximum=32765
            routeMetric: 0 # optional; integer; minimum=0
            routeTable: 0 # optional; integer; minimum=0; maximum=4294967295
            table: 0 # optional; integer; minimum=0; maximum=4294967295
        weight: 0 # optional; integer; minimum=0
        when: # optional; object
          state: # optional; object
            "<key>": # optional; object
              contains: "<string>" # optional; string
              equals: "<string>" # optional; string
              exists: true # optional; boolean
              for: "<string>" # optional; string
              in: # optional; array
                - "<string>" # string
              maxAge: "<string>" # optional; string
              status: "set" # optional; string; enum=["set", "unset", "unknown"]
    destinationCIDRs: # optional; array
      - "<string>" # string
    destinationSetRefs: # optional; array
      - "<string>" # string
    excludeDestinationCIDRs: # optional; array
      - "<string>" # string
    excludeDestinationSetRefs: # optional; array
      - "<string>" # string
    family: "ipv4" # optional; string; enum=["ipv4", "ipv6"]
    hashFields: # optional; array
      - "<string>" # string
    hostTraffic: true # optional; boolean
    hysteresis: "<string>" # optional; string
    mode: "priority" # optional; string; enum=["priority", "mark", "hash"]
    selection: "highest-weight-ready" # optional; string; enum=["highest-weight-ready", "weighted-ecmp"]
    sourceCIDRs: # optional; array
      - "<string>" # string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## EventRule

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EventRule
  metadata:
    name: "<name>"
  spec:
    emit: # Required; object
      attributes: # optional; object
        "<key>": "<string>" # optional; string
      topic: "<string>" # Required; string
    pattern: # Required; object
      allow_missing_correlation: true # optional; boolean
      correlate_by: "<string>" # optional; string
      duration: "<string>" # optional; string
      expected: "<string>" # optional; string
      interval: "<string>" # optional; string
      operator: "all_of" # Required; string; enum=["all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"]
      quiet: "<string>" # optional; string
      rate: 0 # optional; integer; minimum=0
      strict: true # optional; boolean
      threshold: 0 # optional; integer; minimum=0
      topic: "<string>" # optional; string
      topics: # optional; array
        - "<string>" # string
      trigger: "<string>" # optional; string
      window: "<string>" # optional; string
```


## DerivedEvent

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DerivedEvent
  metadata:
    name: "<name>"
  spec:
    emitInitial: true # optional; boolean
    emitWhen: "all_true" # optional; string; enum=["all_true", "any_true"]
    hysteresis: "<string>" # optional; string
    inputs: # Required; array
      - # object
        any_of: # optional; array
          - # array
            - # object
              equals: "<string>" # optional; string
              field: "<string>" # optional; string
              not_empty: true # optional; boolean
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        not_empty: true # optional; boolean
    retractWhen: "any_false" # optional; string; enum=["any_false", "all_false"]
    topic: "<string>" # Required; string
```


## NAT44Rule

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs: # optional; array
      - "<string>" # string
    destinationSetRefs: # optional; array
      - "<string>" # string
    egressInterface: "<string>" # optional; string
    egressPolicyRef: "<string>" # optional; string
    excludeDestinationCIDRs: # optional; array
      - "<string>" # string
    excludeDestinationSetRefs: # optional; array
      - "<string>" # string
    outboundInterface: "<string>" # optional; string
    snatAddress: "<string>" # optional; string
    snatAddressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    sourceCIDRs: # optional; array
      - "<string>" # string
    sourceRanges: # optional; array
      - "<string>" # string
    translation: # optional; object
      address: "<string>" # optional; string
      addresses: # optional; array
        - "<string>" # string
      portMapping: # optional; object
        end: 1 # optional; integer; minimum=1; maximum=65535
        start: 1 # optional; integer; minimum=1; maximum=65535
        type: "auto" # optional; string; enum=["auto", "preserve", "range"]
      type: "interfaceAddress" # Required; string; enum=["interfaceAddress", "address", "pool"]
    type: "masquerade" # optional; string; enum=["masquerade", "snat"]
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)


## NAT44FlowDNATPinhole

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44FlowDNATPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # optional; object
      key: "" # optional; string; enum=["", "localPort"]
    fromInterfaceRefs: # optional; array
      - "<string>" # string
    inbound: # Required; object
      sourcePorts: # Required; array
        - "<string>" # string
    outbound: # Required; object
      destinationCIDRs: # optional; array
        - "<string>" # string
      destinationPorts: # Required; array
        - "<string>" # string
      destinationSetRefs: # optional; array
        - "<string>" # string
      protocol: "udp" # Required; string; enum=["udp"]
      sourceCIDRs: # optional; array
        - "<string>" # string
      sourceSetRef: "<string>" # optional; string
    timeout: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```


## NAT44SessionSync

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: "<name>"
  spec:
    conntrackCommand: "<string>" # optional; string
    conntrackd: # optional; object
      ignoreIPv4: # optional; array
        - "<string>" # string
      interface: "<string>" # Required; string
      localAddress: "<string>" # Required; string
      peerAddress: "<string>" # Required; string
      port: 1 # optional; integer; minimum=1; maximum=65535
    excludeNatRules: # optional; array
      - "<string>" # string
    interval: "<string>" # optional; string
    mode: "event-stream" # optional; string; enum=["event-stream", "conntrackd"]
    natRules: # optional; array
      - "<string>" # string
    snatAddresses: # optional; array
      - "<string>" # string
    targets: # optional; array
      - # object
        host: "<string>" # Required; string
        name: "<string>" # optional; string
        restoreCommand: # optional; array
          - "<string>" # string
        sshOptions: # optional; array
          - "<string>" # string
        user: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```


## ManagementAccess

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ManagementAccess
  metadata:
    name: "<name>"
  spec:
    allowSourceCIDRs: # optional; array
      - "<string>" # string
    interfaces: # Required; array
      - "<string>" # string
    requireWebConsoleBound: true # optional; boolean
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)


## PortForward

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    hairpin: # optional; object
      enabled: true # optional; boolean
      interfaces: # optional; array
        - "<string>" # string
      mode: "" # optional; string; enum=["", "auto", "manual", "off"]
    listen: # Required; object
      address: "<string>" # optional; string
      addressFrom: # optional; object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
      interface: "<string>" # Required; string
      port: 1 # Required; integer; minimum=1; maximum=65535
      protocol: "tcp" # Required; string; enum=["tcp", "udp"]
    target: # Required; object
      address: "<string>" # optional; string
      addressFrom: # optional; object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
      port: 1 # Required; integer; minimum=1; maximum=65535
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)


## IngressService

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends: # Required; array
      - # object
        address: "<string>" # optional; string
        addressFrom: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        name: "<string>" # optional; string
        port: 1 # Required; integer; minimum=1; maximum=65535
        weight: 0 # optional; integer; minimum=0
    externalDNS: true # optional; boolean
    hairpin: # optional; object
      enabled: true # optional; boolean
      interfaces: # optional; array
        - "<string>" # string
      mode: "" # optional; string; enum=["", "auto", "manual", "off"]
    healthCheck: # optional; object
      expectedBody: "<string>" # optional; string
      expectedStatus: # optional; array
        - 0 # integer
      healthyThreshold: 1 # optional; integer; minimum=1
      host: "<string>" # optional; string
      interval: "<string>" # optional; string
      path: "<string>" # optional; string
      protocol: "" # optional; string; enum=["", "tcp", "http", "https"]
      timeout: "<string>" # optional; string
      tlsSkipVerify: true # optional; boolean
      unhealthyThreshold: 1 # optional; integer; minimum=1
    hostname: "<string>" # optional; string
    listen: # Required; object
      address: "<string>" # optional; string
      addressFrom: # optional; object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
      interface: "<string>" # Required; string
      port: 1 # Required; integer; minimum=1; maximum=65535
      protocol: "tcp" # Required; string; enum=["tcp", "udp"]
    policy: # optional; object
      onNoHealthyBackends: "" # optional; string; enum=["", "drop", "reject"]
      selection: "" # optional; string; enum=["", "failover", "sourceHash", "random"]
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)


## IPAddressSet

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPAddressSet
  metadata:
    name: "<name>"
  spec:
    addresses: # optional; array
      - "<string>" # string
    names: # optional; array
      - "<string>" # string
    refreshInterval: "<string>" # optional; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)


## LocalServiceRedirect

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: LocalServiceRedirect
  metadata:
    name: "<name>"
  spec:
    interface: "<string>" # Required; string
    rules: # Required; array
      - # object
        destinationPort: 1 # Required; integer; minimum=1; maximum=65535
        destinationSetRef: "<string>" # Required; string
        name: "<string>" # optional; string
        protocols: # Required; array
          - "<string>" # string
        redirectPort: 1 # Required; integer; minimum=1; maximum=65535
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)


## FirewallFlowPinhole

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallFlowPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # optional; object
      key: "" # optional; string; enum=["", "remoteAddress", "localEndpoint"]
    fromZone: "<string>" # Required; string
    inbound: # Required; object
      sourcePorts: # Required; array
        - "<string>" # string
    outbound: # Required; object
      destinationCIDRs: # optional; array
        - "<string>" # string
      destinationPorts: # Required; array
        - "<string>" # string
      destinationSetRefs: # optional; array
        - "<string>" # string
      protocol: "udp" # Required; string; enum=["udp"]
      sourceCIDRs: # optional; array
        - "<string>" # string
      sourceSetRef: "<string>" # optional; string
    timeout: "<string>" # optional; string
    toZone: "<string>" # Required; string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```


## TrafficFlowLog

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TrafficFlowLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # optional; boolean
    includeApplicationLayer: true # optional; boolean
    includeTLSSNI: true # optional; boolean
    path: "<string>" # optional; string
    source: "" # optional; string; enum=["", "conntrack"]
```


## FirewallZone

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: "<name>"
  spec:
    interfaces: # Required; array
      - "<string>" # string
    role: "untrust" # Required; string; enum=["untrust", "trust", "mgmt"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)


## FirewallPolicy

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: "<name>"
  spec:
    logDeny: true # optional; boolean
    sameRoleAccept: true # optional; boolean
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-basic-ipv4-nat.yaml)


## ClientPolicy

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    classification: # optional; array
      - # object
        ipv4Reservation: "<string>" # optional; string
        ipv6Addresses: # optional; array
          - "<string>" # string
        match: # Required; object
          dhcpFingerprints: # optional; array
            - "<string>" # string
          hostnamePatterns: # optional; array
            - "<string>" # string
          macs: # optional; array
            - "<string>" # string
          ouiPrefixes: # optional; array
            - "<string>" # string
        mode: "trusted" # Required; string; enum=["trusted", "guest", "isolated"]
        name: "<string>" # optional; string
    guestEgressAllow: # optional; array
      - "<string>" # string
    guestEgressDeny: # optional; array
      - "<string>" # string
    guestServices: # optional; array
      - "<string>" # string
    interfaces: # optional; array
      - "<string>" # string
    isolation: # optional; object
      lanInternet: "" # optional; string; enum=["", "allow", "deny"]
      lanLAN: "" # optional; string; enum=["", "allow", "deny"]
      lanMgmt: "" # optional; string; enum=["", "allow", "deny"]
      mDNSBroadcast: "" # optional; string; enum=["", "allow", "deny"]
    macs: # optional; array
      - "<string>" # string
    mode: "include" # Required; string; enum=["include", "exclude"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)


## FirewallRule

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: "<name>"
  spec:
    action: "accept" # Required; string; enum=["accept", "drop", "reject"]
    connLimit: # optional; object
      log: true # optional; boolean
      maxPerSource: 0 # optional; integer; minimum=0
    destinationCIDRs: # optional; array
      - "<string>" # string
    destinationPorts: # optional; array
      - "<string>" # string
    destinationSetRefs: # optional; array
      - "<string>" # string
    excludeDestinationSetRefs: # optional; array
      - "<string>" # string
    fromZone: "<string>" # Required; string
    icmpType: "<string>" # optional; string
    icmpv6Type: "<string>" # optional; string
    log: true # optional; boolean
    port: 0 # optional; integer; minimum=0; maximum=65535
    protocol: "" # optional; string; enum=["", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip"]
    rateLimit: # optional; object
      burst: 0 # optional; integer; minimum=0
      log: true # optional; boolean
      packetsPerSecond: 0 # optional; integer; minimum=0
      per: "" # optional; string; enum=["", "second", "minute"]
      rate: 0 # optional; integer; minimum=0
      unit: "" # optional; string; enum=["", "packet", "byte", "kilobyte", "megabyte"]
    sourcePorts: # optional; array
      - "<string>" # string
    srcCIDRs: # optional; array
      - "<string>" # string
    toZone: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/firewall-rate-limit.yaml)


## FirewallEventLog

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # optional; boolean
    events: # optional; array
      - "deny" # string; enum=["deny", "allow", "rateLimit", "connLimit"]
    fromZones: # optional; array
      - "<string>" # string
    log: # optional; object
      acceptSampleRate: 0 # optional; integer; minimum=0
      copyRange: 0 # optional; integer; minimum=0
      dropSampleRate: 0 # optional; integer; minimum=0
      reject: true # optional; boolean
    nflogGroup: 0 # optional; integer; minimum=0; maximum=65535
    path: "<string>" # optional; string
    retention: "<string>" # optional; string
    rules: # optional; array
      - "<string>" # string
    sampleRate: 0 # optional; integer; minimum=0
    sinks: # optional; array
      - "<string>" # string
    toZones: # optional; array
      - "<string>" # string
```


## Hostname

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

### YAML definition

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Hostname
  metadata:
    name: "<name>"
  spec:
    hostname: "<string>" # Required; string
    managed: true # optional; boolean
```
