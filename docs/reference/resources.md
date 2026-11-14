# Resource configuration reference

Place resources inside Router.spec.resources. Names, interfaces, addresses, credentials and peer settings in examples must be adapted. A schema-required field is required within its parent object; cross-resource and semantic constraints are checked by routerd validate, not by the schema alone.

[Kind/name · when · dependsOn](common-fields.md)

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

- [Plugin](resources/Plugin.md)
- [DynamicConfigSource](resources/DynamicConfigSource.md)
- [DynamicOverridePolicy](resources/DynamicOverridePolicy.md)
- [LogSink](resources/LogSink.md)
- [Telemetry](resources/Telemetry.md)
- [ObservabilityPipeline](resources/ObservabilityPipeline.md)
- [LogRetention](resources/LogRetention.md)
- [Sysctl](resources/Sysctl.md)
- [SysctlProfile](resources/SysctlProfile.md)
- [Package](resources/Package.md)
- [NTPClient](resources/NTPClient.md)
- [NTPServer](resources/NTPServer.md)
- [WebConsole](resources/WebConsole.md)
- [ControlAPI](resources/ControlAPI.md)
- [RouterdCluster](resources/RouterdCluster.md)
- [Interface](resources/Interface.md)
- [PPPoESession](resources/PPPoESession.md)
- [WireGuardInterface](resources/WireGuardInterface.md)
- [WireGuardPeer](resources/WireGuardPeer.md)
- [TailscaleNode](resources/TailscaleNode.md)
- [IPsecConnection](resources/IPsecConnection.md)
- [VRF](resources/VRF.md)
- [VXLANTunnel](resources/VXLANTunnel.md)
- [IPv4StaticAddress](resources/IPv4StaticAddress.md)
- [VirtualAddress](resources/VirtualAddress.md)
- [BGPRouter](resources/BGPRouter.md)
- [BGPPeer](resources/BGPPeer.md)
- [BGPDynamicPeer](resources/BGPDynamicPeer.md)
- [BFD](resources/BFD.md)
- [DHCPv4Client](resources/DHCPv4Client.md)
- [ClusterNetworkRoute](resources/ClusterNetworkRoute.md)
- [DHCPv4Server](resources/DHCPv4Server.md)
- [DHCPv4Reservation](resources/DHCPv4Reservation.md)
- [DHCPv6Address](resources/DHCPv6Address.md)
- [IPv6RAAddress](resources/IPv6RAAddress.md)
- [DHCPv6PrefixDelegation](resources/DHCPv6PrefixDelegation.md)
- [IPv6DelegatedAddress](resources/IPv6DelegatedAddress.md)
- [DHCPv6Information](resources/DHCPv6Information.md)
- [IPv6RouterAdvertisement](resources/IPv6RouterAdvertisement.md)
- [DHCPv6Server](resources/DHCPv6Server.md)
- [DHCPv4ServerLeaseSync](resources/DHCPv4ServerLeaseSync.md)
- [DHCPv6ServerLeaseSync](resources/DHCPv6ServerLeaseSync.md)
- [DHCPv6PrefixDelegationLeaseSync](resources/DHCPv6PrefixDelegationLeaseSync.md)
- [DHCPv4Relay](resources/DHCPv4Relay.md)
- [SelfAddressPolicy](resources/SelfAddressPolicy.md)
- [DNSZone](resources/DNSZone.md)
- [DNSResolver](resources/DNSResolver.md)
- [DNSForwarder](resources/DNSForwarder.md)
- [DNSUpstream](resources/DNSUpstream.md)
- [DSLiteTunnel](resources/DSLiteTunnel.md)
- [TunnelInterface](resources/TunnelInterface.md)
- [OverlayPeer](resources/OverlayPeer.md)
- [HybridRoute](resources/HybridRoute.md)
- [CloudProviderProfile](resources/CloudProviderProfile.md)
- [ProviderActionPolicy](resources/ProviderActionPolicy.md)
- [EventGroup](resources/EventGroup.md)
- [EventPeer](resources/EventPeer.md)
- [EventSubscription](resources/EventSubscription.md)
- [FederationSLO](resources/FederationSLO.md)
- [MobilityPool](resources/MobilityPool.md)
- [SAMNodeSet](resources/SAMNodeSet.md)
- [SAMEnrollmentPolicy](resources/SAMEnrollmentPolicy.md)
- [SAMEnrollmentClaim](resources/SAMEnrollmentClaim.md)
- [SAMEnrollmentClient](resources/SAMEnrollmentClient.md)
- [SAMTransportProfile](resources/SAMTransportProfile.md)
- [SAMSubnetPolicy](resources/SAMSubnetPolicy.md)
- [IPv4Route](resources/IPv4Route.md)
- [HealthCheck](resources/HealthCheck.md)
- [EgressRoutePolicy](resources/EgressRoutePolicy.md)
- [EventRule](resources/EventRule.md)
- [DerivedEvent](resources/DerivedEvent.md)
- [NAT44Rule](resources/NAT44Rule.md)
- [NAT44FlowDNATPinhole](resources/NAT44FlowDNATPinhole.md)
- [NAT44SessionSync](resources/NAT44SessionSync.md)
- [ManagementAccess](resources/ManagementAccess.md)
- [PortForward](resources/PortForward.md)
- [IngressService](resources/IngressService.md)
- [IPAddressSet](resources/IPAddressSet.md)
- [LocalServiceRedirect](resources/LocalServiceRedirect.md)
- [FirewallFlowPinhole](resources/FirewallFlowPinhole.md)
- [TrafficFlowLog](resources/TrafficFlowLog.md)
- [FirewallZone](resources/FirewallZone.md)
- [FirewallPolicy](resources/FirewallPolicy.md)
- [ClientPolicy](resources/ClientPolicy.md)
- [FirewallRule](resources/FirewallRule.md)
- [FirewallEventLog](resources/FirewallEventLog.md)
- [Hostname](resources/Hostname.md)

