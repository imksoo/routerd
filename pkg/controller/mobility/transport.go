// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

const samTransportSourceKind = "SAMTransportProfile"

type TransportController struct {
	Router *api.Router
	Store  Store
	Now    func() time.Time
}

type transportPeerStatus struct {
	NodeRef            string `json:"nodeRef"`
	TunnelInterface    string `json:"tunnelInterface"`
	BGPPeer            string `json:"bgpPeer"`
	EndpointRoute      string `json:"endpointRoute,omitempty"`
	LocalInner         string `json:"localInner"`
	RemoteInner        string `json:"remoteInner"`
	UnderlayInterface  string `json:"underlayInterface"`
	RemoteEndpoint     string `json:"remoteEndpoint,omitempty"`
	RemoteEndpointFrom string `json:"remoteEndpointFrom,omitempty"`
}

type transportDerivation struct {
	Resources      []api.Resource
	Peers          []transportPeerStatus
	PendingSources []string
	Tunnels        int
	BGPPeers       int
	EndpointRoutes int
}

func (c TransportController) Reconcile(_ context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := transportNow(c.Now)
	desiredSources := map[string]bool{}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "SAMTransportProfile" {
			continue
		}
		spec, err := res.SAMTransportProfileSpec()
		if err != nil {
			source := TransportDynamicSource(res.Metadata.Name, "")
			desiredSources[source] = true
			_ = c.upsertTransportPart(res, source, nil, now)
			_ = c.saveTransportStatus(res.Metadata.Name, map[string]any{
				"phase":     "Degraded",
				"reason":    err.Error(),
				"updatedAt": now.Format(time.RFC3339Nano),
			})
			continue
		}
		source := TransportDynamicSource(res.Metadata.Name, spec.SelfNodeRef)
		desiredSources[source] = true
		derived, err := c.deriveTransportResources(res, spec)
		if err != nil {
			if upsertErr := c.upsertTransportPart(res, source, nil, now); upsertErr != nil {
				return upsertErr
			}
			_ = c.saveTransportStatus(res.Metadata.Name, map[string]any{
				"phase":         "Degraded",
				"reason":        err.Error(),
				"selfNode":      strings.TrimSpace(spec.SelfNodeRef),
				"dynamicSource": source,
				"updatedAt":     now.Format(time.RFC3339Nano),
			})
			continue
		}
		if err := c.upsertTransportPart(res, source, derived.Resources, now); err != nil {
			return err
		}
		phase := "Derived"
		if len(derived.PendingSources) > 0 {
			phase = "Pending"
		}
		_ = c.saveTransportStatus(res.Metadata.Name, map[string]any{
			"phase":                   phase,
			"selfNode":                strings.TrimSpace(spec.SelfNodeRef),
			"dynamicSource":           source,
			"innerPrefix":             strings.TrimSpace(spec.InnerPrefix),
			"generatedTunnels":        derived.Tunnels,
			"generatedBGPPeers":       derived.BGPPeers,
			"generatedEndpointRoutes": derived.EndpointRoutes,
			"pendingSources":          derived.PendingSources,
			"peers":                   transportPeerStatusMaps(derived.Peers),
			"updatedAt":               now.Format(time.RFC3339Nano),
		})
	}
	return c.deprovisionStaleTransportSources(desiredSources, now)
}

