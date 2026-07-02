// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

const samTransportSourceKind = "SAMTransportProfile"

type TransportController struct {
	Router        *api.Router
	Store         Store
	PeerGroupSync *PeerGroupSyncClient
	Now           func() time.Time
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
	Resources        []api.Resource
	Peers            []transportPeerStatus
	PeersFrom        []transportPeersFromStatus
	TopologyNodeRefs []string
	PendingSources   []string
	Tunnels          int
	BGPPeers         int
	BFDs             int
	EndpointRoutes   int
}

type transportPeersFromStatus struct {
	Resource       string   `json:"resource"`
	Optional       bool     `json:"optional,omitempty"`
	Phase          string   `json:"phase"`
	PeerCount      int      `json:"peerCount,omitempty"`
	SkippedReasons []string `json:"skippedReasons,omitempty"`
	Reason         string   `json:"reason,omitempty"`
}

func (c TransportController) Reconcile(ctx context.Context) error {
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
		peerGroupStatus := map[string]any{"phase": "Disabled"}
		if spec.PublishPeerGroup {
			peerGroupSource := TransportPeerGroupDynamicSource(res.Metadata.Name)
			desiredSources[peerGroupSource] = true
			status, err := c.upsertTransportPeerGroupPart(res, spec, peerGroupSource, now)
			if err != nil {
				if upsertErr := c.upsertTransportPart(res, source, nil, now); upsertErr != nil {
					return upsertErr
				}
				_ = c.saveTransportStatus(res.Metadata.Name, map[string]any{
					"phase":          "Degraded",
					"reason":         err.Error(),
					"selfNode":       strings.TrimSpace(spec.SelfNodeRef),
					"addressingMode": strings.TrimSpace(spec.AddressingMode),
					"dynamicSource":  source,
					"peerGroup":      status,
					"updatedAt":      now.Format(time.RFC3339Nano),
				})
				continue
			}
			peerGroupStatus = status
		}
		derived, err := c.deriveTransportResources(ctx, res, spec)
		if err != nil {
			if upsertErr := c.upsertTransportPart(res, source, nil, now); upsertErr != nil {
				return upsertErr
			}
			_ = c.saveTransportStatus(res.Metadata.Name, map[string]any{
				"phase":          "Degraded",
				"reason":         err.Error(),
				"selfNode":       strings.TrimSpace(spec.SelfNodeRef),
				"addressingMode": strings.TrimSpace(spec.AddressingMode),
				"dynamicSource":  source,
				"peerGroup":      peerGroupStatus,
				"updatedAt":      now.Format(time.RFC3339Nano),
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
			"addressingMode":          firstNonEmpty(strings.TrimSpace(spec.AddressingMode), "edge-index"),
			"dynamicSource":           source,
			"innerPrefix":             strings.TrimSpace(spec.InnerPrefix),
			"generatedTunnels":        derived.Tunnels,
			"generatedBGPPeers":       derived.BGPPeers,
			"generatedBFDs":           derived.BFDs,
			"generatedEndpointRoutes": derived.EndpointRoutes,
			"pendingSources":          derived.PendingSources,
			"peers":                   transportPeerStatusMaps(derived.Peers),
			"peersFrom":               transportPeersFromStatusMaps(derived.PeersFrom),
			"topologyNodeRefs":        append([]string(nil), derived.TopologyNodeRefs...),
			"peerGroup":               peerGroupStatus,
			"updatedAt":               now.Format(time.RFC3339Nano),
		})
	}
	return c.deprovisionStaleTransportSources(desiredSources, now)
}

