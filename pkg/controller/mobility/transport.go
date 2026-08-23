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
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	"github.com/imksoo/routerd/pkg/samenrollment"
)

const samTransportSourceKind = "SAMTransportProfile"

type TransportController struct {
	Router        *api.Router
	Bus           *bus.Bus
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
			digest, _ := c.upsertTransportDynamicPart(ctx, res, source, "sam-transport", nil, now)
			_ = c.saveTransportStatus(res.Metadata.Name, map[string]any{
				"phase":         "Degraded",
				"reason":        err.Error(),
				"outputDigests": transportOutputDigests(source, digest),
			})
			continue
		}
		source := TransportDynamicSource(res.Metadata.Name, spec.SelfNodeRef)
		desiredSources[source] = true
		degrade := func(cause error) error {
			digest, err := c.upsertTransportDynamicPart(ctx, res, source, "sam-transport", nil, now)
			if err != nil {
				return err
			}
			_ = c.saveTransportStatus(res.Metadata.Name, map[string]any{
				"phase":         "Degraded",
				"reason":        cause.Error(),
				"outputDigests": transportOutputDigests(source, digest),
			})
			return nil
		}
		peerGroupPending := ""
		peerGroupDigest := ""
		peerGroupSource := ""
		if spec.PublishPeerGroup {
			peerGroupSource = TransportPeerGroupDynamicSource(res.Metadata.Name)
			desiredSources[peerGroupSource] = true
			pending, digest, err := c.upsertTransportPeerGroupPart(ctx, res, spec, peerGroupSource, now)
			if err != nil {
				if err := degrade(err); err != nil {
					return err
				}
				continue
			}
			peerGroupPending = pending
			peerGroupDigest = digest
		}
		derived, err := c.deriveTransportResources(ctx, res, spec)
		if err != nil {
			if err := degrade(err); err != nil {
				return err
			}
			continue
		}
		digest, err := c.upsertTransportDynamicPart(ctx, res, source, "sam-transport", derived.Resources, now)
		if err != nil {
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
			"outputDigests":  transportOutputDigests(source, digest, peerGroupSource, peerGroupDigest),
		}
		_ = c.saveTransportStatus(res.Metadata.Name, status)
	}
	return c.deprovisionStaleTransportSources(ctx, desiredSources, now)
}

