// SPDX-License-Identifier: BSD-3-Clause

package api

import "sort"

const (
	ProvidesTypeString     = "string"
	ProvidesTypeStringList = "stringList"
	ProvidesTypeInt        = "int"
	ProvidesTypeBool       = "bool"
	ProvidesTypeObject     = "object"
	ProvidesTypeObjectList = "objectList"
	ProvidesTypeTimestamp  = "timestamp"
)

type ProvidedFieldSpec struct {
	Name        string
	Type        string
	Description string
}

type ResourceProvidesSpec struct {
	Kind   string
	Fields []ProvidedFieldSpec
}

func ResourceProvides(kind string) []ProvidedFieldSpec {
	fields := resourceProvidesTable()[kind]
	if len(fields) == 0 {
		return nil
	}
	out := append([]ProvidedFieldSpec(nil), fields...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func ResourceProvidesContract() []ResourceProvidesSpec {
	table := resourceProvidesTable()
	kinds := make([]string, 0, len(table))
	for kind := range table {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	out := make([]ResourceProvidesSpec, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, ResourceProvidesSpec{Kind: kind, Fields: ResourceProvides(kind)})
	}
	return out
}

func ResourceProvidesField(kind, field string) bool {
	_, ok := ResourceProvidesFieldType(kind, field)
	return ok
}

func ResourceProvidesFieldType(kind, field string) (string, bool) {
	for _, provided := range resourceProvidesTable()[kind] {
		if provided.Name == field {
			return provided.Type, true
		}
	}
	return "", false
}

func resourceProvidesTable() map[string][]ProvidedFieldSpec {
	common := []ProvidedFieldSpec{{Name: "phase", Type: ProvidesTypeString, Description: "Current lifecycle phase"}}
	withCommon := func(fields ...ProvidedFieldSpec) []ProvidedFieldSpec {
		out := append([]ProvidedFieldSpec{}, common...)
		out = append(out, fields...)
		return out
	}
	s := func(name, desc string) ProvidedFieldSpec {
		return ProvidedFieldSpec{Name: name, Type: ProvidesTypeString, Description: desc}
	}
	ss := func(name, desc string) ProvidedFieldSpec {
		return ProvidedFieldSpec{Name: name, Type: ProvidesTypeStringList, Description: desc}
	}
	i := func(name, desc string) ProvidedFieldSpec {
		return ProvidedFieldSpec{Name: name, Type: ProvidesTypeInt, Description: desc}
	}
	b := func(name, desc string) ProvidedFieldSpec {
		return ProvidedFieldSpec{Name: name, Type: ProvidesTypeBool, Description: desc}
	}
	o := func(name, desc string) ProvidedFieldSpec {
		return ProvidedFieldSpec{Name: name, Type: ProvidesTypeObject, Description: desc}
	}
	os := func(name, desc string) ProvidedFieldSpec {
		return ProvidedFieldSpec{Name: name, Type: ProvidesTypeObjectList, Description: desc}
	}
	t := func(name, desc string) ProvidedFieldSpec {
		return ProvidedFieldSpec{Name: name, Type: ProvidesTypeTimestamp, Description: desc}
	}

	return map[string][]ProvidedFieldSpec{
		"BFD":                             withCommon(s("peer", "Observed or configured peer")),
		"BGPDynamicPeer":                  withCommon(s("peerGroup", "GoBGP peer group used by dynamic neighbors"), ss("sourcePrefixes", "Accepted dynamic neighbor source prefixes"), i("sourcePrefixCount", "Configured dynamic neighbor source prefix count"), os("discoveredPeers", "Observed dynamic BGP peers with best-effort admission counters"), i("discoveredPeerCount", "Observed dynamic BGP peer count"), i("acceptedRouteCount", "Routerd-side accepted dynamic route count"), i("rejectedRouteCount", "Routerd-side rejected dynamic route count"), o("rejectedRouteSummary", "Routerd-side rejected dynamic route reasons"), t("observedAt", "Observation time")),
		"BGPPeer":                         withCommon(s("address", "Peer address"), s("state", "Peer session state"), i("acceptedPrefixes", "Accepted prefix count"), t("observedAt", "Observation time")),
		"BGPRouter":                       withCommon(os("peers", "Observed peers"), i("prefixes", "Observed prefix count"), i("establishedPeers", "Established peer count"), i("acceptedPrefixes", "Accepted prefix count"), t("observedAt", "Observation time")),
		"Bridge":                          withCommon(s("ifname", "Kernel interface name"), ss("members", "Bridge member interfaces")),
		"ClientPolicy":                    withCommon(),
		"ClusterNetworkRoute":             withCommon(ss("pods", "Pod CIDRs"), ss("services", "Service CIDRs")),
		"DHCPv4Client":                    withCommon(s("interface", "Logical interface"), s("device", "Kernel device"), s("currentAddress", "Current IPv4 address"), s("gateway", "Default gateway"), s("defaultGateway", "Default gateway"), ss("dnsServers", "DNS servers"), ss("ntpServers", "NTP servers"), s("domain", "Domain name"), i("leaseTime", "Lease lifetime seconds"), t("renewAt", "Renew time"), t("rebindAt", "Rebind time"), t("expiresAt", "Expiry time")),
		"DHCPv4Relay":                     withCommon(),
		"DHCPv4Reservation":               withCommon(s("address", "Reserved IPv4 address"), s("hostname", "Reserved hostname")),
		"DHCPv4Server":                    withCommon(s("interface", "Serving interface"), ss("dnsServers", "Advertised DNS servers"), ss("ntpServers", "Advertised NTP servers"), s("domain", "Advertised domain"), s("configPath", "Rendered dnsmasq config"), b("dryRun", "Dry-run status")),
		"DHCPv4ServerLeaseSync":           withCommon(s("command", "Sync command"), i("sourceCount", "Synced source count"), i("targetCount", "Sync target count"), os("sources", "Synced source files"), os("targets", "Sync targets"), t("syncedAt", "Last sync time"), b("dryRun", "Dry-run status")),
		"DHCPv6ServerLeaseSync":           withCommon(s("command", "Sync command"), i("sourceCount", "Synced source count"), i("targetCount", "Sync target count"), os("sources", "Synced source files"), os("targets", "Sync targets"), t("syncedAt", "Last sync time"), b("dryRun", "Dry-run status")),
		"DHCPv6PrefixDelegationLeaseSync": withCommon(s("command", "Sync command"), i("sourceCount", "Synced source count"), i("targetCount", "Sync target count"), os("sources", "Synced source files"), os("targets", "Sync targets"), t("syncedAt", "Last sync time"), b("dryRun", "Dry-run status")),
		"DHCPv6Address":                   withCommon(s("interface", "Logical interface"), s("address", "Observed DHCPv6 address")),
		"DHCPv6Information":               withCommon(s("source", "Prefix delegation source"), s("aftrName", "AFTR hostname"), ss("dnsServers", "DNS servers"), ss("sntpServers", "SNTP servers"), ss("domainSearch", "Domain search list")),
		"DHCPv6PrefixDelegation":          withCommon(s("interface", "Logical interface"), s("currentPrefix", "Delegated IPv6 prefix"), ss("dnsServers", "DNS servers"), ss("sntpServers", "SNTP servers"), ss("domainSearch", "Domain search list"), s("aftrName", "AFTR hostname")),
		"DHCPv6Server":                    withCommon(s("interface", "Serving interface"), ss("dnsServers", "Advertised DNS servers"), ss("sntpServers", "Advertised SNTP servers"), s("configPath", "Rendered dnsmasq config"), b("dryRun", "Dry-run status")),
		"DNSForwarder":                    withCommon(s("resolver", "Resolver reference"), ss("upstreams", "Upstream references")),
		"DNSResolver":                     withCommon(i("listeners", "Listener count"), ss("listenAddresses", "Resolved listen addresses"), i("sources", "Source count"), t("updatedAt", "Update time")),
		"DNSUpstream":                     withCommon(s("address", "Upstream address"), s("url", "Resolved upstream URL")),
		"DNSZone":                         withCommon(s("zone", "DNS zone name"), i("records", "Record count"), os("pendingRecords", "Records waiting for source status"), t("updatedAt", "Update time")),
		"DSLiteTunnel":                    withCommon(s("interface", "Tunnel interface name"), s("device", "Tunnel device name"), s("tunnelName", "Tunnel device name"), s("localIPv6", "Local IPv6 endpoint"), s("innerLocalIPv4", "Inner local IPv4 endpoint"), s("innerRemoteIPv4", "Inner remote IPv4 endpoint"), s("localInterface", "Local underlay interface"), s("aftrName", "AFTR hostname"), s("aftrIPv6", "AFTR IPv6 endpoint"), i("mtu", "Tunnel MTU"), b("dryRun", "Dry-run status")),
		"AddressMobilityDomain":           withCommon(s("prefix", "Mobility domain IPv4 prefix"), s("mode", "Mobility mode"), s("peerRef", "Default overlay peer reference"), i("claimCount", "Member claim count"), os("claims", "Member claim statuses")),
		"CloudProviderProfile":            withCommon(s("provider", "Cloud provider"), ss("capabilities", "Provider capabilities")),
		"DerivedEvent":                    withCommon(s("topic", "Emitted event topic")),
		"EgressRoutePolicy":               withCommon(s("family", "Address family"), s("selectedCandidate", "Selected candidate"), s("selectedSource", "Selected candidate source"), s("selectedDevice", "Selected output device"), s("selectedGateway", "Selected gateway"), s("selectedGatewaySource", "Selected gateway source"), i("selectedRouteTable", "Selected route table"), i("selectedMetric", "Selected metric"), i("selectedWeight", "Selected weight"), i("selectedTargets", "Selected target count"), s("selectedInterface", "Selected logical interface"), os("candidates", "Candidate status list"), s("role", "Controller role for selection-only status"), b("advisory", "Selection-only advisory status"), t("lastTransitionAt", "Last transition time"), t("updatedAt", "Update time"), b("dryRun", "Apply dry-run status")),
		"EventGroup":                      withCommon(s("group", "Event federation group name"), s("nodeName", "Local event federation node name"), i("peers", "Resolved event peer count"), os("peersFrom", "Resolved peersFrom source status"), ss("pendingSources", "Status sources that have not resolved yet"), s("listenAddress", "Event receiver listen address"), i("listenPort", "Event receiver listen port")),
		"EventRule":                       withCommon(s("topic", "Emitted event topic")),
		"FirewallEventLog":                withCommon(s("path", "Log path"), ss("sinks", "Log sink references")),
		"FirewallFlowPinhole":             withCommon(),
		"FirewallPolicy":                  withCommon(),
		"FirewallRule":                    withCommon(s("action", "Rendered action")),
		"FirewallZone":                    withCommon(ss("interfaces", "Zone interfaces")),
		"HealthCheck":                     withCommon(s("role", "Health-check role"), s("target", "Probe target"), s("sourceAddress", "Probe source address"), s("sourceInterface", "Probe source interface"), s("protocol", "Probe protocol"), i("consecutiveFailed", "Consecutive failure count"), t("lastCheckedAt", "Last probe time")),
		"Hostname":                        withCommon(s("hostname", "Configured hostname")),
		"HybridRoute":                     withCommon(os("routes", "Lowered IPv4Route resources"), s("peerRef", "Overlay peer reference"), b("defaultRouteUntouched", "Default route safety guard"), i("estimatedMTU", "Estimated overlay MTU")),
		"IPAddressSet":                    withCommon(ss("addresses", "All addresses"), ss("ipv4Addresses", "IPv4 addresses"), ss("ipv6Addresses", "IPv6 addresses"), t("updatedAt", "Update time")),
		"IPsecConnection":                 withCommon(),
		"IngressService":                  withCommon(s("hostname", "Service hostname"), s("listenAddress", "Resolved listen address"), o("activeBackend", "Selected backend"), os("activeBackends", "Selected backend set"), i("healthyBackends", "Healthy backend count"), i("totalBackends", "Total backend count"), os("backends", "Backend status list"), t("observedAt", "Observation time"), b("dryRun", "Dry-run status")),
		"Interface":                       withCommon(s("ifname", "Kernel interface name"), ss("addresses", "All observed addresses"), ss("ipv4Addresses", "Observed IPv4 addresses"), ss("ipv6Addresses", "Observed IPv6 addresses"), s("primaryIPv4", "Primary observed IPv4 address"), s("primaryIPv6", "Primary observed IPv6 address"), s("macAddress", "Observed MAC address")),
		"Inventory":                       withCommon(o("host", "Host inventory")),
		"IPv4Route":                       withCommon(s("type", "Route type"), s("destination", "Route destination"), s("device", "Output device"), s("gateway", "Route gateway"), s("preferredSource", "Route preferred source address"), i("metric", "Route metric"), b("dryRun", "Dry-run status")),
		"IPv4StaticAddress":               withCommon(s("interface", "Logical interface"), s("ifname", "Kernel interface name"), s("address", "Configured IPv4 address"), b("dryRun", "Dry-run status")),
		"IPv4StaticRoute":                 withCommon(s("destination", "Route destination"), s("gateway", "Route gateway"), s("interface", "Output interface")),
		"IPv6DelegatedAddress":            withCommon(s("address", "Derived IPv6 address"), s("interface", "Logical interface"), s("prefixSource", "Prefix delegation source"), b("dryRun", "Dry-run status")),
		"IPv6RAAddress":                   withCommon(s("interface", "Logical interface"), s("address", "Observed RA address")),
		"IPv6RouterAdvertisement":         withCommon(s("interface", "Serving interface"), s("prefix", "Advertised prefix"), ss("rdnss", "Advertised RDNSS servers"), s("configPath", "Rendered dnsmasq config"), b("dryRun", "Dry-run status")),
		"RogueRADetector":                 withCommon(s("interface", "Observed interface"), s("selfMAC", "Local router MAC address"), s("packetsSeen", "Observed RA packet count"), s("rogueCount", "Observed non-self router count"), s("observedRouters", "JSON encoded observed router list")),
		"IPv6StaticRoute":                 withCommon(s("destination", "Route destination"), s("gateway", "Route gateway"), s("interface", "Output interface")),
		"LocalServiceRedirect":            withCommon(),
		"LogRetention":                    withCommon(os("targets", "Retention targets"), t("updatedAt", "Update time")),
		"LogSink":                         withCommon(s("type", "Sink type")),
		"ManagementAccess":                withCommon(ss("interfaces", "Management interfaces")),
		"MobilityMemberSet":               withCommon(i("memberCount", "Published member count")),
		"MobilityPool":                    withCommon(s("groupRef", "Federation event group"), s("prefix", "Mobility pool IPv4 prefix"), s("plannerPhase", "BGP mobility planner phase"), s("plannerReason", "BGP mobility planner reason"), s("dynamicSource", "Generated DynamicConfigPart source"), i("generatedBGPPaths", "Generated BGP /32 path count"), i("generatedActions", "Generated provider trap action count"), i("resolvedMemberCount", "Resolved member count"), ss("pendingSources", "Status sources that have not resolved yet"), s("placementGroup", "Capture placement group"), b("placementActive", "Whether this node is placement-active"), s("placementActiveNode", "Selected placement-active node"), ss("discoverySelfPrivateIPs", "Provider inventory self private IPs"), ss("discoveryLocalInventoryIPs", "Provider local inventory private IPs"), s("providerActionPhase", "Provider action lifecycle phase"), i("providerActionFailedCount", "Active provider action failure count"), ss("providerActionFailedAddresses", "Addresses with active provider action failures"), i("providerActionSupersededFailureCount", "Provider action failures superseded by current provider truth"), ss("providerActionSupersededFailureAddresses", "Addresses whose provider action failures are superseded by current provider truth"), s("providerActionSupersededFailureReason", "Reason provider action failures were superseded"), s("ownershipResolverPhase", "Ownership resolver phase"), i("ownershipResolverAddressCount", "Ownership resolver address count"), os("ownershipResolverDecisions", "Ownership resolver per-address decisions"), os("ownershipResolverControlPlaneOwnerTable", "SAM control-plane owner table")),
		"NAT44Rule":                       withCommon(s("egressInterface", "Resolved egress interface"), s("snatAddress", "Resolved SNAT address"), b("dryRun", "Dry-run status")),
		"NAT44FlowDNATPinhole":            withCommon(),
		"NAT44SessionSync":                withCommon(s("mode", "Sync mode"), s("streamState", "Event stream state"), i("sessionCount", "Synced session count"), i("targetCount", "Sync target count"), ss("snatAddresses", "SNAT addresses"), os("targets", "Sync targets"), i("deleteOK", "Successful conntrack delete count"), i("deleteMissing", "Missing conntrack delete count"), i("deleteFailed", "Failed conntrack delete count"), i("insertOK", "Successful conntrack insert count"), i("insertExisting", "Existing conntrack insert count"), i("insertFailed", "Failed conntrack insert count"), i("queuedEventCount", "Queued conntrack event count"), i("lastBatchEvents", "Last event batch size"), i("resyncCount", "Snapshot resync count"), t("syncedAt", "Last sync time"), t("lastEventAt", "Last event time"), t("lastBatchAt", "Last event batch time"), t("lastResyncAt", "Last snapshot resync time"), b("dryRun", "Dry-run status")),
		"NTPClient":                       withCommon(ss("servers", "Configured upstream servers"), s("source", "Server source"), t("updatedAt", "Update time")),
		"NTPServer":                       withCommon(ss("servers", "Configured upstream servers"), ss("listenAddresses", "Resolved listen addresses"), ss("allowCIDRs", "Resolved client allow CIDRs"), s("source", "Server source"), t("updatedAt", "Update time")),
		"ObservabilityPipeline":           withCommon(ss("signals", "Exported signals")),
		"OverlayPeer":                     withCommon(s("nodeID", "Overlay node ID"), s("role", "Overlay role"), s("underlayType", "Underlay type"), s("underlayInterface", "Underlay interface")),
		"PPPoESession":                    withCommon(s("interface", "Logical interface"), s("device", "PPP device"), s("currentAddress", "Current IPv4 address"), s("peerAddress", "PPP peer address"), s("gateway", "PPP gateway"), ss("dnsServers", "DNS servers"), t("connectedAt", "Connection time"), b("dryRun", "Dry-run status")),
		"Package":                         withCommon(ss("packages", "Package names"), b("dryRun", "Dry-run status")),
		"PortForward":                     withCommon(s("listenAddress", "Resolved listen address"), o("target", "Resolved target"), b("dryRun", "Dry-run status")),
		"RouterdCluster":                  withCommon(s("leader", "Current leader"), t("leaseExpiresAt", "Lease expiry time")),
		"SelfAddressPolicy":               withCommon(s("address", "Selected address"), s("source", "Selected source")),
		"RemoteAddressClaim":              withCommon(s("domainRef", "Address mobility domain reference"), s("address", "Claimed /32 address"), s("ownerSide", "Owning side"), s("captureType", "Capture type"), s("captureInterface", "Capture interface"), s("peerRef", "Delivery overlay peer reference"), s("deliveryMode", "Delivery mode"), s("deliveryRouteName", "Lowered IPv4Route name"), s("deliveryDevice", "Delivery tunnel interface"), i("deliveryMetric", "Delivery route metric")),
		"SAMNodeSet":                      withCommon(i("nodeCount", "SAM fabric node count")),
		"SAMRRSet":                        withCommon(s("enrollmentPolicyRef", "Shared enrollment policy reference"), i("memberCount", "RR member count"), ss("members", "RR member node references")),
		"SAMEnrollmentPolicy":             withCommon(i("acceptedClaims", "Enrollment claims that can be materialized"), i("skippedClaims", "Enrollment claims skipped by policy, expiry, revocation, or authorization"), ss("leafIDs", "Accepted leaf identities")),
		"SAMEnrollmentClaim":              withCommon(s("leafID", "Leaf identity"), s("rrSetRef", "RR set reference"), s("tunnelAddress", "Leaf tunnel address"), s("endpoint", "Leaf transport endpoint"), b("revoked", "Claim revocation state"), t("expiresAt", "Claim expiry time")),
		"SAMEnrollmentClient":             withCommon(s("claimRef", "Submitted enrollment claim reference"), s("observedRRSet", "Fetched RR set reference"), t("lastAttempt", "Last join attempt time"), t("lastSuccess", "Last successful join/fetch time"), t("nextAttempt", "Next allowed join attempt time"), s("backoff", "Current retry backoff"), s("reason", "Current enrollment reason")),
		"SAMTransportProfile":             withCommon(s("selfNode", "Local SAM transport node reference"), s("dynamicSource", "Generated DynamicConfigPart source"), s("innerPrefix", "Inner tunnel prefix"), i("generatedTunnels", "Generated tunnel interface count"), i("generatedBGPPeers", "Generated BGP peer count"), i("generatedBFDs", "Generated BFD session count"), i("generatedEndpointRoutes", "Generated endpoint route count"), os("peers", "Derived per-peer transport details"), os("peersFrom", "Resolved peersFrom source status"), ss("topologyNodeRefs", "Resolved SAM transport topology node references"), ss("pendingSources", "Status sources that have not resolved yet")),
		"Sysctl":                          withCommon(s("key", "Sysctl key"), s("value", "Applied value"), b("dryRun", "Dry-run status")),
		"SysctlProfile":                   withCommon(s("profile", "Applied profile"), b("dryRun", "Dry-run status")),
		"TailscaleNode":                   withCommon(s("tailnetName", "Tailnet name"), i("peerCount", "Peer count"), ss("advertiseRoutes", "Advertised routes")),
		"Telemetry":                       withCommon(ss("signals", "Exported signals")),
		"TunnelInterface":                 withCommon(s("interface", "Tunnel device name"), s("mode", "Tunnel mode"), s("local", "Local underlay address"), s("remote", "Remote underlay address"), s("underlayInterface", "Underlay interface used for MTU derivation"), i("underlayMTU", "Underlay MTU"), i("tunnelOverhead", "Tunnel encapsulation overhead"), i("mtu", "Tunnel MTU"), i("ttl", "Tunnel TTL"), i("encapSport", "FOU/GUE local UDP source/listen port"), i("encapDport", "FOU/GUE peer UDP destination port"), b("dryRun", "Dry-run status")),
		"TrafficFlowLog":                  withCommon(s("path", "Flow log path"), ss("sinks", "Log sink references")),
		"VRF":                             withCommon(s("ifname", "VRF device"), i("routeTable", "Route table"), ss("members", "Member interfaces")),
		"VXLANTunnel":                     withCommon(s("ifname", "Tunnel device"), i("vni", "VXLAN VNI")),
		"VXLANSegment":                    withCommon(s("ifname", "VXLAN device"), i("vni", "VXLAN VNI")),
		"VirtualAddress":                  withCommon(s("address", "Resolved virtual address"), s("ifname", "Kernel interface name"), s("role", "VRRP role"), s("hostname", "Announced hostname"), i("virtualRouterID", "VRRP router ID"), i("priority", "Effective VRRP priority"), b("dryRun", "Dry-run status")),
		"WebConsole":                      withCommon(s("listenAddress", "Resolved listen address"), i("port", "Listen port")),
		"WireGuardInterface":              withCommon(s("publicKey", "Interface public key"), s("selfNodeRef", "Local SAM node reference used for peersFrom self-skip"), i("listenPort", "Listen port"), i("fwmark", "Firewall mark"), i("peerCount", "Peer count"), o("hostFirewall", "Managed host firewall opening for the listen port"), os("peersFrom", "Resolved peersFrom source status"), ss("pendingSources", "Status sources that have not resolved yet")),
		"WireGuardPeer":                   withCommon(s("latestEndpoint", "Latest endpoint"), t("latestHandshake", "Latest handshake"), i("handshakeAgeSeconds", "Handshake age seconds"), i("transferRxBytes", "Received bytes"), i("transferTxBytes", "Transmitted bytes")),
	}
}