func (c TransportController) deriveTransportResources(ctx context.Context, owner api.Resource, spec api.SAMTransportProfileSpec) (transportDerivation, error) {
	self := strings.TrimSpace(spec.SelfNodeRef)
	inner, err := netip.ParsePrefix(strings.TrimSpace(spec.InnerPrefix))
	if err != nil {
		return transportDerivation{}, err
	}
	inner = inner.Masked()
	peers, topologyNodeRefs, peerSources, pendingSources, err := c.resolveTransportPeers(ctx, owner, spec)
	if err != nil {
		return transportDerivation{}, err
	}
	spec.Peers = peers
	if len(topologyNodeRefs) > 0 {
		spec.TopologyNodeRefs = topologyNodeRefs
	}
	if len(spec.Peers) == 0 {
		return transportDerivation{PeersFrom: peerSources, TopologyNodeRefs: topologyNodeRefs, PendingSources: pendingSources}, nil
	}
	edgeIndex, err := transportAddressSlots(spec, inner)
	if err != nil {
		return transportDerivation{}, err
	}
	out := transportDerivation{
		PeersFrom:        peerSources,
		TopologyNodeRefs: append([]string(nil), spec.TopologyNodeRefs...),
		PendingSources:   append([]string(nil), pendingSources...),
	}
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
			EncapSport:        spec.EncapSport,
			EncapDport:        spec.EncapDport,
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
		generateBGPPeers := spec.BGP.GeneratePeers == nil || *spec.BGP.GeneratePeers
		timers := spec.BGP.Timers
		if strings.TrimSpace(timers.Profile) == "" {
			timers.Profile = strings.TrimSpace(spec.BGP.TimersPreset)
		}
		bfdRef := ""
		if generateBGPPeers && spec.BGP.BFD.Enabled {
			bfdName := safeName(bgpPeerName + "-bfd")
			bfdRef = "BFD/" + bfdName
			out.Resources = append(out.Resources, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BFD"},
				Metadata: api.ObjectMeta{Name: bfdName, OwnerRefs: ownerRef, Annotations: transportAnnotations(owner.Metadata.Name, self, peerNode)},
				Spec: api.BFDSpec{
					Peer:             "BGPPeer/" + bgpPeerName,
					Interface:        strings.TrimSpace(spec.BGP.BFD.Interface),
					Profile:          strings.TrimSpace(spec.BGP.BFD.Profile),
					MinRx:            strings.TrimSpace(spec.BGP.BFD.MinRx),
					MinTx:            strings.TrimSpace(spec.BGP.BFD.MinTx),
					DetectMultiplier: spec.BGP.BFD.DetectMultiplier,
				},
			})
			out.BFDs++
		}
		if generateBGPPeers {
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
					BFD:                     bfdRef,
				},
			})
			out.BGPPeers++
		}
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

