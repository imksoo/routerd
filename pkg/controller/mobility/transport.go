// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	"github.com/imksoo/routerd/pkg/samenrollment"
)

const samTransportSourceKind = "SAMTransportProfile"

type TransportController struct {
	Router        *api.Router
	Store         Store
	PeerGroupSync *PeerGroupSyncClient
	Now           func() time.Time
	OS            platform.OS
}

type transportDerivation struct {
	Resources      []api.Resource
	PeersFrom      []transportPeersFromStatus
	PendingSources []string
}

type transportPeersFromStatus struct {
	Resource       string   `json:"resource"`
	Phase          string   `json:"phase"`
	PeerCount      int      `json:"peerCount,omitempty"`
	SkippedReasons []string `json:"skippedReasons,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	Warning        string   `json:"warning,omitempty"`
}

func (c TransportController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := controllerNow(c.Now)
	desiredSources := map[string]bool{}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "SAMTransportProfile" {
			continue
		}
		spec, err := res.SAMTransportProfileSpec()
		if err != nil {
			source := TransportDynamicSource(res.Metadata.Name, "")
			desiredSources[source] = true
			_ = c.upsertTransportDynamicPart(res, source, "sam-transport", nil, now)
			_ = c.saveTransportStatus(res.Metadata.Name, map[string]any{
				"phase":  "Degraded",
				"reason": err.Error(),
			})
			continue
		}
		source := TransportDynamicSource(res.Metadata.Name, spec.SelfNodeRef)
		desiredSources[source] = true
		degrade := func(cause error) error {
			if err := c.upsertTransportDynamicPart(res, source, "sam-transport", nil, now); err != nil {
				return err
			}
			_ = c.saveTransportStatus(res.Metadata.Name, map[string]any{
				"phase":  "Degraded",
				"reason": cause.Error(),
			})
			return nil
		}
		peerGroupPending := ""
		if spec.PublishPeerGroup {
			peerGroupSource := TransportPeerGroupDynamicSource(res.Metadata.Name)
			desiredSources[peerGroupSource] = true
			pending, err := c.upsertTransportPeerGroupPart(res, spec, peerGroupSource, now)
			if err != nil {
				if err := degrade(err); err != nil {
					return err
				}
				continue
			}
			peerGroupPending = pending
		}
		derived, err := c.deriveTransportResources(ctx, res, spec)
		if err != nil {
			if err := degrade(err); err != nil {
				return err
			}
			continue
		}
		if err := c.upsertTransportDynamicPart(res, source, "sam-transport", derived.Resources, now); err != nil {
			return err
		}
		pendingSources := cleanStrings(append(append([]string(nil), derived.PendingSources...), peerGroupPending))
		phase := "Derived"
		if len(pendingSources) > 0 {
			phase = "Pending"
		}
		status := map[string]any{
			"phase":          phase,
			"pendingSources": pendingSources,
			"peersFrom":      statusRowMaps(derived.PeersFrom),
		}
		_ = c.saveTransportStatus(res.Metadata.Name, status)
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
	peers, topologyNodes, peerSources, pendingSources, err := c.resolveTransportPeers(ctx, owner, spec)
	if err != nil {
		return transportDerivation{}, err
	}
	if len(peers) == 0 {
		return transportDerivation{PeersFrom: peerSources, PendingSources: pendingSources}, nil
	}
	edgeIndex, err := transportAddressSlots(spec, peers, topologyNodes, inner)
	if err != nil {
		return transportDerivation{}, err
	}
	out := transportDerivation{
		PeersFrom:      peerSources,
		PendingSources: append([]string(nil), pendingSources...),
	}
	for _, peer := range peers {
		peerNode := strings.TrimSpace(peer.NodeRef)
		if peerNode == "" || peerNode == self {
			return transportDerivation{}, fmt.Errorf("invalid peer nodeRef %q", peer.NodeRef)
		}
		index, ok := edgeIndex[sortedEdgeKey(self, peerNode)]
		if !ok {
			return transportDerivation{}, fmt.Errorf("peer %s was not supplied by a topology source", peerNode)
		}
		localPrefix, remoteAddr, err := derivedInnerAddresses(inner, self, peerNode, index)
		if err != nil {
			return transportDerivation{}, fmt.Errorf("peer %s: %w", peerNode, err)
		}
		tunnelName := c.transportTunnelName(spec.Mode, index, owner.Metadata.Name, self, peerNode)
		bgpPeerName := safeName("sam-transport-" + owner.Metadata.Name + "-" + self + "-" + peerNode)
		routeName := safeName("sam-endpoint-" + owner.Metadata.Name + "-" + self + "-" + peerNode)
		underlay := strings.TrimSpace(spec.UnderlayInterface)
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
		if c.targetOS() == platform.OSFreeBSD {
			tunnelSpec.PeerAddress = remoteAddr.String()
		}
		out.Resources = append(out.Resources, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: tunnelName, OwnerRefs: ownerRef, Annotations: transportAnnotations(owner.Metadata.Name, self, peerNode)},
			Spec:     tunnelSpec,
		})
		remoteEndpoint, pending := c.remoteEndpoint(peer)
		if pending != "" {
			out.PendingSources = append(out.PendingSources, pending)
		}
		if remoteEndpoint != "" {
			endpointAddr, err := endpointAddress(remoteEndpoint)
			if err != nil {
				return transportDerivation{}, fmt.Errorf("peer %s remote endpoint %q: %w", peerNode, remoteEndpoint, err)
			}
			out.Resources = append(out.Resources, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
				Metadata: api.ObjectMeta{Name: routeName, OwnerRefs: ownerRef, Annotations: transportAnnotations(owner.Metadata.Name, self, peerNode)},
				Spec: api.IPv4RouteSpec{
					Destination: endpointAddr.String() + "/32",
					Device:      underlay,
				},
			})
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
		}
		if generateBGPPeers {
			importPolicy := transportBGPImportPolicyForPeer(spec.BGP.ImportPolicy, mobilityPoolImportPrefixes(c.Router), topologyNodes, peerNode, spec.BGP.RouteReflectorClient)
			out.Resources = append(out.Resources, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
				Metadata: api.ObjectMeta{Name: bgpPeerName, OwnerRefs: ownerRef, Annotations: transportAnnotations(owner.Metadata.Name, self, peerNode)},
				Spec: api.BGPPeerSpec{
					RouterRef:               strings.TrimSpace(spec.BGP.RouterRef),
					PeerASN:                 spec.BGP.PeerASN,
					Peers:                   []string{remoteAddr.String()},
					PassiveMode:             localPrefix.Addr().Compare(remoteAddr) > 0,
					EbgpMultihop:            spec.BGP.EbgpMultihop,
					RouteReflectorClient:    spec.BGP.RouteReflectorClient,
					RouteReflectorClusterID: strings.TrimSpace(spec.BGP.RouteReflectorClusterID),
					ImportPolicy:            importPolicy,
					ExportPolicy:            spec.BGP.ExportPolicy,
					Timers:                  timers,
					BFD:                     bfdRef,
				},
			})
		}
	}
	sort.Strings(out.PendingSources)
	return out, nil
}

func (c TransportController) targetOS() platform.OS {
	if c.OS != "" {
		return c.OS
	}
	return platform.CurrentOS()
}

func (c TransportController) transportTunnelName(mode string, edgeIndex int, parts ...string) string {
	if c.targetOS() == platform.OSFreeBSD {
		switch strings.TrimSpace(mode) {
		case "ipip":
			return "gif" + strconv.Itoa(edgeIndex)
		case "gre":
			return "gre" + strconv.Itoa(edgeIndex)
		}
	}
	return compactHashedName("samt", parts...)
}

func transportBGPImportPolicyForPeer(base api.BGPImportPolicySpec, defaultAllowedPrefixes []string, topologyNodeRefs []string, peerNode string, routeReflectorClient bool) api.BGPImportPolicySpec {
	if !routeReflectorClient {
		return base
	}
	if len(cleanStrings(base.AllowedPrefixes)) == 0 {
		base.AllowedPrefixes = cleanStrings(defaultAllowedPrefixes)
	}
	base.AllowedPrefixLengthMin = 32
	base.AllowedPrefixLengthMax = 32
	nodeCommunity := bgpstate.MobilityNodeIdentityCommunity(peerNode)
	base.RequiredCommunities = mergeTransportPolicyStrings(base.RequiredCommunities, []string{nodeCommunity})
	var forbidden []string
	for _, nodeRef := range topologyNodeRefs {
		nodeRef = strings.TrimSpace(nodeRef)
		if nodeRef == "" || nodeRef == strings.TrimSpace(peerNode) {
			continue
		}
		if community := bgpstate.MobilityNodeIdentityCommunity(nodeRef); community != "" {
			forbidden = append(forbidden, community)
		}
	}
	base.ForbiddenCommunities = mergeTransportPolicyStrings(base.ForbiddenCommunities, forbidden)
	return base
}

func mergeTransportPolicyStrings(groups ...[]string) []string {
	var values []string
	for _, group := range groups {
		values = append(values, group...)
	}
	return stringutil.UniqueTrimmedSorted(values)
}

func (c TransportController) resolveTransportPeers(ctx context.Context, _ api.Resource, spec api.SAMTransportProfileSpec) ([]api.SAMTransportPeerSpec, []string, []transportPeersFromStatus, []string, error) {
	peers := []api.SAMTransportPeerSpec{}
	indexByNode := map[string]int{}
	topology := []string{}
	topologyIndex := map[string]bool{}
	statuses := make([]transportPeersFromStatus, 0, len(spec.PeersFrom))
	pending := []string{}
	addTopology := func(nodeRef string) {
		nodeRef = strings.TrimSpace(nodeRef)
		if nodeRef == "" || topologyIndex[nodeRef] {
			return
		}
		topologyIndex[nodeRef] = true
		topology = append(topology, nodeRef)
	}
	addPeer := func(peer api.SAMTransportPeerSpec) {
		nodeRef := strings.TrimSpace(peer.NodeRef)
		addTopology(nodeRef)
		if existing, ok := indexByNode[nodeRef]; ok {
			peers[existing] = peer
			return
		}
		indexByNode[nodeRef] = len(peers)
		peers = append(peers, peer)
	}
	for _, source := range spec.PeersFrom {
		ref := strings.TrimSpace(source.Resource)
		status := transportPeersFromStatus{
			Resource: ref,
			Phase:    "Resolved",
		}
		sourceKind, sourceName, ok := strings.Cut(ref, "/")
		if !ok {
			status.Phase = "Invalid"
			status.Reason = "peersFrom resource must reference SAMPeerGroup/<name>, SAMNodeSet/<name>, SAMEnrollmentPolicy/<name>, or SAMRRSet/<name>"
			statuses = append(statuses, status)
			return nil, nil, statuses, pending, fmt.Errorf("%s", status.Reason)
		}
		if sourceKind == "SAMRRSet" || sourceKind == "SAMEnrollmentPolicy" || sourceKind == "SAMNodeSet" {
			var (
				nodeSet        api.SAMNodeSetSpec
				found          bool
				skipped        int
				skippedReasons []string
				err            error
			)
			if sourceKind == "SAMRRSet" {
				rrSet, rrFound, rrErr := api.LookupSAMRRSet(c.Router, ref, "peersFrom")
				if rrErr != nil {
					err = rrErr
				} else if rrFound {
					nodeSet.Nodes = rrSet.Nodes
				}
				found = rrFound
			} else if sourceKind == "SAMEnrollmentPolicy" {
				nodeSet, found, skipped, skippedReasons, err = c.samEnrollmentNodeSet(ref)
			} else {
				nodeSet, found, err = api.LookupSAMNodeSet(c.Router, ref, "peersFrom")
			}
			if err != nil {
				status.Phase = "Invalid"
				status.Reason = err.Error()
				statuses = append(statuses, status)
				return nil, nil, statuses, pending, err
			}
			if !found {
				status.Phase = "Missing"
				status.Reason = sourceKind + " not found"
				statuses = append(statuses, status)
				if !source.Optional {
					pending = append(pending, ref)
				}
				continue
			}
			self := strings.TrimSpace(spec.SelfNodeRef)
			selected := map[string]bool{}
			if sourceKind == "SAMEnrollmentPolicy" {
				// Enrollment status counts every accepted claim, including a
				// self claim that does not produce a transport peer. SAMNodeSet
				// status instead counts only resolved remote peers below.
				status.PeerCount = len(nodeSet.Nodes)
				if skipped > 0 {
					status.Reason = fmt.Sprintf("%d enrollment claims skipped", skipped)
					status.SkippedReasons = append([]string(nil), skippedReasons...)
				}
			}
			for _, node := range nodeSet.Nodes {
				nodeRef := strings.TrimSpace(node.NodeRef)
				if nodeRef == "" {
					continue
				}
				addTopology(nodeRef)
				if nodeRef == self || !transportSourceSelectsNode(source, nodeRef) {
					continue
				}
				selected[nodeRef] = true
				endpoint, endpointPending, err := c.samNodeEndpointForTransport(spec, node)
				if err != nil {
					status.Phase = "Invalid"
					status.Reason = fmt.Sprintf("%s node %s transport endpoint: %v", ref, nodeRef, err)
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
					status.Reason = fmt.Sprintf("%s node %s transport endpoint %q: %v", ref, nodeRef, endpoint, err)
					statuses = append(statuses, status)
					return nil, nil, statuses, pending, fmt.Errorf("%s", status.Reason)
				}
				addPeer(api.SAMTransportPeerSpec{
					NodeRef:        nodeRef,
					RemoteEndpoint: addr.String(),
				})
				if sourceKind != "SAMEnrollmentPolicy" {
					status.PeerCount++
				}
			}
			if missing := transportSourceMissingNodes(source, selected); len(missing) > 0 {
				status.Phase = "Invalid"
				status.Reason = "selected nodeRefs are not members of " + ref + ": " + strings.Join(missing, ", ")
				statuses = append(statuses, status)
				return nil, nil, statuses, pending, fmt.Errorf("%s", status.Reason)
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
		groupName := strings.TrimSpace(sourceName)
		addGroupPeers := func(group api.SAMPeerGroupSpec) {
			status.PeerCount = len(group.Peers)
			for _, peer := range group.Peers {
				if transportSourceSelectsNode(source, peer.NodeRef) {
					addPeer(peer)
				}
			}
		}
		if !source.Optional && c.PeerGroupSync != nil {
			synced, syncedOK, syncErr := c.PeerGroupSync.SyncPeerGroup(ctx, c.Router, spec.UnderlayInterface, groupName)
			if syncedOK {
				status.Phase = "Synced"
				addGroupPeers(synced)
				statuses = append(statuses, status)
				continue
			}
			if syncErr != nil {
				status.Reason = "SAMPeerGroup sync failed: " + syncErr.Error()
			}
		}
		if !source.Optional {
			cached, cacheStatus, cachedOK, cacheErr := c.lastKnownSyncedPeerGroup(groupName)
			if cacheErr != nil {
				status.Phase = "Invalid"
				status.Reason = cacheErr.Error()
				statuses = append(statuses, status)
				return nil, nil, statuses, pending, cacheErr
			}
			if cachedOK {
				status.Phase = "Cached"
				if cacheStatus == "expired" {
					status.Phase = "Stale"
					status.Reason = "using expired last-known-good peer-group-sync dynamic part"
					status.Warning = "publisher TTL expired; generated transport artifacts are fail-static until a fresh SAMPeerGroup is observed"
				}
				addGroupPeers(cached)
				statuses = append(statuses, status)
				continue
			}
		}
		status.Phase = "Missing"
		if status.Reason == "" {
			status.Reason = "SAMPeerGroup has not been synchronized"
		}
		statuses = append(statuses, status)
		if !source.Optional {
			pending = append(pending, ref)
		}
	}
	if len(topology) > 0 && !topologyIndex[strings.TrimSpace(spec.SelfNodeRef)] {
		topology = append([]string{strings.TrimSpace(spec.SelfNodeRef)}, topology...)
		topologyIndex[strings.TrimSpace(spec.SelfNodeRef)] = true
	}
	sort.Strings(pending)
	return peers, topology, statuses, pending, nil
}

func transportSourceSelectsNode(source api.SAMTransportPeersSourceSpec, nodeRef string) bool {
	if len(source.NodeRefs) == 0 {
		return true
	}
	nodeRef = strings.TrimSpace(nodeRef)
	for _, selected := range source.NodeRefs {
		if nodeRef == strings.TrimSpace(selected) {
			return true
		}
	}
	return false
}

func transportSourceMissingNodes(source api.SAMTransportPeersSourceSpec, selected map[string]bool) []string {
	missing := make([]string, 0)
	for _, nodeRef := range source.NodeRefs {
		nodeRef = strings.TrimSpace(nodeRef)
		if !selected[nodeRef] {
			missing = append(missing, nodeRef)
		}
	}
	sort.Strings(missing)
	return missing
}

func (c TransportController) samNodeEndpointForTransport(profile api.SAMTransportProfileSpec, node api.SAMNodeSpec) (string, string, error) {
	if strings.EqualFold(strings.TrimSpace(profile.Encryption), "wireguard") {
		if endpoint := strings.TrimSpace(node.WireGuard.TransportEndpoint); endpoint != "" {
			return endpoint, "", nil
		}
	}
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

func (c TransportController) lastKnownSyncedPeerGroup(name string) (api.SAMPeerGroupSpec, string, bool, error) {
	name = strings.TrimSpace(name)
	resource, status, found, err := latestSyncedMobilityResource(c.Store, PeerGroupSyncDynamicSource(name), "SAMPeerGroup", name, controllerNow(c.Now))
	if err != nil || !found {
		return api.SAMPeerGroupSpec{}, status, found, err
	}
	spec, err := resource.SAMPeerGroupSpec()
	if err != nil {
		return api.SAMPeerGroupSpec{}, status, true, fmt.Errorf("last-known-good SAMPeerGroup/%s spec: %w", name, err)
	}
	return spec, status, true, nil
}

func (c TransportController) samEnrollmentNodeSet(ref string) (api.SAMNodeSetSpec, bool, int, []string, error) {
	policy, found, err := api.LookupSAMEnrollmentPolicy(c.Router, ref, "peersFrom")
	if err != nil || !found {
		return api.SAMNodeSetSpec{}, found, 0, nil, err
	}
	selection, err := samenrollment.ActiveClaims(c.Router.Spec.Resources, ref, policy, controllerNow(c.Now))
	if err != nil {
		return api.SAMNodeSetSpec{}, true, selection.Skipped, selection.SkippedReasons, err
	}
	nodeSet, _ := samenrollment.ActiveClaimNodeSet(selection, policy, samenrollment.ActiveClaimNodeSetOptions{UseClaimEndpoint: true})
	return nodeSet, true, selection.Skipped, selection.SkippedReasons, nil
}