## Plugin

[Plugin](resources/Plugin.md)

Registers an executable implementing the routerd plugin protocol. Invoke it explicitly or through a configured consumer such as EventSubscription.


## DynamicConfigSource

[DynamicConfigSource](resources/DynamicConfigSource.md)

Declares a plugin-backed dynamic configuration source. The current implementation decodes and validates this kind, but contains no runtime consumer of DynamicConfigSourceSpec; declaring it does not invoke the plugin.


## DynamicOverridePolicy

[DynamicOverridePolicy](resources/DynamicOverridePolicy.md)

Authorizes selected dynamic sources to mask exact startup resources while the dynamic part is active. It does not rewrite the saved startup configuration.


## LogSink

[LogSink](resources/LogSink.md)

Defines an operational event-log destination selected by its type. It is separate from ObservabilityPipeline log sinks.


## Telemetry

[Telemetry](resources/Telemetry.md)

Configures OpenTelemetry environment variables for rendered managed services. It does not define a LogSink or a separate collector process.


## ObservabilityPipeline

[ObservabilityPipeline](resources/ObservabilityPipeline.md)

Combines an OpenTelemetry environment declaration for managed services with an independent built-in operational-event log consumer.


## LogRetention

[LogRetention](resources/LogRetention.md)

Prunes operational-log SQLite records and optionally archives them first. It does not control arbitrary system log files.


## Sysctl

[Sysctl](resources/Sysctl.md)

Reconciles one kernel sysctl setting and can write a persistent sysctl.d file. This resource has no reference to another routerd resource.


## SysctlProfile

[SysctlProfile](resources/SysctlProfile.md)

Applies a named set of router-oriented sysctls, with optional per-key overrides. It does not refer to a Sysctl resource by name.


## Package

[Package](resources/Package.md)

Ensures selected OS packages are installed. The controller selects the first package set whose os exactly matches the running host.