func (c TransportController) resolveTransportPeers(ctx context.Context, _ api.Resource, spec api.SAMTransportProfileSpec) ([]api.SAMTransportPeerSpec, []string, []transportPeersFromStatus, []string, error) {
	peers := []api.SAMTransportPeerSpec{}
	indexByNode := map[string]int{}
	topology := []string{}
	topologyIndex := map[string]bool{}
	statuses := make([]transportPeersFromStatus, 0, len(spec.PeersFrom))
	pending := []string{}
	addPeer := func(peer api.SAMTransportPeerSpec) {
		nodeRef := strings.TrimSpace(peer.NodeRef)
		if existing, ok := indexByNode[nodeRef]; ok {
			peers[existing] = peer
			return
		}
		indexByNode[nodeRef] = len(peers)
		peers = append(peers, peer)
	}
	addTopology := func(nodeRef string) {
		nodeRef = strings.TrimSpace(nodeRef)
		if nodeRef == "" || topologyIndex[nodeRef] {
			return
		}
		topologyIndex[nodeRef] = true
		topology = append(topology, nodeRef)
	}
	for _, source := range spec.PeersFrom {
		ref := strings.TrimSpace(source.Resource)
		status := transportPeersFromStatus{
			Resource: ref,
			Optional: source.Optional,
			Phase:    "Resolved",
		}
		sourceKind, _, ok := strings.Cut(ref, "/")
		if !ok {
			status.Phase = "Invalid"
			status.Reason = "peersFrom resource must reference SAMPeerGroup/<name>, SAMNodeSet/<name>, SAMEnrollmentPolicy/<name>, or SAMRRSet/<name>"
			statuses = append(statuses, status)
			return nil, nil, statuses, pending, fmt.Errorf("%s", status.Reason)
		}
		if sourceKind == "SAMRRSet" {
			rrSet, found, err := c.samRRSet(ref)
			if err != nil {
				status.Phase = "Invalid"
				status.Reason = err.Error()
				statuses = append(statuses, status)
				return nil, nil, statuses, pending, err
			}
			if !found {
				status.Phase = "Missing"
				status.Reason = "SAMRRSet not found"
				statuses = append(statuses, status)
				if !source.Optional {
					pending = append(pending, ref)
				}
				continue
			}
			self := strings.TrimSpace(spec.SelfNodeRef)
			for _, member := range rrSet.Members {
				nodeRef := strings.TrimSpace(member.NodeRef)
				if nodeRef == "" {
					continue
				}
				addTopology(nodeRef)
				if nodeRef == self {
					continue
				}
				addPeer(api.SAMTransportPeerSpec{
					NodeRef:        nodeRef,
					RemoteEndpoint: samRRSetMemberEndpointForTransport(spec, member),
				})
				status.PeerCount++
			}
			statuses = append(statuses, status)
			continue
		}
		if sourceKind == "SAMEnrollmentPolicy" {
			nodeSet, found, skipped, skippedReasons, err := c.samEnrollmentNodeSet(ref)
			if err != nil {
				status.Phase = "Invalid"
				status.Reason = err.Error()
				statuses = append(statuses, status)
				return nil, nil, statuses, pending, err
			}
			if !found {
				status.Phase = "Missing"
				status.Reason = "SAMEnrollmentPolicy not found"
				statuses = append(statuses, status)
				if !source.Optional {
					pending = append(pending, ref)
				}
				continue
			}
			self := strings.TrimSpace(spec.SelfNodeRef)
			status.PeerCount = len(nodeSet.Nodes)
			if skipped > 0 {
				status.Reason = fmt.Sprintf("%d enrollment claims skipped", skipped)
				status.SkippedReasons = append([]string(nil), skippedReasons...)
			}
			for _, node := range nodeSet.Nodes {
				nodeRef := strings.TrimSpace(node.NodeRef)
				if nodeRef == "" {
					continue
				}
				addTopology(nodeRef)
				if nodeRef == self {
					continue
				}
				endpoint, endpointPending, err := c.samNodeEndpoint(node)
				if err != nil {
					status.Phase = "Invalid"
					status.Reason = fmt.Sprintf("%s node %s samEndpoint: %v", ref, nodeRef, err)
					statuses = append(statuses, status)
					return nil, nil, statuses, pending, fmt.Errorf("%s", status.Reason)
				}
				if endpointPending != "" {
					status.Phase = "Pending"
					status.Reason = endpointPending + " not resolved"
					pending = append(pending, endpointPending)
					continue
				}
				if endpoint == "" {
					continue
				}
				addr, err := endpointAddress(endpoint)
				if err != nil {
					status.Phase = "Invalid"
					status.Reason = fmt.Sprintf("%s node %s samEndpoint %q: %v", ref, nodeRef, endpoint, err)
					statuses = append(statuses, status)
					return nil, nil, statuses, pending, fmt.Errorf("%s", status.Reason)
				}
				addPeer(api.SAMTransportPeerSpec{
					NodeRef:        nodeRef,
					RemoteEndpoint: addr.String(),
				})
			}
			statuses = append(statuses, status)
			continue
		}
		if sourceKind == "SAMNodeSet" {
			nodeSet, found, err := c.samNodeSet(ref)
			if err != nil {
				status.Phase = "Invalid"
				status.Reason = err.Error()
				statuses = append(statuses, status)
				return nil, nil, statuses, pending, err
			}
			if !found {
				status.Phase = "Missing"
				status.Reason = "SAMNodeSet not found"
				statuses = append(statuses, status)
				if !source.Optional {
					pending = append(pending, ref)
				}
				continue
			}
			self := strings.TrimSpace(spec.SelfNodeRef)
			for _, node := range nodeSet.Nodes {
				nodeRef := strings.TrimSpace(node.NodeRef)
				if nodeRef == "" {
					continue
				}
				addTopology(nodeRef)
				if nodeRef == self {
					continue
				}
				endpoint, endpointPending, err := c.samNodeEndpoint(node)
				if err != nil {
					status.Phase = "Invalid"
					status.Reason = fmt.Sprintf("%s node %s samEndpoint: %v", ref, nodeRef, err)
					statuses = append(statuses, status)
					return nil, nil, statuses, pending, fmt.Errorf("%s", status.Reason)
				}
				if endpointPending != "" {
					status.Phase = "Pending"
					status.Reason = endpointPending + " not resolved"
					pending = append(pending, endpointPending)
					continue
				}
				if endpoint == "" {
					continue
				}
				addr, err := endpointAddress(endpoint)
				if err != nil {
					status.Phase = "Invalid"
					status.Reason = fmt.Sprintf("%s node %s samEndpoint %q: %v", ref, nodeRef, endpoint, err)
					statuses = append(statuses, status)
					return nil, nil, statuses, pending, fmt.Errorf("%s", status.Reason)
				}
				addPeer(api.SAMTransportPeerSpec{
					NodeRef:        nodeRef,
					RemoteEndpoint: addr.String(),
				})
				status.PeerCount++
			}
			statuses = append(statuses, status)
			continue
		}
		if sourceKind != "SAMPeerGroup" {
			status.Phase = "Invalid"
			status.Reason = "peersFrom resource must reference SAMPeerGroup/<name>, SAMNodeSet/<name>, SAMEnrollmentPolicy/<name>, or SAMRRSet/<name>"
			statuses = append(statuses, status)
			return nil, nil, statuses, pending, fmt.Errorf("%s", status.Reason)
		}
		group, found, err := c.samPeerGroup(ref)
		if err != nil {
			status.Phase = "Invalid"
			status.Reason = err.Error()
			statuses = append(statuses, status)
			return nil, nil, statuses, pending, err
		}
		if !found {
			status.Phase = "Missing"
			status.Reason = "SAMPeerGroup not found"
			if !source.Optional && c.PeerGroupSync != nil {
				groupName := strings.TrimSpace(nameFromPeerGroupRef(ref))
				synced, ok, syncErr := c.PeerGroupSync.SyncPeerGroup(ctx, c.Router, spec.UnderlayInterface, groupName)
				if syncErr != nil {
					status.Reason = "SAMPeerGroup not found; sync failed: " + syncErr.Error()
				}
				if ok {
					status.Phase = "Synced"
					status.Reason = ""
					status.PeerCount = len(synced.Peers)
					for _, peer := range synced.Peers {
						addPeer(peer)
					}
					statuses = append(statuses, status)
					continue
				}
			}
			if !source.Optional {
				groupName := strings.TrimSpace(nameFromPeerGroupRef(ref))
				cached, cacheStatus, ok, cacheErr := c.lastKnownSyncedPeerGroup(groupName)
				if cacheErr != nil {
					status.Phase = "Invalid"
					status.Reason = cacheErr.Error()
					statuses = append(statuses, status)
					return nil, nil, statuses, pending, cacheErr
				}
				if ok {
					status.Phase = "Cached"
					if cacheStatus == "expired" {
						status.Phase = "Stale"
						status.Reason = "using expired last-known-good peer-group-sync dynamic part"
					}
					status.PeerCount = len(cached.Peers)
					for _, peer := range cached.Peers {
						addPeer(peer)
					}
					statuses = append(statuses, status)
					continue
				}
			}
			statuses = append(statuses, status)
			if !source.Optional {
				pending = append(pending, ref)
			}
			continue
		}
		status.PeerCount = len(group.Peers)
		for _, peer := range group.Peers {
			addPeer(peer)
		}
		statuses = append(statuses, status)
	}
	for _, peer := range spec.Peers {
		addPeer(peer)
		addTopology(peer.NodeRef)
	}
	for _, nodeRef := range spec.TopologyNodeRefs {
		addTopology(nodeRef)
	}
	if len(topology) > 0 && !topologyIndex[strings.TrimSpace(spec.SelfNodeRef)] && len(spec.TopologyNodeRefs) == 0 {
		topology = append([]string{strings.TrimSpace(spec.SelfNodeRef)}, topology...)
		topologyIndex[strings.TrimSpace(spec.SelfNodeRef)] = true
	}
	sort.Strings(pending)
	return peers, topology, statuses, pending, nil
}