func (c TransportController) deriveTransportResources(owner api.Resource, spec api.SAMTransportProfileSpec) (transportDerivation, error) {
	self := strings.TrimSpace(spec.SelfNodeRef)
	inner, err := netip.ParsePrefix(strings.TrimSpace(spec.InnerPrefix))
	if err != nil {
		return transportDerivation{}, err
	}
	inner = inner.Masked()
	edgeIndex, err := transportAddressSlots(spec, inner)
	if err != nil {
		return transportDerivation{}, err
	}
	var out transportDerivation
	for _, peer := range spec.Peers {
		peerNode := strings.TrimSpace(peer.NodeRef)
		if peerNode == "" || peerNode == self {
			return transportDerivation{}, fmt.Errorf("invalid peer nodeRef %q", peer.NodeRef)
		}
		index, ok := edgeIndex[sortedEdgeKey(self, peerNode)]
		if !ok {
			return transportDerivation{}, fmt.Errorf("peer %s must be listed in topologyNodeRefs", peerNode)
		}
		localPrefix, remoteAddr, err := derivedInnerAddresses(inner, self, peerNode, index, peer.Override)
		if err != nil {
			return transportDerivation{}, fmt.Errorf("peer %s: %w", peerNode, err)
		}
		tunnelName := firstNonEmpty(strings.TrimSpace(peer.Override.TunnelInterface), compactHashedName("samt", owner.Metadata.Name, self, peerNode))
		bgpPeerName := firstNonEmpty(strings.TrimSpace(peer.Override.BGPPeer), safeName("sam-transport-"+owner.Metadata.Name+"-"+self+"-"+peerNode))
		routeName := firstNonEmpty(strings.TrimSpace(peer.Override.EndpointRoute), safeName("sam-endpoint-"+owner.Metadata.Name+"-"+self+"-"+peerNode))
		underlay := firstNonEmpty(strings.TrimSpace(peer.Override.UnderlayInterface), strings.TrimSpace(spec.UnderlayInterface))
		ownerRef := []api.OwnerRef{{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile", Name: owner.Metadata.Name}}
		tunnelSpec := api.TunnelInterfaceSpec{
			Mode:              strings.TrimSpace(spec.Mode),
			Local:             strings.TrimSpace(spec.LocalEndpoint),
			LocalFrom:         spec.LocalEndpointFrom,
			Remote:            strings.TrimSpace(peer.RemoteEndpoint),
			RemoteFrom:        peer.RemoteEndpointFrom,
			Address:           localPrefix.String(),
			UnderlayInterface: underlay,
			TrustedUnderlay:   true,
		}
		out.Resources = append(out.Resources, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: tunnelName, OwnerRefs: ownerRef, Annotations: transportAnnotations(owner.Metadata.Name, self, peerNode)},
			Spec:     tunnelSpec,
		})
		out.Tunnels++
		remoteEndpoint, pending := c.remoteEndpoint(peer)
		if pending != "" {
			out.PendingSources = append(out.PendingSources, pending)
		}
		endpointRoute := ""
		if remoteEndpoint != "" {
			endpointAddr, err := endpointAddress(remoteEndpoint)
			if err != nil {
				return transportDerivation{}, fmt.Errorf("peer %s remote endpoint %q: %w", peerNode, remoteEndpoint, err)
			}
			endpointRoute = routeName
			out.Resources = append(out.Resources, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
				Metadata: api.ObjectMeta{Name: routeName, OwnerRefs: ownerRef, Annotations: transportAnnotations(owner.Metadata.Name, self, peerNode)},
				Spec: api.IPv4RouteSpec{
					Destination: endpointAddr.String() + "/32",
					Device:      underlay,
				},
			})
			out.EndpointRoutes++
		}
		timers := spec.BGP.Timers
		if strings.TrimSpace(timers.Profile) == "" {
			timers.Profile = strings.TrimSpace(spec.BGP.TimersPreset)
		}
		out.Resources = append(out.Resources, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: bgpPeerName, OwnerRefs: ownerRef, Annotations: transportAnnotations(owner.Metadata.Name, self, peerNode)},
			Spec: api.BGPPeerSpec{
				RouterRef:               strings.TrimSpace(spec.BGP.RouterRef),
				PeerASN:                 spec.BGP.PeerASN,
				Peers:                   []string{remoteAddr.String()},
				EbgpMultihop:            spec.BGP.EbgpMultihop,
				RouteReflectorClient:    spec.BGP.RouteReflectorClient,
				RouteReflectorClusterID: strings.TrimSpace(spec.BGP.RouteReflectorClusterID),
				ImportPolicy:            spec.BGP.ImportPolicy,
				ExportPolicy:            spec.BGP.ExportPolicy,
				Timers:                  timers,
			},
		})
		out.BGPPeers++
		out.Peers = append(out.Peers, transportPeerStatus{
			NodeRef:            peerNode,
			TunnelInterface:    tunnelName,
			BGPPeer:            bgpPeerName,
			EndpointRoute:      endpointRoute,
			LocalInner:         localPrefix.String(),
			RemoteInner:        remoteAddr.String(),
			UnderlayInterface:  underlay,
			RemoteEndpoint:     remoteEndpoint,
			RemoteEndpointFrom: strings.TrimSpace(peer.RemoteEndpointFrom.Resource),
		})
	}
	sort.Strings(out.PendingSources)
	return out, nil
}