// transportOutputDigests exposes the durable desired-output identities in
// status so a same-count peer replacement still produces a status event. The
// status is only a wake-up signal; each consumer reloads the part itself.
func transportOutputDigests(values ...string) map[string]string {
	out := map[string]string{}
	for i := 0; i+1 < len(values); i += 2 {
		source, digest := strings.TrimSpace(values[i]), strings.TrimSpace(values[i+1])
		if source != "" && digest != "" {
			out[source] = digest
		}
	}
	return out
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
	timers, convergenceProfile, err := c.transportBGPDefaults(spec.BGP)
	if err != nil {
		return transportDerivation{}, err
	}
	edgeIndex, err := transportAddressSlots(spec, peers, topologyNodes, inner)
	if err != nil {
		// Pair-stable slots are deliberately collision-detected instead of
		// silently moving an existing tunnel. A collision introduced by an
		// optional direct group must not take the established RR topology down,
		// though: discard only the direct accelerator and retry the stable RR
		// peers. A collision among fallback peers remains a hard configuration
		// error, exactly as before.
		fallbackPeers := transportFallbackPeers(peers)
		if len(fallbackPeers) == len(peers) {
			return transportDerivation{}, err
		}
		fallbackSlots, fallbackErr := transportAddressSlots(spec, fallbackPeers, topologyNodes, inner)
		if fallbackErr != nil {
			return transportDerivation{}, err
		}
		peers = fallbackPeers
		edgeIndex = fallbackSlots
		for i := range peerSources {
			if peerSources[i].Phase != "Direct" {
				continue
			}
			peerSources[i].Phase = "Incompatible"
			peerSources[i].PeerCount = 0
			peerSources[i].Reason = "direct peer-group pair-stable address slot collides with fallback topology"
		}
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
			// An RR can legitimately relay the policy-wide /32 range. A direct
			// leaf cannot: it is allowed only the signed /32s projected for this
			// exact peer. Bind that narrow prefix set to the peer's identity and
			// only then give it the higher LOCAL_PREF.
			requirePeerIdentity := spec.BGP.RouteReflectorClient || peer.Direct
			baseImportPolicy := spec.BGP.ImportPolicy
			// A direct profile always has an RR fallback. Older generated configs
			// wrote unchanged here, which left an RR-reflected route with the
			// origin leaf as an unreachable gateway. Normalize that legacy spelling
			// at the effect boundary so an upgraded daemon can start before the
			// management API replaces its persisted YAML.
			if mobilityconfig.SAMTransportHasDirectPeerSource(spec.PeersFrom) {
				baseImportPolicy.NextHopRewrite = "peer-address"
			}
			defaultAllowedPrefixes := mobilityPoolImportPrefixes(c.Router)
			if peer.Direct {
				baseImportPolicy.AllowedPrefixes = append([]string(nil), peer.AllowedPrefixes...)
				defaultAllowedPrefixes = peer.AllowedPrefixes
			}
			importPolicy := transportBGPImportPolicyForPeer(baseImportPolicy, defaultAllowedPrefixes, topologyNodes, peerNode, requirePeerIdentity)
			if peer.Direct {
				importPolicy = directTransportBGPImportPolicy(importPolicy, spec.BGP.DirectLocalPreference)
			}
			annotations := transportAnnotations(owner.Metadata.Name, self, peerNode)
			if peer.Direct {
				annotations[mobilityconfig.SAMTransportDirectPeerAnnotation] = "true"
				if peer.RejectRoutes {
					annotations[mobilityconfig.SAMTransportDirectPeerRejectRoutesAnnotation] = "true"
				}
			}
			out.Resources = append(out.Resources, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
				Metadata: api.ObjectMeta{Name: bgpPeerName, OwnerRefs: ownerRef, Annotations: annotations},
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
					ConvergenceProfile:      convergenceProfile,
					BFD:                     bfdRef,
				},
			})
		}
	}
	sort.Strings(out.PendingSources)
	return out, nil
}

func transportFallbackPeers(peers []api.SAMTransportPeerSpec) []api.SAMTransportPeerSpec {
	fallback := make([]api.SAMTransportPeerSpec, 0, len(peers))
	for _, peer := range peers {
		if peer.Direct {
			continue
		}
		fallback = append(fallback, peer)
	}
	return fallback
}

// transportBGPDefaults materializes the referenced router's peer defaults in
// the generated DynamicConfigPart. This keeps generated SAM peers stable and
// inspectable even before the BGP controller applies them. Profile-local
// settings always win: timers.profile takes precedence over timersPreset, and
// an explicit convergenceProfile takes precedence over the router default.
func (c TransportController) transportBGPDefaults(profile api.SAMTransportBGPProfileSpec) (api.BGPTimersSpec, string, error) {
	timers := profile.Timers
	if strings.TrimSpace(timers.Profile) == "" {
		timers.Profile = strings.TrimSpace(profile.TimersPreset)
	}
	convergenceProfile := strings.TrimSpace(profile.ConvergenceProfile)

	if c.Router == nil {
		return timers, convergenceProfile, nil
	}
	kind, routerName, ok := strings.Cut(strings.TrimSpace(profile.RouterRef), "/")
	if !ok || kind != "BGPRouter" || routerName == "" {
		return timers, convergenceProfile, nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" || strings.TrimSpace(resource.Metadata.Name) != routerName {
			continue
		}
		routerSpec, err := resource.BGPRouterSpec()
		if err != nil {
			return api.BGPTimersSpec{}, "", err
		}
		if strings.TrimSpace(timers.Profile) == "" {
			timers = routerSpec.Timers
		}
		if convergenceProfile == "" {
			convergenceProfile = strings.TrimSpace(routerSpec.ConvergenceProfile)
		}
		break
	}
	return timers, convergenceProfile, nil
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

