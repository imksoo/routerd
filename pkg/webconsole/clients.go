// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/logstore"
)

func (h Handler) clients(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	clientSince := now.Add(-clientObservationWindow)
	leases, err := h.dhcpLeaseList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stickyLeases, err := h.dhcpStickyLeaseList(logstore.DHCPStickyFilter{HeldOnly: true, Now: now, Limit: 10000})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	leases = annotateDHCPLeasesWithSticky(leases, stickyLeases, now)
	neighbors, err := neighborList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	flows, err := h.trafficFlowList(r.Context(), logstore.TrafficFlowFilter{Since: clientSince, Limit: 200})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	queries, err := h.queryLogList(r.Context(), logstore.DNSQueryFilter{Since: clientSince, Limit: 1000})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	flows = enrichTrafficFlowsWithDNS(flows, queries)
	if enriched, err := h.enrichTrafficFlowsWithDPI(r.Context(), flows, now, clientObservationWindow); err == nil {
		flows = enriched
	}
	firewallLogs, err := h.firewallLogList(r.Context(), logstore.FirewallLogFilter{Since: clientSince, Action: "drop", Limit: 1000})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dhcpFingerprints, err := h.dhcpFingerprintList(logstore.DHCPFingerprintFilter{Since: clientSince, Limit: 1000})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, h.annotateClientsWithPolicy(correlateClients(leases, neighbors, flows, queries, firewallLogs, dhcpFingerprints)))
}