func transportAddressSlots(spec api.SAMTransportProfileSpec, inner netip.Prefix) (map[string]int, error) {
	switch transportAddressingMode(spec) {
	case "pair-stable":
		return transportPairStableSlots(spec, inner)
	default:
		return transportEdgeIndex(spec)
	}
}

func transportEdgeIndex(spec api.SAMTransportProfileSpec) (map[string]int, error) {
	topology := normalizeTransportTopology(spec)
	if len(topology) < 2 {
		return nil, fmt.Errorf("topologyNodeRefs requires at least two nodes")
	}
	sort.Strings(topology)
	seen := map[string]bool{}
	for _, node := range topology {
		if node == "" {
			return nil, fmt.Errorf("topologyNodeRefs must not contain empty nodeRefs")
		}
		if seen[node] {
			return nil, fmt.Errorf("topologyNodeRefs nodeRef %q is duplicated", node)
		}
		seen[node] = true
	}
	if !seen[strings.TrimSpace(spec.SelfNodeRef)] {
		return nil, fmt.Errorf("selfNodeRef %q must be listed in topologyNodeRefs", spec.SelfNodeRef)
	}
	out := map[string]int{}
	index := 0
	for i := 0; i < len(topology); i++ {
		for j := i + 1; j < len(topology); j++ {
			out[sortedEdgeKey(topology[i], topology[j])] = index
			index++
		}
	}
	return out, nil
}

func transportPairStableSlots(spec api.SAMTransportProfileSpec, inner netip.Prefix) (map[string]int, error) {
	capacity := 1 << (31 - inner.Bits())
	self := strings.TrimSpace(spec.SelfNodeRef)
	out := map[string]int{}
	used := map[int]string{}
	for _, peer := range spec.Peers {
		peerNode := strings.TrimSpace(peer.NodeRef)
		if peerNode == "" || peerNode == self {
			continue
		}
		edgeKey := sortedEdgeKey(self, peerNode)
		if _, exists := out[edgeKey]; exists {
			continue
		}
		slot := stableEdgeSlot(spec, self, peerNode, capacity)
		if previous, conflict := used[slot]; conflict && previous != edgeKey {
			return nil, fmt.Errorf("pair-stable inner /31 slot collision: %s and %s both map to %s; use peer override.localInner/remoteInner or expand spec.innerPrefix",
				describeEdgeKey(previous), describeEdgeKey(edgeKey), innerSlotPrefix(inner, slot))
		}
		used[slot] = edgeKey
		out[edgeKey] = slot
	}
	return out, nil
}

func normalizeTransportTopology(spec api.SAMTransportProfileSpec) []string {
	if len(spec.TopologyNodeRefs) > 0 {
		out := make([]string, 0, len(spec.TopologyNodeRefs))
		for _, node := range spec.TopologyNodeRefs {
			out = append(out, strings.TrimSpace(node))
		}
		return out
	}
	out := []string{strings.TrimSpace(spec.SelfNodeRef)}
	for _, peer := range spec.Peers {
		out = append(out, strings.TrimSpace(peer.NodeRef))
	}
	return out
}