## NTPClient

[NTPClient](resources/NTPClient.md)

Manages or observes an upstream time-synchronization client.


## NTPServer

[NTPServer](resources/NTPServer.md)

Configures a time server and its upstream servers using chrony or FreeBSD ntpd.


## WebConsole

[WebConsole](resources/WebConsole.md)

Serves the read-only router web console from routerd. Its HTTP listener is separate from the mutating ControlAPI listener.


## ControlAPI

[ControlAPI](resources/ControlAPI.md)

Enables the HTTP control API for reading state and changing configuration. Unix-socket control and the WebConsole are separate.


## RouterdCluster

[RouterdCluster](resources/RouterdCluster.md)

Coordinates which routerd process may perform mutations through a file lease and OS file locking. It is not VRRP election or peer network discovery.


## Interface

[Interface](resources/Interface.md)

Names an existing OS interface so other resources can refer to its logical metadata.name. It does not create a physical interface.


## PPPoESession

[PPPoESession](resources/PPPoESession.md)

Run a supervised routerd-pppoe-client using pppd on Linux or mpd5 on FreeBSD, and publish negotiated IPv4 session status. Several legacy fields affect only manual FreeBSD render output, not this daemon path.


## WireGuardInterface

[WireGuardInterface](resources/WireGuardInterface.md)

Create or configure a WireGuard device and apply its complete peer configuration with wg setconf. Address assignment and ordinary routing are separate resources.


## WireGuardPeer

[WireGuardPeer](resources/WireGuardPeer.md)

Define a peer on a declared WireGuardInterface. Its AllowedIPs configure WireGuard cryptographic routing; they do not automatically create ordinary IP routes in this controller.


## TailscaleNode

[TailscaleNode](resources/TailscaleNode.md)

Manage Tailscale enrollment options through a routerd-generated systemd oneshot on Linux or rc.d script on FreeBSD, then observe tailscale status --json. Tailscale/Headscale server-side approvals remain separate.


## IPsecConnection

[IPsecConnection](resources/IPsecConnection.md)

Render a strongSwan swanctl connection and synchronize all routerd IPsec connections together. A traffic-triggered CHILD_SA trap is configured; loading the connection is not proof of an established SA.


## VRF

[VRF](resources/VRF.md)

Declares a Linux VRF device, routing table and member intentions. The current implementation renders plans/artifact identities and protects its routing table, but has no VRF creation/member-attachment controller. Do not treat plan output as proof that a VRF was created.


## VXLANTunnel

[VXLANTunnel](resources/VXLANTunnel.md)

Manage a Linux VXLAN device with static remote flood destinations and optional bridge attachment. No multicast discovery or dynamic MAC learning is configured.


## IPv4StaticAddress

[IPv4StaticAddress](resources/IPv4StaticAddress.md)

Declares a static IPv4 address on a logical interface. The controller converges configured addresses; this is not a DHCP lease.


## VirtualAddress

[VirtualAddress](resources/VirtualAddress.md)


## BGPRouter

[BGPRouter](resources/BGPRouter.md)

Configure the local GoBGP instance, its admitted routes and locally originated advertisements. Actual kernel installation and remote reachability are separate from a BGP Established state.


## BGPPeer

[BGPPeer](resources/BGPPeer.md)

Configure one or more static BGP neighbors on a declared BGPRouter.


## BGPDynamicPeer

[BGPDynamicPeer](resources/BGPDynamicPeer.md)

Define a GoBGP inbound dynamic-neighbor admission group, not an individual peer instance or a WireGuard/SAM authorization identity.


## BFD

[BFD](resources/BFD.md)

Configure FRR bfdd sessions through vtysh and observe their status for BGPPeer failure handling. This is a separate backend from GoBGP; BFD Down can trigger a BGP peer reset.


## DHCPv4Client

[DHCPv4Client](resources/DHCPv4Client.md)