func transportBGPImportPolicyForPeer(base api.BGPImportPolicySpec, defaultAllowedPrefixes []string, topologyNodeRefs []string, peerNode string, requirePeerIdentity bool) api.BGPImportPolicySpec {
	if !requirePeerIdentity {
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

func directTransportBGPImportPolicy(base api.BGPImportPolicySpec, preference uint32) api.BGPImportPolicySpec {
	base.NextHopRewrite = "peer-address"
	base.LocalPreference = mobilityconfig.EffectiveSAMTransportDirectLocalPreference(preference)
	return base
}

// directPeerGroupOwnedPrefixes validates the runtime counterpart of the
// enrollment direct-peer schema. It is intentionally repeated at the effect
// boundary: dynamic config may have been created by an older or malformed RR.
// A missing or empty map entry is a valid signed no-ownership state and is
// turned into a direct session with a deny-all import policy. Invalid entries
// still degrade the optional accelerator to its RR fallback rather than turn
// it into a broad import policy.
func directPeerGroupOwnedPrefixes(group api.SAMPeerGroupSpec) (map[string][]string, error) {
	nodeRefs := map[string]bool{}
	seenPrefixes := map[string]string{}
	out := make(map[string][]string, len(group.Nodes))
	for _, node := range group.Nodes {
		nodeRef := strings.TrimSpace(node.NodeRef)
		if nodeRef == "" {
			return nil, fmt.Errorf("direct peer-group contains an empty nodeRef")
		}
		nodeRefs[nodeRef] = true
		values := group.OwnedPrefixesByNode[nodeRef]
		out[nodeRef] = nil
		seenForNode := map[string]bool{}
		for _, value := range values {
			prefix, err := samenrollment.ParsePrefixOrAddress(value)
			if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
				return nil, fmt.Errorf("direct peer-group node %s has invalid owned prefix %q", nodeRef, value)
			}
			key := prefix.String()
			if seenForNode[key] {
				return nil, fmt.Errorf("direct peer-group node %s repeats owned prefix %s", nodeRef, key)
			}
			if owner := seenPrefixes[key]; owner != "" && owner != nodeRef {
				return nil, fmt.Errorf("direct peer-group owned prefix %s is assigned to both %s and %s", key, owner, nodeRef)
			}
			seenForNode[key] = true
			seenPrefixes[key] = nodeRef
			out[nodeRef] = append(out[nodeRef], key)
		}
		sort.Strings(out[nodeRef])
	}
	for nodeRef := range group.OwnedPrefixesByNode {
		if !nodeRefs[strings.TrimSpace(nodeRef)] {
			return nil, fmt.Errorf("direct peer-group ownership refers to unknown node %s", nodeRef)
		}
	}
	return out, nil
}

// directPeerGroupIdentityCollision protects the required/forbidden community
// filter emitted for every direct peer. The identity encoding uses a bounded
// standard-community space; if a direct leaf shares an identity value with the
// local leaf, an RR, or another direct leaf, its rule would either be
// self-contradictory or authenticate the wrong node. Reject only the optional
// accelerator and retain the independently valid RR sources.
func directPeerGroupIdentityCollision(self string, fallbackTopology []string, nodes []api.SAMNodeSpec) error {
	directNodes := map[string]bool{}
	allNodes := append([]string{strings.TrimSpace(self)}, fallbackTopology...)
	for _, node := range nodes {
		nodeRef := strings.TrimSpace(node.NodeRef)
		if nodeRef == "" {
			continue
		}
		directNodes[nodeRef] = true
		allNodes = append(allNodes, nodeRef)
	}
	for _, collision := range bgpstate.MobilityNodeIdentityCollisions(allNodes) {
		for _, nodeRef := range collision.NodeRefs {
			if directNodes[nodeRef] {
				return fmt.Errorf("direct peer-group node identity collision %s for %s", collision.Community, strings.Join(collision.NodeRefs, ", "))
			}
		}
	}
	return nil
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
	// Direct groups are an optimization of an already usable RR path. Source
	// order is validated statically, but a fetched RRSet can still be empty or
	// unresolved at runtime. Keep the readiness key policy-scoped so one
	// enrollment domain can never bootstrap another domain's direct group.
	rrFallbackReady := map[string]bool{}
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
			// A direct group must never replace a fallback peer. The direct
			// source is an optional accelerator, whereas the RR source is the
			// only safe recovery path. A later non-direct source can replace an
			// accidentally staged direct collision as an extra safety belt.
			if peers[existing].Direct && !peer.Direct {
				peers[existing] = peer
			}
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
				rrSetPolicyRef string
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
					rrSetPolicyRef = strings.TrimSpace(rrSet.EnrollmentPolicyRef)
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
			if sourceKind == "SAMRRSet" && !source.Optional && rrSetPolicyRef != "" && status.PeerCount > 0 {
				rrFallbackReady[rrSetPolicyRef] = true
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
		addGroupNodes := func(group api.SAMPeerGroupSpec) (bool, error) {
			directOwnedPrefixes := map[string][]string(nil)
			if source.Direct {
				policyRef := strings.TrimSpace(group.EnrollmentPolicyRef)
				if policyRef == "" {
					status.Phase = "Incompatible"
					status.Reason = "direct peersFrom requires an enrollment-scoped SAMPeerGroup"
					return false, nil
				}
				want := mobilityconfig.SAMTransportMeshFingerprint(spec)
				if want == "" || strings.TrimSpace(group.TransportFingerprint) != want {
					status.Phase = "Incompatible"
					status.Reason = "direct peer-group transport fingerprint does not match this SAMTransportProfile"
					return false, nil
				}
				if !rrFallbackReady[policyRef] {
					status.Phase = "Unavailable"
					status.Reason = "matching RR fallback SAMRRSet has no usable peer"
					return false, nil
				}
				var ownedErr error
				directOwnedPrefixes, ownedErr = directPeerGroupOwnedPrefixes(group)
				if ownedErr != nil {
					status.Phase = "Incompatible"
					status.Reason = ownedErr.Error()
					return false, nil
				}
				if identityErr := directPeerGroupIdentityCollision(spec.SelfNodeRef, topology, group.Nodes); identityErr != nil {
					status.Phase = "Incompatible"
					status.Reason = identityErr.Error()
					return false, nil
				}
			}
			status.PeerCount = len(group.Nodes)
			self := strings.TrimSpace(spec.SelfNodeRef)
			selected := map[string]bool{}
			// A direct group is all-or-nothing. It is only an optimization, so a
			// malformed or unresolved later node must not leave an earlier direct
			// peer in the desired set while the source is reported unavailable.
			// Generic peer groups retain their established streaming/fail-static
			// behavior below.
			stagedTopology := []string{}
			stagedPeers := []api.SAMTransportPeerSpec{}
			for _, node := range group.Nodes {
				nodeRef := strings.TrimSpace(node.NodeRef)
				if nodeRef == "" {
					continue
				}
				if source.Direct && node.RouteReflector {
					status.Phase = "Incompatible"
					status.Reason = "direct peer-group contains a route-reflector node"
					return false, nil
				}
				if source.Direct {
					stagedTopology = append(stagedTopology, nodeRef)
				} else {
					addTopology(nodeRef)
				}
				if nodeRef == self || !transportSourceSelectsNode(source, nodeRef) {
					continue
				}
				if source.Direct {
					if _, exists := indexByNode[nodeRef]; exists {
						status.Phase = "Incompatible"
						status.Reason = "direct peer-group overlaps an existing fallback peer: " + nodeRef
						return false, nil
					}
				}
				selected[nodeRef] = true
				endpoint, endpointPending, err := c.samNodeEndpointForTransport(spec, node)
				if err != nil {
					if source.Direct {
						status.Phase = "Incompatible"
						status.Reason = fmt.Sprintf("%s node %s transport endpoint: %v", ref, nodeRef, err)
						return false, nil
					}
					return false, fmt.Errorf("%s node %s transport endpoint: %w", ref, nodeRef, err)
				}
				if endpointPending != "" {
					if source.Direct {
						status.Phase = "Unavailable"
						status.Reason = endpointPending + " not resolved"
						return false, nil
					}
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
					if source.Direct {
						status.Phase = "Incompatible"
						status.Reason = fmt.Sprintf("%s node %s transport endpoint %q: %v", ref, nodeRef, endpoint, err)
						return false, nil
					}
					return false, fmt.Errorf("%s node %s transport endpoint %q: %w", ref, nodeRef, endpoint, err)
				}
				peer := api.SAMTransportPeerSpec{
					NodeRef:        nodeRef,
					RemoteEndpoint: addr.String(),
					Direct:         source.Direct,
				}
				if source.Direct {
					peer.AllowedPrefixes = append([]string(nil), directOwnedPrefixes[nodeRef]...)
					peer.RejectRoutes = len(peer.AllowedPrefixes) == 0
				}
				if source.Direct {
					stagedPeers = append(stagedPeers, peer)
				} else {
					addPeer(peer)
				}
			}
			if source.Direct {
				if len(stagedPeers) == 0 {
					status.Phase = "Unavailable"
					status.Reason = "no eligible remote direct peers"
					return false, nil
				}
				for _, nodeRef := range stagedTopology {
					addTopology(nodeRef)
				}
				for _, peer := range stagedPeers {
					addPeer(peer)
				}
			}
			if !source.Direct {
				if missing := transportSourceMissingNodes(source, selected); len(missing) > 0 {
					return false, fmt.Errorf("selected nodeRefs are not members of %s: %s", ref, strings.Join(missing, ", "))
				}
			}
			return true, nil
		}
		if source.Direct {
			local, localFound, localErr := api.LookupSAMPeerGroup(c.Router, ref, "peersFrom")
			if localErr != nil {
				// The group is an optional accelerator. Even a malformed runtime
				// payload must not prevent the independently resolved RR peers
				// from being reconciled.
				status.Phase = "Incompatible"
				status.Reason = localErr.Error()
				statuses = append(statuses, status)
				continue
			}
			if localFound {
				usable, err := addGroupNodes(local)
				if err != nil {
					status.Phase = "Invalid"
					status.Reason = err.Error()
					statuses = append(statuses, status)
					return nil, nil, statuses, pending, err
				}
				if usable && status.Phase == "Resolved" {
					status.Phase = "Direct"
				}
				statuses = append(statuses, status)
				continue
			}
			status.Phase = "Unavailable"
			status.Reason = "enrollment direct peer group is not present"
			statuses = append(statuses, status)
			continue
		}
		if !source.Optional && c.PeerGroupSync != nil {
			synced, syncedOK, syncErr := c.PeerGroupSync.SyncPeerGroup(ctx, c.Router, spec.UnderlayInterface, groupName)
			if syncedOK {
				status.Phase = "Synced"
				if _, err := addGroupNodes(synced); err != nil {
					status.Phase = "Invalid"
					status.Reason = err.Error()
					statuses = append(statuses, status)
					return nil, nil, statuses, pending, err
				}
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
				if _, err := addGroupNodes(cached); err != nil {
					status.Phase = "Invalid"
					status.Reason = err.Error()
					statuses = append(statuses, status)
					return nil, nil, statuses, pending, err
				}
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