func samRRSetMemberEndpointForTransport(profile api.SAMTransportProfileSpec, member api.SAMRRSetMember) string {
	if strings.EqualFold(strings.TrimSpace(profile.Encryption), "wireguard") {
		for _, allowedIP := range member.WireGuard.AllowedIPs {
			allowedIP = strings.TrimSpace(allowedIP)
			if allowedIP == "" {
				continue
			}
			if prefix, err := netip.ParsePrefix(allowedIP); err == nil {
				return prefix.Addr().String()
			}
			if addr, err := netip.ParseAddr(allowedIP); err == nil {
				return addr.String()
			}
		}
	}
	return strings.TrimSpace(member.Endpoint)
}

func (c TransportController) samNodeEndpoint(node api.SAMNodeSpec) (string, string, error) {
	if endpoint := strings.TrimSpace(node.SAMEndpoint); endpoint != "" {
		return endpoint, "", nil
	}
	if strings.TrimSpace(node.SAMEndpointFrom.Resource) == "" {
		return "", "", nil
	}
	value := resourcequery.Value(c.Store, node.SAMEndpointFrom)
	if strings.TrimSpace(value) == "" {
		return "", node.SAMEndpointFrom.Resource + "." + firstNonEmpty(strings.TrimSpace(node.SAMEndpointFrom.Field), "phase"), nil
	}
	addr, err := endpointAddress(value)
	if err != nil {
		return "", "", fmt.Errorf("samEndpointFrom %s value %q: %w", node.SAMEndpointFrom.Resource, value, err)
	}
	return addr.String(), "", nil
}