Runs the managed routerd-dhcpv4-client daemon and applies its current lease address, optional default route and optional host DNS settings.


## ClusterNetworkRoute

[ClusterNetworkRoute](resources/ClusterNetworkRoute.md)

Declare IPv4 routes to Kubernetes Pod and Service networks through explicitly configured next hops. This does not discover cluster CIDRs or nodes.


## DHCPv4Server

[DHCPv4Server](resources/DHCPv4Server.md)

Serve IPv4 DHCP pools, tagged selector scopes and reservations through the current shared dnsmasq service.


## DHCPv4Reservation

[DHCPv4Reservation](resources/DHCPv4Reservation.md)

Reserve one IPv4 address for a MAC address on a declared DHCPv4Server.


## DHCPv6Address

[DHCPv6Address](resources/DHCPv6Address.md)

Declares an IPv6 address acquired by a host DHCPv6 client. It is separate from DHCPv6PrefixDelegation and does not allocate LAN prefixes.


## IPv6RAAddress

[IPv6RAAddress](resources/IPv6RAAddress.md)

Declares host-side receipt of IPv6 router advertisements/SLAAC. This is an RA client, not an IPv6RouterAdvertisement server for the LAN.


## DHCPv6PrefixDelegation

[DHCPv6PrefixDelegation](resources/DHCPv6PrefixDelegation.md)

Acquire an IPv6 delegated prefix on an uplink. Use IPv6DelegatedAddress to derive and apply downstream or host addresses; obtaining a prefix alone does not advertise it to clients.


## IPv6DelegatedAddress

[IPv6DelegatedAddress](resources/IPv6DelegatedAddress.md)

Derive and install a local IPv6 address from a DHCPv6PrefixDelegation prefix.


## DHCPv6Information

[DHCPv6Information](resources/DHCPv6Information.md)

Projects AFTR, DNS, SNTP and domain-search information from the DHCPv6-PD daemon that uses the same interface. It does not start a standalone information-request client.


## IPv6RouterAdvertisement

[IPv6RouterAdvertisement](resources/IPv6RouterAdvertisement.md)

Configure IPv6 router-advertisement options on a LAN and deprecate previously delegated prefixes.


## DHCPv6Server

[DHCPv6Server](resources/DHCPv6Server.md)

Provide LAN DHCPv6 address/information service and associated RA behavior using the shared dnsmasq process.


## DHCPv4ServerLeaseSync

[DHCPv4ServerLeaseSync](resources/DHCPv4ServerLeaseSync.md)

Copy a DHCPv4 server lease file and available sticky-state files to peer hosts using rsync over SSH. This is file replication, not the DHCP failover protocol.


## DHCPv6ServerLeaseSync

[DHCPv6ServerLeaseSync](resources/DHCPv6ServerLeaseSync.md)

Copy one DHCPv6 server lease file to peer hosts using rsync over SSH; not a DHCP failover protocol.


## DHCPv6PrefixDelegationLeaseSync

[DHCPv6PrefixDelegationLeaseSync](resources/DHCPv6PrefixDelegationLeaseSync.md)

Replicate the built-in DHCPv6-PD client lease snapshot to peer hosts for recovery/takeover.


## DHCPv4Relay

[DHCPv4Relay](resources/DHCPv4Relay.md)

Relay DHCPv4 requests through dnsmasq to one explicitly configured IPv4 server.


## SelfAddressPolicy

[SelfAddressPolicy](resources/SelfAddressPolicy.md)

Describe an ordered self-address selection policy used by supporting static DHCPv6 rendering. It does not assign an address to an interface.


## DNSZone

[DNSZone](resources/DNSZone.md)

Local A/AAAA records and generated PTR answers served through a DNSForwarder zoneRefs rule. This is not a general authoritative DNS server with arbitrary record types, zone transfers or signing.


## DNSResolver

[DNSResolver](resources/DNSResolver.md)

A local DNS service with UDP and TCP listeners, connected to DNSForwarder rules.