func correlateClients(leases []DHCPLease, neighbors []NeighborEntry, flows []logstore.TrafficFlow, queries []logstore.DNSQuery, firewallLogs []logstore.FirewallLogEntry, dhcpFingerprints ...[]logstore.DHCPFingerprint) []ClientEntry {
	rows := map[string]*clientMutableEntry{}
	ipToKey := map[string]string{}
	passive := buildPassiveFingerprints(leases, flows, queries, firewallLogs)
	dhcpFingerprintByMAC := latestDHCPFingerprintByMAC(dhcpFingerprints...)
	upsert := func(key, address string) *clientMutableEntry {
		key = strings.TrimSpace(key)
		if key == "" {
			key = strings.TrimSpace(address)
		}
		if key == "" {
			key = "-"
		}
		row := rows[key]
		if row == nil {
			row = &clientMutableEntry{
				ClientEntry: ClientEntry{ID: key},
				addresses:   map[string]bool{},
				sources:     map[string]bool{},
				peers:       map[string]bool{},
				activity:    map[string]*clientActivityStat{},
			}
			rows[key] = row
		}
		if address = strings.TrimSpace(address); address != "" {
			row.addresses[address] = true
			ipToKey[address] = key
		}
		return row
	}
	for _, lease := range leases {
		if strings.TrimSpace(lease.IP) == "" {
			continue
		}
		key := clientCorrelationKey(lease.MAC, lease.IP)
		row := upsert(key, lease.IP)
		if row.Hostname == "" {
			row.Hostname = lease.Hostname
		}
		if row.MAC == "" {
			row.MAC = normalizeClientMAC(lease.MAC)
		}
		if row.Vendor == "" {
			row.Vendor = lease.Vendor
		}
		if lease.StickyUntil != nil && !lease.StickyUntil.IsZero() {
			row.StickyUntil = lease.StickyUntil.Format(time.RFC3339Nano)
			row.StickyState = firstNonEmptyString(lease.StickyState, "held")
		}
		row.sources["dhcpv4"] = true
	}
	for _, neighbor := range neighbors {
		if strings.TrimSpace(neighbor.IP) == "" {
			continue
		}
		if neighborStateFailed(neighbor.State) {
			continue
		}
		key := clientCorrelationKey(neighbor.MAC, neighbor.IP)
		row := upsert(key, neighbor.IP)
		if row.MAC == "" {
			row.MAC = normalizeClientMAC(neighbor.MAC)
		}
		if row.Vendor == "" {
			row.Vendor = neighbor.Vendor
		}
		if row.State == "" {
			row.State = neighbor.State
		}
		source := strings.TrimSpace(neighbor.Source)
		if source == "" {
			source = "neighbor"
		}
		row.sources[source] = true
	}
	for ip, fingerprint := range passive {
		if ip == "" || ipToKey[ip] != "" {
			continue
		}
		if key := matchFingerprintToClient(rows, ip, fingerprint); key != "" {
			ipToKey[ip] = key
			rows[key].addresses[ip] = true
		}
	}
	for _, query := range queries {
		ip := strings.TrimSpace(query.ClientAddress)
		if ip == "" {
			continue
		}
		key := ipToKey[ip]
		if key == "" {
			key = passiveCorrelationKey(passive[ip], ip)
		}
		row := upsert(key, ip)
		if query.QuestionName != "" {
			row.peers[strings.TrimSuffix(query.QuestionName, ".")] = true
		}
		row.sources["dns"] = true
	}
	for _, flow := range flows {
		ip := strings.TrimSpace(flow.ClientAddress)
		if ip == "" {
			continue
		}
		key := ipToKey[ip]
		if key == "" {
			key = passiveCorrelationKey(passive[ip], ip)
		}
		row := upsert(key, ip)
		if flow.Accounting {
			row.BytesOut += flow.BytesOut
			row.BytesIn += flow.BytesIn
		}
		peer := firstNonEmptyString(flow.TLSSNI, flow.HTTPHost, flow.DNSQuery, flow.ResolvedHostname, flow.PeerAddress)
		if peer != "" {
			row.peers[peer] = true
		}
		row.recordActivity(flowActivityName(flow), flowActivityDetail(flow), flow.BytesOut+flow.BytesIn, flow.EndedAt)
		row.sources["traffic"] = true
	}
	out := make([]ClientEntry, 0, len(rows))
	for _, row := range rows {
		row.Addresses = sortedClientAddresses(row.addresses)
		row.Sources = sortedSet(row.sources)
		row.Peers = sortedSet(row.peers)
		row.applyActivitySummary()
		fingerprint := inferClientFingerprint(row.ClientEntry, passive, dhcpFingerprintByMAC[normalizeClientMAC(row.MAC)])
		row.InferredOSFamily = fingerprint.OSFamily
		row.InferredDeviceClass = fingerprint.DeviceClass
		row.FingerprintConfidence = fingerprint.Confidence
		row.FingerprintSignals = fingerprint.Signals
		out = append(out, row.ClientEntry)
	}
	sort.Slice(out, func(i, j int) bool {
		trafficI := out[i].BytesOut + out[i].BytesIn
		trafficJ := out[j].BytesOut + out[j].BytesIn
		if trafficI != trafficJ {
			return trafficI > trafficJ
		}
		return out[i].ID < out[j].ID
	})
	return out
}

type clientPolicyAssignment struct {
	Name      string
	Mode      string
	Isolation []string
}

func (h Handler) annotateClientsWithPolicy(clients []ClientEntry) []ClientEntry {
	if h.opts.Router == nil || len(clients) == 0 {
		return clients
	}
	byMAC := map[string]clientPolicyAssignment{}
	for _, res := range h.opts.Router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "ClientPolicy" {
			continue
		}
		spec, err := res.ClientPolicySpec()
		if err != nil {
			continue
		}
		assignment := clientPolicyAssignment{Name: res.Metadata.Name, Mode: spec.Mode, Isolation: clientPolicyIsolationLabels(spec)}
		for _, mac := range spec.MACs {
			if normalized := normalizeClientMAC(mac); normalized != "" {
				byMAC[normalized] = assignment
			}
		}
		for _, entry := range spec.Classification {
			for _, mac := range entry.Match.MACs {
				normalized := normalizeClientMAC(mac)
				if normalized == "" {
					continue
				}
				switch spec.Mode {
				case "include":
					if entry.Mode == "guest" || entry.Mode == "isolated" {
						byMAC[normalized] = assignment
					}
				case "exclude":
					if entry.Mode == "trusted" {
						byMAC[normalized] = clientPolicyAssignment{Name: res.Metadata.Name, Mode: "trusted", Isolation: []string{"trusted exception"}}
					}
				}
			}
		}
	}
	for i := range clients {
		assignment, ok := byMAC[normalizeClientMAC(clients[i].MAC)]
		if !ok {
			continue
		}
		clients[i].ClientPolicy = assignment.Name
		clients[i].ClientPolicyMode = assignment.Mode
		clients[i].IsolationPolicy = assignment.Isolation
	}
	return clients
}