func nameFromPeerGroupRef(ref string) string {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMPeerGroup" {
		return ""
	}
	return strings.TrimSpace(name)
}

func (c TransportController) samPeerGroup(ref string) (api.SAMPeerGroupSpec, bool, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMPeerGroup" || strings.TrimSpace(name) == "" {
		return api.SAMPeerGroupSpec{}, false, fmt.Errorf("peersFrom resource must reference SAMPeerGroup/<name>")
	}
	if c.Router == nil {
		return api.SAMPeerGroupSpec{}, false, nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMPeerGroup" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.SAMPeerGroupSpec()
		if err != nil {
			return api.SAMPeerGroupSpec{}, true, fmt.Errorf("%s spec: %w", ref, err)
		}
		return spec, true, nil
	}
	return api.SAMPeerGroupSpec{}, false, nil
}

func (c TransportController) lastKnownSyncedPeerGroup(name string) (api.SAMPeerGroupSpec, string, bool, error) {
	name = strings.TrimSpace(name)
	resource, status, found, err := latestSyncedMobilityResource(c.Store, PeerGroupSyncDynamicSource(name), "SAMPeerGroup", name, transportNow(c.Now))
	if err != nil || !found {
		return api.SAMPeerGroupSpec{}, status, found, err
	}
	spec, err := resource.SAMPeerGroupSpec()
	if err != nil {
		return api.SAMPeerGroupSpec{}, status, true, fmt.Errorf("last-known-good SAMPeerGroup/%s spec: %w", name, err)
	}
	return spec, status, true, nil
}

func (c TransportController) samNodeSet(ref string) (api.SAMNodeSetSpec, bool, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMNodeSet" || strings.TrimSpace(name) == "" {
		return api.SAMNodeSetSpec{}, false, fmt.Errorf("peersFrom resource must reference SAMNodeSet/<name>")
	}
	if c.Router == nil {
		return api.SAMNodeSetSpec{}, false, nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMNodeSet" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.SAMNodeSetSpec()
		if err != nil {
			return api.SAMNodeSetSpec{}, true, fmt.Errorf("%s spec: %w", ref, err)
		}
		return spec, true, nil
	}
	return api.SAMNodeSetSpec{}, false, nil
}

func (c TransportController) samRRSet(ref string) (api.SAMRRSetSpec, bool, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMRRSet" || strings.TrimSpace(name) == "" {
		return api.SAMRRSetSpec{}, false, fmt.Errorf("peersFrom resource must reference SAMRRSet/<name>")
	}
	if c.Router == nil {
		return api.SAMRRSetSpec{}, false, nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMRRSet" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.SAMRRSetSpec()
		if err != nil {
			return api.SAMRRSetSpec{}, true, fmt.Errorf("%s spec: %w", ref, err)
		}
		return spec, true, nil
	}
	return api.SAMRRSetSpec{}, false, nil
}