## DNSForwarder

[DNSForwarder](resources/DNSForwarder.md)

Select local zones or upstream DNS servers for matching query names on one DNSResolver.


## DNSUpstream

[DNSUpstream](resources/DNSUpstream.md)

A DNS server endpoint referenced by DNSForwarder; declaration alone does not start a listener or forward queries.


## DSLiteTunnel

[DSLiteTunnel](resources/DSLiteTunnel.md)

Encapsulate IPv4 in IPv6 toward an AFTR. Tunnel setup, an IPv4 default route, NAT and reachability checks are separate concerns.


## TunnelInterface

[TunnelInterface](resources/TunnelInterface.md)

Create and reconcile an unencrypted IPv4 outer tunnel and an optional IPv4 inner address. metadata.name is the actual OS tunnel name.


## OverlayPeer

[OverlayPeer](resources/OverlayPeer.md)

Describe an existing transport used by HybridRoute and path-MTU policy derivation. This resource does not establish WireGuard, Tailscale, IPsec or a tunnel by itself.


## HybridRoute

[HybridRoute](resources/HybridRoute.md)

Expand explicitly listed IPv4 destination networks into main-table IPv4Route resources through an OverlayPeer.


## CloudProviderProfile

[CloudProviderProfile](resources/CloudProviderProfile.md)

Describe a provider identity and default target metadata used when planning provider actions. This declaration does not authenticate to a cloud or create resources.


## ProviderActionPolicy

[ProviderActionPolicy](resources/ProviderActionPolicy.md)

Gate execution of proposed provider actions by executor Plugins. Declaring a policy is not itself an action or a cloud login.


## EventGroup

[EventGroup](resources/EventGroup.md)

Declare a shared federation event group and this router’s identity, peer discovery, receiver and local retention settings.


## EventPeer

[EventPeer](resources/EventPeer.md)

Declare one remote delivery target for an EventGroup. Transport authentication and local identity come from that group.


## EventSubscription

[EventSubscription](resources/EventSubscription.md)

Match nonexpired persisted federation events and invoke a local Plugin, storing its result as a DynamicConfigPart.


## FederationSLO

[FederationSLO](resources/FederationSLO.md)

Configure per-group thresholds used by routerctl doctor for federation delivery and subscription diagnostics. This does not change delivery retry or plugin execution policy.


## MobilityPool

[MobilityPool](resources/MobilityPool.md)


## SAMNodeSet

[SAMNodeSet](resources/SAMNodeSet.md)

Declare shared SAM node identity/topology used by transport, event peers, WireGuard and MobilityPool member resolution. Local provider capture/discovery details belong in a MobilityPool self overlay, not here.


## SAMEnrollmentPolicy

[SAMEnrollmentPolicy](resources/SAMEnrollmentPolicy.md)

Define RR-side admission boundaries for SAM leaf identity, transport addresses, optional join authentication and authorized mobility ownership. Leaves consume the admitted topology instead of copying this policy.


## SAMEnrollmentClaim

[SAMEnrollmentClaim](resources/SAMEnrollmentClaim.md)

Declare the leaf-authored identity, transport and mobility intent submitted for SAM enrollment. Expiry and revocation are RR/controller administration state, not part of the leaf join HMAC.


## SAMEnrollmentClient

[SAMEnrollmentClient](resources/SAMEnrollmentClient.md)

Submit a local enrollment claim to bootstrap RR control APIs, fetch admitted topology and persist it as dynamic SAMRRSet/optional direct peer-group state for existing controllers. No cloud infrastructure is created.


## SAMTransportProfile

[SAMTransportProfile](resources/SAMTransportProfile.md)


## SAMSubnetPolicy

[SAMSubnetPolicy](resources/SAMSubnetPolicy.md)

Publish shard assignments as federation events so each node can narrow MobilityPool ownership discovery to its assigned IPv4 subnets. This is a discovery-scope assignment, not an IP routing resource.


## IPv4Route