func clientPolicyIsolationLabels(spec api.ClientPolicySpec) []string {
	var labels []string
	if spec.Isolation.LANInternet != "" {
		labels = append(labels, "internet "+spec.Isolation.LANInternet)
	}
	if spec.Isolation.LANLAN != "" {
		labels = append(labels, "LAN "+spec.Isolation.LANLAN)
	}
	if spec.Isolation.LANMgmt != "" {
		labels = append(labels, "mgmt "+spec.Isolation.LANMgmt)
	}
	if spec.Isolation.MDNSBroadcast != "" {
		labels = append(labels, "discovery "+spec.Isolation.MDNSBroadcast)
	}
	if len(labels) == 0 {
		labels = append(labels, "private LAN deny")
	}
	return labels
}

func neighborStateFailed(state string) bool {
	for _, part := range strings.Split(strings.ToUpper(state), ",") {
		if strings.TrimSpace(part) == "FAILED" {
			return true
		}
	}
	return false
}

func clientCorrelationKey(mac, ip string) string {
	if normalized := normalizeClientMAC(mac); normalized != "" {
		return normalized
	}
	return strings.TrimSpace(ip)
}

func normalizeClientMAC(mac string) string {
	return strings.ToLower(strings.TrimSpace(mac))
}

func (row *clientMutableEntry) recordActivity(protocol, detail string, bytes int64, seen time.Time) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" || protocol == "unknown" {
		protocol = "unidentified"
	}
	if row.activity == nil {
		row.activity = map[string]*clientActivityStat{}
	}
	stat := row.activity[protocol]
	if stat == nil {
		stat = &clientActivityStat{Protocol: protocol}
		row.activity[protocol] = stat
	}
	stat.Count++
	if bytes > 0 {
		stat.Bytes += bytes
	}
	if strings.TrimSpace(detail) != "" {
		stat.Detail = detail
	}
	if seen.IsZero() {
		seen = time.Now().UTC()
	}
	if stat.LastSeen.IsZero() || seen.After(stat.LastSeen) {
		stat.LastSeen = seen
	}
}

func (row *clientMutableEntry) applyActivitySummary() {
	if len(row.activity) == 0 {
		return
	}
	stats := make([]*clientActivityStat, 0, len(row.activity))
	for _, stat := range row.activity {
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Bytes != stats[j].Bytes {
			return stats[i].Bytes > stats[j].Bytes
		}
		if stats[i].Count != stats[j].Count {
			return stats[i].Count > stats[j].Count
		}
		return stats[i].Protocol < stats[j].Protocol
	})
	row.ProtocolMix = make([]string, 0, min(len(stats), 3))
	for _, stat := range stats {
		if len(row.ProtocolMix) >= 3 {
			break
		}
		row.ProtocolMix = append(row.ProtocolMix, stat.Protocol)
	}
	row.PrimaryActivity = classifyClientActivity(stats)
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].LastSeen.After(stats[j].LastSeen)
	})
	row.LastProtocol = stats[0].Protocol
	row.LastProtocolDetail = stats[0].Detail
}

func flowActivityName(flow logstore.TrafficFlow) string {
	return flowActivityProtocol(flow)
}