func (c TransportController) remoteEndpoint(peer api.SAMTransportPeerSpec) (string, string) {
	if endpoint := strings.TrimSpace(peer.RemoteEndpoint); endpoint != "" {
		return endpoint, ""
	}
	if strings.TrimSpace(peer.RemoteEndpointFrom.Resource) == "" {
		return "", ""
	}
	value := resourcequery.Value(c.Store, peer.RemoteEndpointFrom)
	if strings.TrimSpace(value) == "" {
		return "", peer.RemoteEndpointFrom.Resource + "." + firstNonEmpty(strings.TrimSpace(peer.RemoteEndpointFrom.Field), "phase")
	}
	return value, ""
}

func (c TransportController) upsertTransportPart(owner api.Resource, source string, resources []api.Resource, now time.Time) error {
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: safeName("sam-transport-" + owner.Metadata.Name),
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.MobilityAPIVersion,
				Kind:       "SAMTransportProfile",
				Name:       owner.Metadata.Name,
			}},
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:      source,
			Generation:  dynamicGeneration,
			ObservedAt:  now,
			ExpiresAt:   now.Add(DefaultLeaseTTL),
			Resources:   append([]api.Resource(nil), resources...),
			Directives:  []dynamicconfig.DynamicConfigDirective{},
			ActionPlans: []dynamicconfig.ActionPlan{},
		},
	}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := dynamicPartRecord(part)
	if err != nil {
		return err
	}
	return c.Store.UpsertDynamicConfigPart(record)
}

func (c TransportController) deprovisionStaleTransportSources(desired map[string]bool, now time.Time) error {
	parts, err := c.Store.ListDynamicConfigParts()
	if err != nil {
		return fmt.Errorf("list dynamic config parts for SAM transport GC: %w", err)
	}
	seen := map[string]bool{}
	for _, part := range parts {
		if !strings.HasPrefix(part.Source, samTransportSourceKind+"/") || desired[part.Source] || seen[part.Source] {
			continue
		}
		seen[part.Source] = true
		profile, self := parseTransportSource(part.Source)
		if err := c.upsertTransportPart(api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
			Metadata: api.ObjectMeta{Name: firstNonEmpty(profile, "deleted-"+self)},
		}, part.Source, nil, now); err != nil {
			return err
		}
	}
	return nil
}

func (c TransportController) saveTransportStatus(profileName string, updates map[string]any) error {
	status := map[string]any{}
	for k, v := range c.Store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", profileName) {
		status[k] = v
	}
	for k, v := range updates {
		status[k] = v
	}
	return c.Store.SaveObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", profileName, status)
}

func TransportDynamicSource(profileName, selfNode string) string {
	return samTransportSourceKind + "/" + strings.TrimSpace(profileName) + "/node/" + strings.TrimSpace(selfNode)
}

func parseTransportSource(source string) (string, string) {
	parts := strings.Split(strings.TrimSpace(source), "/")
	if len(parts) >= 4 && parts[0] == samTransportSourceKind && parts[2] == "node" {
		return parts[1], parts[3]
	}
	return "", ""
}

func sortedEdgeKey(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a <= b {
		return a + "\x00" + b
	}
	return b + "\x00" + a
}

func stableEdgeSlot(spec api.SAMTransportProfileSpec, a, b string, capacity int) int {
	input := strings.TrimSpace(spec.InnerPrefix) + "\x00" + sortedEdgeKey(a, b)
	h := fnv.New64a()
	_, _ = h.Write([]byte(input))
	return int(h.Sum64() % uint64(capacity))
}

func innerSlotPrefix(inner netip.Prefix, slot int) string {
	base, err := addIPv4(inner.Addr(), uint32(slot*2))
	if err != nil {
		return fmt.Sprintf("slot=%d", slot)
	}
	return netip.PrefixFrom(base, 31).String()
}

func describeEdgeKey(key string) string {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return key
	}
	return parts[0] + "<->" + parts[1]
}