[IPv4Route](resources/IPv4Route.md)

Install an IPv4 unicast or blackhole route, optionally resolving device and gateway from resource status.


## HealthCheck

[HealthCheck](resources/HealthCheck.md)

Probe an IP target and publish Healthy/Unhealthy state for route selection. A successful probe verifies only its configured protocol and path.


## EgressRoutePolicy

[EgressRoutePolicy](resources/EgressRoutePolicy.md)


## EventRule

[EventRule](resources/EventRule.md)

Derive an event from patterns in the persisted local event journal, with optional per-correlation state.


## DerivedEvent

[DerivedEvent](resources/DerivedEvent.md)

Publish asserted/retracted events when conditions over resource status change. Unlike EventRule, this evaluates current status rather than an event sequence.


## NAT44Rule

[NAT44Rule](resources/NAT44Rule.md)

Translate routed IPv4 source addresses through an egress interface or selected EgressRoutePolicy device. NAT does not itself permit forwarding or assign the translated address.


## NAT44FlowDNATPinhole

[NAT44FlowDNATPinhole](resources/NAT44FlowDNATPinhole.md)

Learn an IPv4 UDP internal endpoint from outbound traffic and dynamically DNAT inbound UDP by its destination port. Linux nftables only; the current PF renderer does not implement this resource.


## NAT44SessionSync

[NAT44SessionSync](resources/NAT44SessionSync.md)

Replicate Linux conntrack state using either an SSH event stream or a continuously running conntrackd FTFW peer link. This is not FreeBSD PF state synchronization.


## ManagementAccess

[ManagementAccess](resources/ManagementAccess.md)

Declare the interfaces checked by the pre-apply management-lockout check. This resource does not create a management listener or firewall allow rule.


## PortForward

[PortForward](resources/PortForward.md)


## IngressService

[IngressService](resources/IngressService.md)


## IPAddressSet

[IPAddressSet](resources/IPAddressSet.md)

Share literal and DNS-resolved IP addresses among supported nftables NAT, routing and firewall rules. It does not create routes or start a DNS service.


## LocalServiceRedirect

[LocalServiceRedirect](resources/LocalServiceRedirect.md)

Redirect matching traffic received on a declared interface to a service port on the router itself, using nftables prerouting NAT.


## FirewallFlowPinhole

[FirewallFlowPinhole](resources/FirewallFlowPinhole.md)

Learn allowed return IPv4 UDP endpoints for a pair of firewall zones, without address translation. Linux nftables only; the current PF renderer omits this resource.


## TrafficFlowLog

[TrafficFlowLog](resources/TrafficFlowLog.md)

Record connection snapshots in a SQLite flow database, optionally enriching them from the DPI cache. This is not a packet capture or a log of every packet.


## FirewallZone

[FirewallZone](resources/FirewallZone.md)

Group configured interfaces under a named trust role for firewall generation. This does not create an OS interface or a separate routing/bridge domain.


## FirewallPolicy

[FirewallPolicy](resources/FirewallPolicy.md)

Set shared implicit firewall options used alongside zones and explicit rules. Multiple resources are aggregated rather than applied independently.


## ClientPolicy

[ClientPolicy](resources/ClientPolicy.md)

Apply guest-style local-service and routed-destination restrictions to selected or non-exempt clients. Linux identifies clients by configured MACs; FreeBSD PF requires explicit IP identities.


## FirewallRule

[FirewallRule](resources/FirewallRule.md)

Declare a named accept/drop/reject rule for a zone path, with address/protocol/port matches and Linux rate/connection predicates.


## FirewallEventLog

[FirewallEventLog](resources/FirewallEventLog.md)

Enable the firewall packet-log collector and configure its database, Linux NFLOG group and renderer logging options. Several accepted filter/archive fields currently have no runtime consumer.


## Hostname

[Hostname](resources/Hostname.md)

Manages the OS hostname, separately from Router.metadata.name and resource names.