func flowActivityDetail(flow logstore.TrafficFlow) string {
	app := flowActivityProtocol(flow)
	switch {
	case strings.TrimSpace(flow.TLSSNI) != "":
		return "TLS-SNI=" + strings.TrimSpace(flow.TLSSNI)
	case strings.TrimSpace(flow.HTTPHost) != "":
		return "HTTP-Host=" + strings.TrimSpace(flow.HTTPHost)
	case strings.TrimSpace(flow.DNSQuery) != "":
		if app == "netbios" {
			return "NBNS-query=" + strings.TrimSpace(flow.DNSQuery)
		}
		return "DNS-query=" + strings.TrimSpace(flow.DNSQuery)
	case strings.TrimSpace(flow.ResolvedHostname) != "":
		if app == "netbios" {
			return "NBNS-query=" + strings.TrimSpace(flow.ResolvedHostname)
		}
		if app == "dns" {
			return "DNS-query=" + strings.TrimSpace(flow.ResolvedHostname)
		}
		if app == "http" {
			return "HTTP-Host=" + strings.TrimSpace(flow.ResolvedHostname)
		}
		return "Host=" + strings.TrimSpace(flow.ResolvedHostname)
	case strings.TrimSpace(flow.PeerAddress) != "":
		return strings.TrimSpace(flow.PeerAddress)
	default:
		return ""
	}
}

func flowActivityProtocol(flow logstore.TrafficFlow) string {
	if name := canonicalProtocolAppName(flow.AppName); name != "" && name != "unknown" {
		if protocol := providerActivityProtocol(name, flow); protocol != "" {
			return protocol
		}
		return name
	}
	if strings.TrimSpace(flow.TLSSNI) != "" {
		return "tls"
	}
	switch flow.PeerPort {
	case 53:
		return "dns"
	case 80:
		return "http"
	case 3478, 5349:
		return "stun"
	case 41641:
		return "tailscale"
	case 51820:
		return "wireguard"
	case 443:
		if strings.EqualFold(flow.Protocol, "udp") {
			return "quic"
		}
		return "tls"
	}
	return strings.ToLower(strings.TrimSpace(flow.Protocol))
}

func providerActivityProtocol(app string, flow logstore.TrafficFlow) string {
	switch strings.ToLower(strings.TrimSpace(app)) {
	case "google", "googleservices", "amazonaws", "microsoft", "microsoft365", "azure", "apple", "appleicloud", "applepush", "cloudflare", "nintendo":
		if strings.EqualFold(flow.Protocol, "udp") && flow.PeerPort == 443 {
			return "quic"
		}
		return "tls"
	default:
		return ""
	}
}

func classifyClientActivity(stats []*clientActivityStat) string {
	totalBytes := int64(0)
	totalCount := 0
	seen := map[string]bool{}
	for _, stat := range stats {
		totalBytes += stat.Bytes
		totalCount += stat.Count
		seen[stat.Protocol] = true
	}
	if len(seen) >= 4 {
		return "mixed"
	}
	if seen["netbios"] || seen["mdns"] || seen["ssdp"] {
		return "iot-telemetry"
	}
	if seen["dns"] && len(seen) == 1 {
		return "resolver-only"
	}
	for _, stat := range stats {
		if (stat.Protocol == "tls" || stat.Protocol == "http") && (totalBytes == 0 || stat.Bytes*100 >= totalBytes*60 || stat.Count*100 >= totalCount*60) {
			return "web-heavy"
		}
	}
	return "mixed"
}

type clientMutableEntry struct {
	ClientEntry
	addresses map[string]bool
	sources   map[string]bool
	peers     map[string]bool
	activity  map[string]*clientActivityStat
}

type clientActivityStat struct {
	Protocol string
	Detail   string
	Bytes    int64
	Count    int
	LastSeen time.Time
}

func firewallClientAddress(entry logstore.FirewallLogEntry) string {
	if isLikelyClientAddress(entry.SrcAddress) {
		return entry.SrcAddress
	}
	if isLikelyClientAddress(entry.DstAddress) {
		return entry.DstAddress
	}
	return ""
}

func isLikelyClientAddress(address string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	if addr.Is4() {
		return addr.IsPrivate()
	}
	return addr.IsPrivate() || addr.IsLinkLocalUnicast()
}