func transportAddressingMode(spec api.SAMTransportProfileSpec) string {
	switch strings.TrimSpace(spec.AddressingMode) {
	case "pair-stable":
		return "pair-stable"
	default:
		return "edge-index"
	}
}

func derivedInnerAddresses(inner netip.Prefix, self, peer string, index int, override api.SAMTransportPeerOverrideSpec) (netip.Prefix, netip.Addr, error) {
	if strings.TrimSpace(override.LocalInner) != "" || strings.TrimSpace(override.RemoteInner) != "" {
		local, err := netip.ParsePrefix(strings.TrimSpace(override.LocalInner))
		if err != nil {
			return netip.Prefix{}, netip.Addr{}, err
		}
		remoteValue := strings.TrimSpace(override.RemoteInner)
		if strings.Contains(remoteValue, "/") {
			remotePrefix, err := netip.ParsePrefix(remoteValue)
			if err != nil {
				return netip.Prefix{}, netip.Addr{}, err
			}
			remoteValue = remotePrefix.Addr().String()
		}
		remote, err := netip.ParseAddr(remoteValue)
		if err != nil {
			return netip.Prefix{}, netip.Addr{}, err
		}
		return local.Masked(), remote, nil
	}
	base, err := addIPv4(inner.Masked().Addr(), uint32(index*2))
	if err != nil {
		return netip.Prefix{}, netip.Addr{}, err
	}
	other, err := addIPv4(base, 1)
	if err != nil {
		return netip.Prefix{}, netip.Addr{}, err
	}
	lower, higher := base, other
	if self <= peer {
		return netip.PrefixFrom(lower, 31), higher, nil
	}
	return netip.PrefixFrom(higher, 31), lower, nil
}

func addIPv4(addr netip.Addr, offset uint32) (netip.Addr, error) {
	if !addr.Is4() {
		return netip.Addr{}, fmt.Errorf("address %s is not IPv4", addr)
	}
	bytes := addr.As4()
	n := binary.BigEndian.Uint32(bytes[:])
	binary.BigEndian.PutUint32(bytes[:], n+offset)
	return netip.AddrFrom4(bytes), nil
}

func endpointAddress(value string) (netip.Addr, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Addr{}, err
		}
		if !prefix.Addr().Is4() {
			return netip.Addr{}, fmt.Errorf("must be IPv4")
		}
		return prefix.Addr(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, err
	}
	if !addr.Is4() {
		return netip.Addr{}, fmt.Errorf("must be IPv4")
	}
	return addr, nil
}

func compactHashedName(prefix string, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(h[:])[:11]
}

func transportAnnotations(profile, self, peer string) map[string]string {
	return map[string]string{
		"mobility.routerd.net/transport-profile": profile,
		"mobility.routerd.net/self-node":         self,
		"mobility.routerd.net/peer-node":         peer,
	}
}

func transportPeerStatusMaps(peers []transportPeerStatus) []map[string]any {
	out := make([]map[string]any, 0, len(peers))
	for _, peer := range peers {
		m := map[string]any{
			"nodeRef":           peer.NodeRef,
			"tunnelInterface":   peer.TunnelInterface,
			"bgpPeer":           peer.BGPPeer,
			"localInner":        peer.LocalInner,
			"remoteInner":       peer.RemoteInner,
			"underlayInterface": peer.UnderlayInterface,
		}
		if peer.EndpointRoute != "" {
			m["endpointRoute"] = peer.EndpointRoute
		}
		if peer.RemoteEndpoint != "" {
			m["remoteEndpoint"] = peer.RemoteEndpoint
		}
		if peer.RemoteEndpointFrom != "" {
			m["remoteEndpointFrom"] = peer.RemoteEndpointFrom
		}
		out = append(out, m)
	}
	return out
}

func transportNow(fn func() time.Time) time.Time {
	if fn != nil {
		return fn().UTC()
	}
	return time.Now().UTC()
}