func (c TransportController) samEnrollmentNodeSet(ref string) (api.SAMNodeSetSpec, bool, int, []string, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMEnrollmentPolicy" || strings.TrimSpace(name) == "" {
		return api.SAMNodeSetSpec{}, false, 0, nil, fmt.Errorf("peersFrom resource must reference SAMEnrollmentPolicy/<name>")
	}
	if c.Router == nil {
		return api.SAMNodeSetSpec{}, false, 0, nil, nil
	}
	var policy api.SAMEnrollmentPolicySpec
	found := false
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentPolicy" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.SAMEnrollmentPolicySpec()
		if err != nil {
			return api.SAMNodeSetSpec{}, true, 0, nil, fmt.Errorf("%s spec: %w", ref, err)
		}
		policy = spec
		found = true
		break
	}
	if !found {
		return api.SAMNodeSetSpec{}, false, 0, nil, nil
	}
	var nodes []api.SAMNodeSpec
	var skippedReasons []string
	skipped := 0
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" {
			continue
		}
		claim, err := resource.SAMEnrollmentClaimSpec()
		if err != nil {
			return api.SAMNodeSetSpec{}, true, skipped, skippedReasons, err
		}
		_, claimPolicyName, _ := strings.Cut(strings.TrimSpace(claim.PolicyRef), "/")
		if claimPolicyName != strings.TrimSpace(name) {
			continue
		}
		if claim.Revoked {
			skipped++
			skippedReasons = append(skippedReasons, strings.TrimSpace(resource.Metadata.Name)+": revoked")
			continue
		}
		if transportEnrollmentClaimExpired(policy, claim, transportNow(c.Now)) {
			skipped++
			skippedReasons = append(skippedReasons, strings.TrimSpace(resource.Metadata.Name)+": expired")
			continue
		}
		tunnel, err := transportEnrollmentTunnelAddress(claim.TunnelAddress)
		if err != nil || !transportEnrollmentPrefixContains(policy.TunnelAddressPrefixes, tunnel.Addr()) {
			skipped++
			skippedReasons = append(skippedReasons, strings.TrimSpace(resource.Metadata.Name)+": unauthorized tunnel address")
			continue
		}
		endpoint := strings.TrimSpace(claim.Endpoint)
		if endpoint == "" {
			endpoint = tunnel.Addr().String()
		}
		nodes = append(nodes, api.SAMNodeSpec{
			NodeRef:     strings.TrimSpace(claim.LeafID),
			Role:        "cloud",
			SAMEndpoint: endpoint,
		})
	}
	sort.Strings(skippedReasons)
	return api.SAMNodeSetSpec{Nodes: nodes}, true, skipped, skippedReasons, nil
}

func transportEnrollmentClaimExpired(policy api.SAMEnrollmentPolicySpec, claim api.SAMEnrollmentClaimSpec, now time.Time) bool {
	if strings.TrimSpace(policy.TTL) != "" && strings.TrimSpace(claim.JoinTimestamp) != "" {
		ttl, ttlErr := time.ParseDuration(strings.TrimSpace(policy.TTL))
		joinedAt, timeErr := transportEnrollmentTime(claim.JoinTimestamp)
		if ttlErr != nil || timeErr != nil || !joinedAt.Add(ttl).After(now) {
			return true
		}
	}
	if strings.TrimSpace(claim.ExpiresAt) == "" {
		return false
	}
	expiresAt, err := transportEnrollmentTime(claim.ExpiresAt)
	if err != nil {
		return true
	}
	return !expiresAt.After(now)
}

func transportEnrollmentTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}

func transportEnrollmentTunnelAddress(value string) (netip.Prefix, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Prefix{}, err
		}
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, 32), nil
}

func transportEnrollmentPrefixContains(prefixes []string, addr netip.Addr) bool {
	for _, value := range prefixes {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err == nil && prefix.Masked().Contains(addr) {
			return true
		}
	}
	return false
}

func transportAddressSlots(spec api.SAMTransportProfileSpec, inner netip.Prefix) (map[string]int, error) {
	addressingMode, err := transportAddressingMode(spec)
	if err != nil {
		return nil, err
	}
	switch addressingMode {
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
	seedPrefix := inner.Masked().String()
	out := map[string]int{}
	used := map[int]string{}
	reservedAddresses := map[string]string{}
	for _, peer := range spec.Peers {
		peerNode := strings.TrimSpace(peer.NodeRef)
		if peerNode == "" || peerNode == self {
			continue
		}
		edgeKey := sortedEdgeKey(self, peerNode)
		if _, reserved, err := reserveOverrideAddresses(inner, peer.Override, edgeKey, reservedAddresses); err != nil {
			return nil, fmt.Errorf("peer %s override: %w", peerNode, err)
		} else if reserved {
			out[edgeKey] = -1
		}
	}
	for _, peer := range spec.Peers {
		peerNode := strings.TrimSpace(peer.NodeRef)
		if peerNode == "" || peerNode == self {
			continue
		}
		edgeKey := sortedEdgeKey(self, peerNode)
		if _, exists := out[edgeKey]; exists {
			continue
		}
		slot := mobilityconfig.StableSAMTransportSlot(seedPrefix, self, peerNode, capacity)
		slotPrefix, err := mobilityconfig.SAMTransportSlotPrefix(inner, slot)
		if err != nil {
			return nil, fmt.Errorf("pair-stable slot computation failed for %s: %w", describeEdgeKey(edgeKey), err)
		}
		for _, addr := range []string{slotPrefix.Addr().String(), slotPrefix.Addr().Next().String()} {
			if previous := reservedAddresses[addr]; previous != "" && previous != edgeKey {
				return nil, fmt.Errorf("pair-stable inner /31 slot conflict: %s maps to %s which is already reserved by %s; use peer override.localInner/remoteInner or expand spec.innerPrefix",
					describeEdgeKey(edgeKey), slotPrefix, describeEdgeKey(previous))
			}
			reservedAddresses[addr] = edgeKey
		}
		if previous, conflict := used[slot]; conflict && previous != edgeKey {
			return nil, fmt.Errorf("pair-stable inner /31 slot collision: %s and %s both map to %s; use peer override.localInner/remoteInner or expand spec.innerPrefix",
				describeEdgeKey(previous), describeEdgeKey(edgeKey), slotPrefix)
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

func (c TransportController) upsertTransportPeerGroupPart(owner api.Resource, spec api.SAMTransportProfileSpec, source string, now time.Time) (map[string]any, error) {
	status := map[string]any{
		"phase":  "Pending",
		"source": source,
	}
	endpoint, pending, err := c.transportPeerGroupEndpoint(spec)
	if err != nil {
		status["phase"] = "Degraded"
		status["reason"] = err.Error()
		return status, err
	}
	resources := []api.Resource(nil)
	if pending != "" {
		status["pendingSource"] = pending
	} else {
		groupName := owner.Metadata.Name
		resources = append(resources, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
			Metadata: api.ObjectMeta{
				Name: groupName,
				OwnerRefs: []api.OwnerRef{{
					APIVersion: api.MobilityAPIVersion,
					Kind:       "SAMTransportProfile",
					Name:       owner.Metadata.Name,
				}},
			},
			Spec: api.SAMPeerGroupSpec{
				Peers: []api.SAMTransportPeerSpec{{
					NodeRef:        strings.TrimSpace(spec.SelfNodeRef),
					RemoteEndpoint: endpoint,
				}},
			},
		})
		status["phase"] = "Published"
		status["resource"] = "SAMPeerGroup/" + groupName
		status["peerCount"] = 1
	}
	if err := c.upsertPeerGroupPart(owner, source, resources, now); err != nil {
		status["phase"] = "Degraded"
		status["reason"] = err.Error()
		return status, err
	}
	return status, nil
}

func (c TransportController) transportPeerGroupEndpoint(spec api.SAMTransportProfileSpec) (string, string, error) {
	if endpoint := strings.TrimSpace(spec.LocalEndpoint); endpoint != "" {
		addr, err := endpointAddress(endpoint)
		if err != nil {
			return "", "", fmt.Errorf("publishPeerGroup localEndpoint %q: %w", endpoint, err)
		}
		return addr.String(), "", nil
	}
	if strings.TrimSpace(spec.LocalEndpointFrom.Resource) == "" {
		return "", "localEndpoint", nil
	}
	value := resourcequery.Value(c.Store, spec.LocalEndpointFrom)
	if strings.TrimSpace(value) == "" {
		return "", spec.LocalEndpointFrom.Resource + "." + firstNonEmpty(strings.TrimSpace(spec.LocalEndpointFrom.Field), "phase"), nil
	}
	addr, err := endpointAddress(value)
	if err != nil {
		return "", "", fmt.Errorf("publishPeerGroup localEndpointFrom %s value %q: %w", spec.LocalEndpointFrom.Resource, value, err)
	}
	return addr.String(), "", nil
}

func (c TransportController) upsertPeerGroupPart(owner api.Resource, source string, resources []api.Resource, now time.Time) error {
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: safeName("sam-peer-group-" + owner.Metadata.Name),
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
		if profile, ok := parseTransportPeerGroupSource(part.Source); ok {
			if err := c.upsertPeerGroupPart(api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
				Metadata: api.ObjectMeta{Name: firstNonEmpty(profile, "deleted-peer-group")},
			}, part.Source, nil, now); err != nil {
				return err
			}
			continue
		}
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

func TransportPeerGroupDynamicSource(profileName string) string {
	return samTransportSourceKind + "/" + strings.TrimSpace(profileName) + "/peer-group"
}

func parseTransportSource(source string) (string, string) {
	parts := strings.Split(strings.TrimSpace(source), "/")
	if len(parts) >= 4 && parts[0] == samTransportSourceKind && parts[2] == "node" {
		return parts[1], parts[3]
	}
	return "", ""
}

func parseTransportPeerGroupSource(source string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(source), "/")
	if len(parts) == 3 && parts[0] == samTransportSourceKind && parts[2] == "peer-group" {
		return parts[1], true
	}
	return "", false
}

func sortedEdgeKey(a, b string) string {
	return mobilityconfig.SAMTransportPairKey(a, b)
}

func describeEdgeKey(key string) string {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return key
	}
	return parts[0] + "<->" + parts[1]
}

func transportAddressingMode(spec api.SAMTransportProfileSpec) (string, error) {
	mode := mobilityconfig.NormalizeSAMTransportAddressingMode(spec.AddressingMode)
	if mode == "" {
		return "", fmt.Errorf("unsupported addressingMode %q", strings.TrimSpace(spec.AddressingMode))
	}
	return mode, nil
}

func reserveOverrideAddresses(inner netip.Prefix, override api.SAMTransportPeerOverrideSpec, edgeKey string, reserved map[string]string) (netip.Prefix, bool, error) {
	localSet := strings.TrimSpace(override.LocalInner) != ""
	remoteSet := strings.TrimSpace(override.RemoteInner) != ""
	if !localSet && !remoteSet {
		return netip.Prefix{}, false, nil
	}
	if localSet != remoteSet {
		return netip.Prefix{}, false, fmt.Errorf("override.localInner and override.remoteInner must be set together")
	}
	local, err := netip.ParsePrefix(strings.TrimSpace(override.LocalInner))
	if err != nil {
		return netip.Prefix{}, false, fmt.Errorf("invalid override.localInner: %w", err)
	}
	local = local.Masked()
	if !local.Addr().Is4() || local.Bits() != 31 || !inner.Contains(local.Addr()) {
		return netip.Prefix{}, false, fmt.Errorf("override.localInner must be an IPv4 /31 inside spec.innerPrefix")
	}
	remoteText := strings.TrimSpace(override.RemoteInner)
	if strings.Contains(remoteText, "/") {
		remotePrefix, err := netip.ParsePrefix(remoteText)
		if err != nil {
			return netip.Prefix{}, false, fmt.Errorf("invalid override.remoteInner: %w", err)
		}
		remoteText = remotePrefix.Addr().String()
	}
	remote, err := netip.ParseAddr(remoteText)
	if err != nil || !remote.Is4() {
		return netip.Prefix{}, false, fmt.Errorf("override.remoteInner must be an IPv4 address")
	}
	if !local.Contains(remote) || remote == local.Addr() {
		return netip.Prefix{}, false, fmt.Errorf("override.remoteInner must be the other address in override.localInner")
	}
	for _, addr := range []string{local.Addr().String(), remote.String()} {
		if previous := reserved[addr]; previous != "" && previous != edgeKey {
			return netip.Prefix{}, false, fmt.Errorf("override inner address %s conflicts with %s", addr, describeEdgeKey(previous))
		}
		reserved[addr] = edgeKey
	}
	return local, true, nil
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

func transportPeersFromStatusMaps(statuses []transportPeersFromStatus) []map[string]any {
	out := make([]map[string]any, 0, len(statuses))
	for _, status := range statuses {
		m := map[string]any{
			"resource": status.Resource,
			"phase":    status.Phase,
		}
		if status.Optional {
			m["optional"] = true
		}
		if status.PeerCount > 0 {
			m["peerCount"] = status.PeerCount
		}
		if strings.TrimSpace(status.Reason) != "" {
			m["reason"] = status.Reason
		}
		if len(status.SkippedReasons) > 0 {
			m["skippedReasons"] = append([]string(nil), status.SkippedReasons...)
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
