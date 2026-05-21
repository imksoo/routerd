// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"sort"
	"strings"
	"time"

	gobgpapi "github.com/osrg/gobgp/v3/api"
	gobgpserver "github.com/osrg/gobgp/v3/pkg/server"
	"google.golang.org/protobuf/types/known/anypb"

	routerapi "routerd/pkg/api"
	bgpstate "routerd/pkg/bgp"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type GoBGPServer interface {
	Serve()
	Stop()
	StartBgp(context.Context, *gobgpapi.StartBgpRequest) error
	StopBgp(context.Context, *gobgpapi.StopBgpRequest) error
	AddPeer(context.Context, *gobgpapi.AddPeerRequest) error
	DeletePeer(context.Context, *gobgpapi.DeletePeerRequest) error
	ListPeer(context.Context, *gobgpapi.ListPeerRequest, func(*gobgpapi.Peer)) error
	AddPath(context.Context, *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error)
	DeletePath(context.Context, *gobgpapi.DeletePathRequest) error
	ListPath(context.Context, *gobgpapi.ListPathRequest, func(*gobgpapi.Destination)) error
}

type FIBSyncer interface {
	SyncBGP(ctx context.Context, routes []FIBRoute) (FIBSyncResult, error)
}

type FIBRoute struct {
	Prefix   string
	NextHops []string
}

type FIBSyncResult struct {
	Installed   map[string]bool
	Unsupported map[string]string
}

const MinPollInterval = 3 * time.Second

type Controller struct {
	Router *routerapi.Router
	Bus    *bus.Bus
	Store  Store
	DryRun bool
	Logger *slog.Logger

	Server    GoBGPServer
	NewServer func() GoBGPServer
	FIB       FIBSyncer

	MaxPrefixes int

	started         bool
	globalKey       string
	desiredPeerKeys map[string]desiredPeer
	pathUUIDs       map[string][]byte
	observed        bool
	lastState       bgpstate.State
	peerEvents      map[string]time.Time
}

type desiredPeer struct {
	Address  string
	ASN      uint32
	Password string
	Timers   routerapi.BGPTimersSpec
}

func (c *Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil || !hasBGP(c.Router) {
		return nil
	}
	routers := c.bgpRouters()
	if len(routers) == 0 {
		return nil
	}
	if len(routers) > 1 {
		err := fmt.Errorf("embedded GoBGP MVP supports one BGPRouter per routerd process; found %d", len(routers))
		return c.savePendingAll("GoBGPMultipleRoutersUnsupported", err)
	}
	routerResource := routers[0]
	routerSpec, err := routerResource.BGPRouterSpec()
	if err != nil {
		return err
	}
	if strings.TrimSpace(routerSpec.VRF) != "" {
		err := fmt.Errorf("embedded GoBGP MVP does not yet support BGPRouter.spec.vrf")
		return c.savePendingAll("GoBGPVRFUnsupported", err)
	}
	if c.usesBFD() {
		err := fmt.Errorf("embedded GoBGP MVP does not yet support BFD; remove BGPPeer.spec.bfd and BFD resources or keep using a release with the FRR backend")
		return c.savePendingAll("GoBGPBFDUnsupported", err)
	}
	if c.DryRun {
		return c.saveServeManagedStatuses("Planned", false, map[string]any{
			"reason":    "GoBGPServeManaged",
			"applyWith": "routerd serve",
		})
	}
	if err := c.ensureServer(ctx, routerSpec); err != nil {
		return c.savePendingAll("GoBGPStartFailed", err)
	}
	desired, err := c.desiredPeers(routerResource.Metadata.Name)
	if err != nil {
		return c.savePendingAll("GoBGPPeerConfigInvalid", err)
	}
	changed, err := c.reconcilePeers(ctx, desired)
	if err != nil {
		return c.savePendingAll("GoBGPPeerApplyFailed", err)
	}
	if err := c.reconcileAdvertisements(ctx, routerSpec); err != nil {
		return c.savePendingAll("GoBGPPathApplyFailed", err)
	}
	allowedImportPrefixes := importAllowedPrefixes(routerSpec)
	state, routes, err := c.observeState(ctx, allowedImportPrefixes)
	if err != nil {
		return c.savePendingAll("GoBGPObserveFailed", err)
	}
	if c.FIB == nil {
		c.FIB = defaultFIBSyncer()
	}
	fibResult, err := c.FIB.SyncBGP(ctx, routes)
	if err != nil {
		return c.savePendingAll("GoBGPFIBSyncFailed", err)
	}
	state = applyFIBResult(state, routes, fibResult)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	state.Peers = c.applyPeerHistory(state.Peers, now)
	var events []bgpstate.Event
	if c.observed {
		events = bgpstate.Diff(c.lastState, state)
	}
	c.lastState = state
	c.observed = true
	if err := c.saveObservedStatuses(routerResource.Metadata.Name, state, changed, fibResult); err != nil {
		return err
	}
	for _, event := range events {
		c.publishBGPEvent(ctx, event)
	}
	return nil
}

func (c *Controller) ensureServer(ctx context.Context, spec routerapi.BGPRouterSpec) error {
	key := bgpGlobalKey(spec)
	if c.started && c.globalKey == key {
		return nil
	}
	if c.started && c.Server != nil {
		c.Server.Stop()
		c.Server = nil
		c.started = false
	}
	if c.Server == nil {
		if c.NewServer != nil {
			c.Server = c.NewServer()
		} else {
			c.Server = gobgpserver.NewBgpServer()
		}
		go c.Server.Serve()
	}
	req := &gobgpapi.StartBgpRequest{Global: &gobgpapi.Global{
		Asn:              spec.ASN,
		RouterId:         strings.TrimSpace(spec.RouterID),
		ListenPort:       int32(bgpListenPort(spec.Listen)),
		ListenAddresses:  bgpListenAddresses(spec.Listen),
		Families:         []uint32{0}, // GoBGP API uses OpenConfig AFI-SAFI type indexes: 0 = ipv4-unicast.
		UseMultiplePaths: true,
	}}
	if c.bgpRouterUsesIPv6(spec) {
		req.Global.Families = append(req.Global.Families, 1) // 1 = ipv6-unicast.
	}
	if gr := gobgpGracefulRestart(spec); gr != nil {
		req.Global.GracefulRestart = gr
	}
	if err := c.Server.StartBgp(ctx, req); err != nil {
		return err
	}
	c.started = true
	c.globalKey = key
	c.desiredPeerKeys = nil
	c.pathUUIDs = map[string][]byte{}
	return nil
}

func (c *Controller) desiredPeers(routerName string) (map[string]desiredPeer, error) {
	out := map[string]desiredPeer{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		spec, err := resource.BGPPeerSpec()
		if err != nil {
			return nil, err
		}
		_, name, ok := strings.Cut(strings.TrimSpace(spec.RouterRef), "/")
		if !ok || name != routerName {
			continue
		}
		password, err := secretValue(spec.Password, spec.PasswordFrom)
		if err != nil {
			return nil, fmt.Errorf("%s/%s passwordFrom: %w", resource.Kind, resource.Metadata.Name, err)
		}
		for _, peer := range spec.Peers {
			peer = strings.TrimSpace(peer)
			out[peer] = desiredPeer{Address: peer, ASN: spec.PeerASN, Password: password, Timers: spec.Timers}
		}
	}
	return out, nil
}

func (c *Controller) reconcilePeers(ctx context.Context, desired map[string]desiredPeer) (bool, error) {
	if c.desiredPeerKeys == nil {
		c.desiredPeerKeys = map[string]desiredPeer{}
	}
	live := map[string]*gobgpapi.Peer{}
	if err := c.Server.ListPeer(ctx, &gobgpapi.ListPeerRequest{}, func(peer *gobgpapi.Peer) {
		address := peerAddress(peer)
		if address != "" {
			live[address] = peer
		}
	}); err != nil {
		return false, err
	}
	changed := false
	for address, current := range live {
		if _, ok := desired[address]; !ok {
			if err := c.Server.DeletePeer(ctx, &gobgpapi.DeletePeerRequest{Address: address}); err != nil {
				return changed, err
			}
			delete(c.desiredPeerKeys, address)
			changed = true
			continue
		}
		if desiredPeerMatches(current, desired[address]) && c.desiredPeerKeys[address] == desired[address] {
			continue
		}
		if err := c.Server.DeletePeer(ctx, &gobgpapi.DeletePeerRequest{Address: address}); err != nil {
			return changed, err
		}
		delete(c.desiredPeerKeys, address)
		changed = true
	}
	for address, peer := range desired {
		if _, ok := live[address]; ok && c.desiredPeerKeys[address] == peer {
			continue
		}
		if err := c.Server.AddPeer(ctx, &gobgpapi.AddPeerRequest{Peer: goBGPPeer(peer)}); err != nil {
			return changed, err
		}
		c.desiredPeerKeys[address] = peer
		changed = true
	}
	return changed, nil
}

func (c *Controller) reconcileAdvertisements(ctx context.Context, spec routerapi.BGPRouterSpec) error {
	desired := advertisedPrefixes(spec)
	if c.pathUUIDs == nil {
		c.pathUUIDs = map[string][]byte{}
	}
	for prefix := range c.pathUUIDs {
		if !desired[prefix] {
			if err := c.Server.DeletePath(ctx, &gobgpapi.DeletePathRequest{TableType: gobgpapi.TableType_GLOBAL, Uuid: c.pathUUIDs[prefix]}); err != nil {
				return err
			}
			delete(c.pathUUIDs, prefix)
		}
	}
	for prefix := range desired {
		if len(c.pathUUIDs[prefix]) > 0 {
			continue
		}
		path, err := localPath(prefix)
		if err != nil {
			return err
		}
		resp, err := c.Server.AddPath(ctx, &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_GLOBAL, Path: path})
		if err != nil {
			return err
		}
		c.pathUUIDs[prefix] = resp.GetUuid()
	}
	return nil
}

func (c *Controller) observeState(ctx context.Context, allowedImportPrefixes []netip.Prefix) (bgpstate.State, []FIBRoute, error) {
	var state bgpstate.State
	var routes []FIBRoute
	if err := c.Server.ListPeer(ctx, &gobgpapi.ListPeerRequest{EnableAdvertised: true}, func(peer *gobgpapi.Peer) {
		state.Peers = append(state.Peers, statePeer(peer))
	}); err != nil {
		return bgpstate.State{}, nil, err
	}
	for _, family := range bgpFamiliesForRouter(c.Router) {
		err := c.Server.ListPath(ctx, &gobgpapi.ListPathRequest{TableType: gobgpapi.TableType_GLOBAL, Family: family}, func(dst *gobgpapi.Destination) {
			state.Prefixes = append(state.Prefixes, statePrefixes(dst)...)
			routes = append(routes, fibRoutesFromDestination(dst, allowedImportPrefixes)...)
		})
		if err != nil {
			return bgpstate.State{}, nil, err
		}
	}
	routes = mergeFIBRoutes(routes)
	limited, truncated := bgpstate.LimitPrefixes(bgpstate.Normalize(state), c.maxPrefixes())
	if truncated {
		limited.Prefixes = append(limited.Prefixes, bgpstate.Prefix{Prefix: "truncated", SelectionReason: "prefix limit reached"})
	}
	return bgpstate.Normalize(limited), routes, nil
}

func (c *Controller) saveObservedStatuses(routerName string, state bgpstate.State, changed bool, fibResult FIBSyncResult) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	peersByResource := c.peersByResource(state)
	fibRoutes := fibInstalledCount(fibResult)
	fibUnsupported := fibUnsupportedCount(fibResult)
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.NetAPIVersion {
			continue
		}
		switch resource.Kind {
		case "BGPRouter":
			if resource.Metadata.Name != routerName {
				continue
			}
			established := establishedPeers(state.Peers)
			phase := "Pending"
			if len(state.Peers) > 0 && established == len(state.Peers) {
				phase = "Established"
			} else if established > 0 {
				phase = "Degraded"
			} else if len(state.Peers) > 0 {
				phase = "Down"
			}
			if fibUnsupported > 0 && phase == "Established" {
				phase = "Degraded"
			}
			status := map[string]any{
				"phase":                phase,
				"backend":              "gobgp",
				"applyWith":            "embedded gobgp API",
				"changed":              changed,
				"dryRun":               c.DryRun,
				"peers":                state.Peers,
				"prefixes":             state.Prefixes,
				"observedCommunities":  observedCommunities(state.Prefixes),
				"establishedPeers":     established,
				"acceptedPrefixes":     len(state.Prefixes),
				"fibRoutes":            fibRoutes,
				"fibUnsupportedRoutes": fibUnsupported,
				"observedAt":           now,
				"conditions":           []map[string]any{{"type": "Observed", "status": "True", "reason": "GoBGPStatus"}},
			}
			if fibUnsupported > 0 {
				status["reason"] = "GoBGPFIBPartial"
				status["pendingReason"] = "GoBGPFIBPartial"
				status["conditions"] = append(status["conditions"].([]map[string]any), map[string]any{
					"type":    "KernelFIB",
					"status":  "False",
					"reason":  "GoBGPFIBPartial",
					"message": fmt.Sprintf("%d imported BGP prefix(es) could not be installed into the kernel FIB", fibUnsupported),
				})
			}
			if err := c.Store.SaveObjectStatus(routerapi.NetAPIVersion, "BGPRouter", resource.Metadata.Name, status); err != nil {
				return err
			}
		case "BGPPeer":
			peers := peersByResource[resource.Metadata.Name]
			established := establishedPeers(peers)
			phase := "Pending"
			if len(peers) > 0 && established == len(peers) {
				phase = "Established"
			} else if established > 0 {
				phase = "Degraded"
			} else if len(peers) > 0 {
				phase = "Down"
			}
			status := map[string]any{
				"phase":            phase,
				"backend":          "gobgp",
				"applyWith":        "embedded gobgp API",
				"changed":          changed,
				"dryRun":           c.DryRun,
				"peers":            peers,
				"establishedPeers": established,
				"observedAt":       now,
			}
			if err := c.Store.SaveObjectStatus(routerapi.NetAPIVersion, "BGPPeer", resource.Metadata.Name, status); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) saveServeManagedStatuses(phase string, changed bool, extra map[string]any) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.NetAPIVersion || (resource.Kind != "BGPRouter" && resource.Kind != "BGPPeer" && resource.Kind != "BFD") {
			continue
		}
		status := map[string]any{
			"phase":      phase,
			"backend":    "gobgp",
			"applyWith":  "routerd serve",
			"changed":    changed,
			"dryRun":     c.DryRun,
			"observedAt": now,
		}
		for key, value := range extra {
			status[key] = value
		}
		if err := c.Store.SaveObjectStatus(routerapi.NetAPIVersion, resource.Kind, resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) savePendingAll(reason string, err error) error {
	status := map[string]any{
		"reason":        "GoBGPConfigPending",
		"pendingReason": reason,
		"error":         err.Error(),
		"conditions": []map[string]any{{
			"type":    "Configured",
			"status":  "False",
			"reason":  "GoBGPConfigPending",
			"message": reason,
		}},
	}
	if saveErr := c.saveServeManagedStatuses("Pending", false, status); saveErr != nil {
		return saveErr
	}
	return fmt.Errorf("%s: %w", reason, err)
}

func (c *Controller) bgpRouters() []routerapi.Resource {
	var out []routerapi.Resource
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == routerapi.NetAPIVersion && resource.Kind == "BGPRouter" {
			out = append(out, resource)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
	return out
}

func (c *Controller) usesBFD() bool {
	if c.Router == nil {
		return false
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.NetAPIVersion {
			continue
		}
		switch resource.Kind {
		case "BFD":
			return true
		case "BGPPeer":
			spec, err := resource.BGPPeerSpec()
			if err == nil && strings.TrimSpace(spec.BFD) != "" {
				return true
			}
		}
	}
	return false
}

func bgpGlobalKey(spec routerapi.BGPRouterSpec) string {
	return fmt.Sprintf("%d|%s|%s|%d|%t", spec.ASN, strings.TrimSpace(spec.RouterID), strings.TrimSpace(spec.Listen.Address), bgpListenPort(spec.Listen), cBool(spec.GracefulRestart.Enabled))
}

func bgpListenPort(spec routerapi.BGPListenSpec) int {
	if spec.Port > 0 {
		return spec.Port
	}
	return 179
}

func bgpListenAddresses(spec routerapi.BGPListenSpec) []string {
	if strings.TrimSpace(spec.Address) == "" {
		return nil
	}
	return []string{strings.TrimSpace(spec.Address)}
}

func goBGPPeer(peer desiredPeer) *gobgpapi.Peer {
	return &gobgpapi.Peer{
		Conf: &gobgpapi.PeerConf{
			NeighborAddress: peer.Address,
			PeerAsn:         peer.ASN,
			AuthPassword:    peer.Password,
			Type:            gobgpapi.PeerType_EXTERNAL,
			SendCommunity:   3,
		},
		Timers: &gobgpapi.Timers{Config: goBGPTimers(peer.Timers)},
		AfiSafis: []*gobgpapi.AfiSafi{
			goBGPAFISAFI(ipv4Family()),
			goBGPAFISAFI(ipv6Family()),
		},
	}
}

func goBGPAFISAFI(family *gobgpapi.Family) *gobgpapi.AfiSafi {
	return &gobgpapi.AfiSafi{
		Config: &gobgpapi.AfiSafiConfig{Family: family, Enabled: true},
		UseMultiplePaths: &gobgpapi.UseMultiplePaths{
			Config: &gobgpapi.UseMultiplePathsConfig{Enabled: true},
			Ebgp:   &gobgpapi.Ebgp{Config: &gobgpapi.EbgpConfig{MaximumPaths: 16}},
		},
	}
}

func goBGPTimers(spec routerapi.BGPTimersSpec) *gobgpapi.TimersConfig {
	switch strings.TrimSpace(spec.Profile) {
	case "fast":
		return &gobgpapi.TimersConfig{ConnectRetry: 1, HoldTime: 9, KeepaliveInterval: 3, IdleHoldTimeAfterReset: 1}
	case "slow":
		return &gobgpapi.TimersConfig{ConnectRetry: 30, HoldTime: 180, KeepaliveInterval: 60, IdleHoldTimeAfterReset: 5}
	default:
		return &gobgpapi.TimersConfig{ConnectRetry: 10, HoldTime: 90, KeepaliveInterval: 30, IdleHoldTimeAfterReset: 1}
	}
}

func gobgpGracefulRestart(spec routerapi.BGPRouterSpec) *gobgpapi.GracefulRestart {
	enabled := true
	if spec.ConvergenceProfile == "fast" {
		enabled = false
	}
	if spec.GracefulRestart.Enabled != nil {
		enabled = *spec.GracefulRestart.Enabled
	}
	if !enabled {
		return nil
	}
	return &gobgpapi.GracefulRestart{Enabled: true, RestartTime: uint32(durationSeconds(spec.GracefulRestart.RestartTime, 120)), StaleRoutesTime: uint32(durationSeconds(spec.GracefulRestart.StalePathTime, 360))}
}

func desiredPeerMatches(peer *gobgpapi.Peer, desired desiredPeer) bool {
	conf := peer.GetConf()
	return conf.GetNeighborAddress() == desired.Address &&
		conf.GetPeerAsn() == desired.ASN &&
		conf.GetAuthPassword() == desired.Password
}

func peerAddress(peer *gobgpapi.Peer) string {
	if address := strings.TrimSpace(peer.GetConf().GetNeighborAddress()); address != "" {
		return address
	}
	return strings.TrimSpace(peer.GetState().GetNeighborAddress())
}

func statePeer(peer *gobgpapi.Peer) bgpstate.Peer {
	state := peer.GetState()
	session := state.GetSessionState().String()
	prefixes := 0
	for _, af := range peer.GetAfiSafis() {
		prefixes += int(af.GetState().GetAccepted())
	}
	messagesReceived, messagesSent := 0, 0
	if messages := state.GetMessages(); messages != nil {
		messagesReceived = int(messages.GetReceived().GetTotal())
		messagesSent = int(messages.GetSent().GetTotal())
	}
	return bgpstate.Peer{
		Address:          firstNonEmpty(peerAddress(peer), state.GetNeighborAddress()),
		ASN:              firstNonZero(state.GetPeerAsn(), peer.GetConf().GetPeerAsn()),
		State:            session,
		Established:      state.GetSessionState() == gobgpapi.PeerState_ESTABLISHED,
		PrefixesReceived: prefixes,
		MessagesReceived: messagesReceived,
		MessagesSent:     messagesSent,
	}
}

func statePrefixes(dst *gobgpapi.Destination) []bgpstate.Prefix {
	var out []bgpstate.Prefix
	for _, path := range dst.GetPaths() {
		if path.GetIsWithdraw() {
			continue
		}
		prefix := firstNonEmpty(dst.GetPrefix(), pathPrefix(path))
		if prefix == "" {
			continue
		}
		out = append(out, bgpstate.Prefix{
			Prefix:      prefix,
			NextHop:     pathNextHop(path),
			Best:        path.GetBest(),
			Valid:       !path.GetIsNexthopInvalid(),
			Installed:   path.GetBest() && !path.GetIsNexthopInvalid(),
			Selected:    path.GetBest(),
			Stale:       path.GetStale(),
			Communities: pathCommunities(path),
		})
	}
	return out
}

func bestFIBRoutes(prefixes []bgpstate.Prefix, allowed []netip.Prefix) []FIBRoute {
	byPrefix := map[string]map[string]bool{}
	for _, prefix := range prefixes {
		if !prefix.Best || !prefix.Valid || strings.TrimSpace(prefix.Prefix) == "" {
			continue
		}
		nextHop := strings.TrimSpace(prefix.NextHop)
		if nextHop == "" || nextHop == "0.0.0.0" || nextHop == "::" {
			continue
		}
		parsed, err := netip.ParsePrefix(prefix.Prefix)
		if err != nil {
			continue
		}
		parsed = parsed.Masked()
		if len(allowed) > 0 && !prefixAllowed(parsed, allowed) {
			continue
		}
		key := parsed.String()
		if byPrefix[key] == nil {
			byPrefix[key] = map[string]bool{}
		}
		byPrefix[key][nextHop] = true
	}
	var out []FIBRoute
	for prefix, nextHops := range byPrefix {
		var hops []string
		for hop := range nextHops {
			hops = append(hops, hop)
		}
		sort.Strings(hops)
		out = append(out, FIBRoute{Prefix: prefix, NextHops: hops})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

type bgpPathRank struct {
	LocalPref uint32
	ASPathLen int
	Origin    uint8
	MED       uint32
}

func fibRoutesFromDestination(dst *gobgpapi.Destination, allowed []netip.Prefix) []FIBRoute {
	prefix := normalizeRoutePrefix(dst.GetPrefix())
	var candidates []struct {
		nextHop string
		rank    bgpPathRank
		best    bool
	}
	for _, path := range dst.GetPaths() {
		if path.GetIsWithdraw() || path.GetIsNexthopInvalid() {
			continue
		}
		pathPrefix := firstNonEmpty(prefix, normalizeRoutePrefix(pathPrefix(path)))
		if pathPrefix == "" {
			continue
		}
		parsed, err := netip.ParsePrefix(pathPrefix)
		if err != nil {
			continue
		}
		parsed = parsed.Masked()
		if len(allowed) > 0 && !prefixAllowed(parsed, allowed) {
			continue
		}
		nextHop := strings.TrimSpace(pathNextHop(path))
		if nextHop == "" || nextHop == "0.0.0.0" || nextHop == "::" {
			continue
		}
		candidates = append(candidates, struct {
			nextHop string
			rank    bgpPathRank
			best    bool
		}{nextHop: nextHop, rank: pathRank(path), best: path.GetBest()})
		prefix = parsed.String()
	}
	if len(candidates) == 0 || prefix == "" {
		return nil
	}
	bestRank := candidates[0].rank
	bestSet := false
	for _, candidate := range candidates {
		if candidate.best {
			bestRank = candidate.rank
			bestSet = true
			break
		}
	}
	if !bestSet {
		for _, candidate := range candidates[1:] {
			if comparePathRank(candidate.rank, bestRank) > 0 {
				bestRank = candidate.rank
			}
		}
	}
	seen := map[string]bool{}
	var nextHops []string
	for _, candidate := range candidates {
		if comparePathRank(candidate.rank, bestRank) != 0 || seen[candidate.nextHop] {
			continue
		}
		seen[candidate.nextHop] = true
		nextHops = append(nextHops, candidate.nextHop)
	}
	sort.Strings(nextHops)
	if len(nextHops) == 0 {
		return nil
	}
	return []FIBRoute{{Prefix: prefix, NextHops: nextHops}}
}

func mergeFIBRoutes(routes []FIBRoute) []FIBRoute {
	byPrefix := map[string]map[string]bool{}
	for _, route := range routes {
		prefix := normalizeRoutePrefix(route.Prefix)
		if prefix == "" {
			continue
		}
		if byPrefix[prefix] == nil {
			byPrefix[prefix] = map[string]bool{}
		}
		for _, nextHop := range normalizeRouteNextHops(route.NextHops) {
			byPrefix[prefix][nextHop] = true
		}
	}
	out := make([]FIBRoute, 0, len(byPrefix))
	for prefix, nextHops := range byPrefix {
		var hops []string
		for hop := range nextHops {
			hops = append(hops, hop)
		}
		sort.Strings(hops)
		out = append(out, FIBRoute{Prefix: prefix, NextHops: hops})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

func normalizeRouteNextHops(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		addr, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		key := addr.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func comparePathRank(a, b bgpPathRank) int {
	switch {
	case a.LocalPref != b.LocalPref:
		return int(a.LocalPref) - int(b.LocalPref)
	case a.ASPathLen != b.ASPathLen:
		return b.ASPathLen - a.ASPathLen
	case a.Origin != b.Origin:
		return int(b.Origin) - int(a.Origin)
	case a.MED != b.MED:
		return int(b.MED) - int(a.MED)
	default:
		return 0
	}
}

func pathRank(path *gobgpapi.Path) bgpPathRank {
	rank := bgpPathRank{LocalPref: 100, Origin: 2}
	for _, attr := range path.GetPattrs() {
		value, err := attr.UnmarshalNew()
		if err != nil {
			continue
		}
		switch typed := value.(type) {
		case *gobgpapi.LocalPrefAttribute:
			rank.LocalPref = typed.GetLocalPref()
		case *gobgpapi.AsPathAttribute:
			rank.ASPathLen += asPathLength(typed.GetSegments())
		case *gobgpapi.As4PathAttribute:
			rank.ASPathLen += asPathLength(typed.GetSegments())
		case *gobgpapi.OriginAttribute:
			rank.Origin = uint8(typed.GetOrigin())
		case *gobgpapi.MultiExitDiscAttribute:
			rank.MED = typed.GetMed()
		}
	}
	return rank
}

func asPathLength(segments []*gobgpapi.AsSegment) int {
	length := 0
	for _, segment := range segments {
		if segment.GetType() == gobgpapi.AsSegment_AS_SET && len(segment.GetNumbers()) > 0 {
			length++
			continue
		}
		length += len(segment.GetNumbers())
	}
	return length
}

func applyFIBResult(state bgpstate.State, routes []FIBRoute, result FIBSyncResult) bgpstate.State {
	targets := map[string]bool{}
	for _, route := range routes {
		prefix := normalizeRoutePrefix(route.Prefix)
		if prefix != "" {
			targets[prefix] = true
		}
	}
	for i := range state.Prefixes {
		prefix := normalizeRoutePrefix(state.Prefixes[i].Prefix)
		if !targets[prefix] {
			continue
		}
		state.Prefixes[i].Prefix = prefix
		if result.Installed[prefix] {
			state.Prefixes[i].Installed = true
			state.Prefixes[i].SelectionState = "installed"
			state.Prefixes[i].SelectionReason = ""
			continue
		}
		state.Prefixes[i].Installed = false
		state.Prefixes[i].SelectionState = "notInstalled"
		if reason := result.Unsupported[prefix]; reason != "" {
			state.Prefixes[i].SelectionReason = reason
		} else {
			state.Prefixes[i].SelectionReason = "GoBGPFIBNotInstalled"
		}
	}
	return state
}

func normalizeRoutePrefix(value string) string {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return prefix.Masked().String()
}

func fibInstalledCount(result FIBSyncResult) int {
	count := 0
	for _, installed := range result.Installed {
		if installed {
			count++
		}
	}
	return count
}

func fibUnsupportedCount(result FIBSyncResult) int {
	return len(result.Unsupported)
}

func importAllowedPrefixes(spec routerapi.BGPRouterSpec) []netip.Prefix {
	var out []netip.Prefix
	for _, prefix := range spec.ImportPolicy.AllowedPrefixes {
		if parsed, err := netip.ParsePrefix(strings.TrimSpace(prefix)); err == nil {
			out = append(out, parsed.Masked())
		}
	}
	return out
}

func prefixAllowed(candidate netip.Prefix, allowed []netip.Prefix) bool {
	for _, parent := range allowed {
		if parent.Addr().Is4() != candidate.Addr().Is4() {
			continue
		}
		if parent.Contains(candidate.Addr()) && candidate.Bits() >= parent.Bits() {
			return true
		}
	}
	return false
}

func pathPrefix(path *gobgpapi.Path) string {
	value, err := path.GetNlri().UnmarshalNew()
	if err != nil {
		return ""
	}
	switch nlri := value.(type) {
	case *gobgpapi.IPAddressPrefix:
		addr, err := netip.ParseAddr(nlri.GetPrefix())
		if err != nil {
			return ""
		}
		return netip.PrefixFrom(addr, int(nlri.GetPrefixLen())).Masked().String()
	default:
		return ""
	}
}

func pathCommunities(path *gobgpapi.Path) []string {
	var out []string
	for _, attr := range path.GetPattrs() {
		value, err := attr.UnmarshalNew()
		if err != nil {
			continue
		}
		if communities, ok := value.(*gobgpapi.CommunitiesAttribute); ok {
			for _, community := range communities.GetCommunities() {
				out = append(out, fmt.Sprintf("%d:%d", community>>16, community&0xffff))
			}
		}
	}
	sort.Strings(out)
	return out
}

func pathNextHop(path *gobgpapi.Path) string {
	for _, attr := range path.GetPattrs() {
		value, err := attr.UnmarshalNew()
		if err != nil {
			continue
		}
		switch typed := value.(type) {
		case *gobgpapi.NextHopAttribute:
			return strings.TrimSpace(typed.GetNextHop())
		case *gobgpapi.MpReachNLRIAttribute:
			for _, hop := range typed.GetNextHops() {
				if strings.TrimSpace(hop) != "" {
					return strings.TrimSpace(hop)
				}
			}
		}
	}
	return strings.TrimSpace(path.GetNeighborIp())
}

func advertisedPrefixes(spec routerapi.BGPRouterSpec) map[string]bool {
	out := map[string]bool{}
	for _, prefix := range spec.ExportPolicy.AllowedPrefixes {
		if normalized, ok := normalizePrefix(prefix); ok {
			out[normalized] = true
		}
	}
	for _, prefix := range spec.Redistribute.Connected.AllowedPrefixes {
		if normalized, ok := normalizePrefix(prefix); ok {
			out[normalized] = true
		}
	}
	for _, prefix := range spec.Redistribute.Static.AllowedPrefixes {
		if normalized, ok := normalizePrefix(prefix); ok {
			out[normalized] = true
		}
	}
	return out
}

func localPath(prefix string) (*gobgpapi.Path, error) {
	parsed, err := netip.ParsePrefix(strings.TrimSpace(prefix))
	if err != nil {
		return nil, err
	}
	parsed = parsed.Masked()
	nlri, err := anypb.New(&gobgpapi.IPAddressPrefix{Prefix: parsed.Addr().String(), PrefixLen: uint32(parsed.Bits())})
	if err != nil {
		return nil, err
	}
	origin, err := anypb.New(&gobgpapi.OriginAttribute{Origin: 0})
	if err != nil {
		return nil, err
	}
	nextHop := "0.0.0.0"
	if parsed.Addr().Is6() {
		nextHop = "::"
	}
	nh, err := anypb.New(&gobgpapi.NextHopAttribute{NextHop: nextHop})
	if err != nil {
		return nil, err
	}
	return &gobgpapi.Path{
		Family: familyForPrefix(parsed),
		Nlri:   nlri,
		Pattrs: []*anypb.Any{origin, nh},
	}, nil
}

func familyForPrefix(prefix netip.Prefix) *gobgpapi.Family {
	if prefix.Addr().Is6() {
		return ipv6Family()
	}
	return ipv4Family()
}

func ipv4Family() *gobgpapi.Family {
	return &gobgpapi.Family{Afi: gobgpapi.Family_AFI_IP, Safi: gobgpapi.Family_SAFI_UNICAST}
}

func ipv6Family() *gobgpapi.Family {
	return &gobgpapi.Family{Afi: gobgpapi.Family_AFI_IP6, Safi: gobgpapi.Family_SAFI_UNICAST}
}

func bgpFamiliesForRouter(router *routerapi.Router) []*gobgpapi.Family {
	has6 := false
	if router != nil {
		for _, resource := range router.Spec.Resources {
			if resource.APIVersion != routerapi.NetAPIVersion {
				continue
			}
			switch resource.Kind {
			case "BGPRouter":
				spec, err := resource.BGPRouterSpec()
				if err == nil {
					for _, p := range append(append(append([]string{}, spec.ImportPolicy.AllowedPrefixes...), spec.ExportPolicy.AllowedPrefixes...), append(spec.Redistribute.Connected.AllowedPrefixes, spec.Redistribute.Static.AllowedPrefixes...)...) {
						if parsed, err := netip.ParsePrefix(strings.TrimSpace(p)); err == nil && parsed.Addr().Is6() {
							has6 = true
						}
					}
				}
			case "BGPPeer":
				spec, err := resource.BGPPeerSpec()
				if err == nil {
					for _, p := range spec.Peers {
						if addr, err := netip.ParseAddr(strings.TrimSpace(p)); err == nil && addr.Is6() {
							has6 = true
						}
					}
				}
			}
		}
	}
	out := []*gobgpapi.Family{ipv4Family()}
	if has6 {
		out = append(out, ipv6Family())
	}
	return out
}

func (c *Controller) bgpRouterUsesIPv6(spec routerapi.BGPRouterSpec) bool {
	for _, family := range bgpFamiliesForRouter(c.Router) {
		if family.GetAfi() == gobgpapi.Family_AFI_IP6 {
			return true
		}
	}
	for prefix := range advertisedPrefixes(spec) {
		if parsed, err := netip.ParsePrefix(prefix); err == nil && parsed.Addr().Is6() {
			return true
		}
	}
	return false
}

func (c *Controller) peersByResource(state bgpstate.State) map[string][]bgpstate.Peer {
	byAddress := map[string]bgpstate.Peer{}
	for _, peer := range state.Peers {
		byAddress[peer.Address] = peer
	}
	out := map[string][]bgpstate.Peer{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		spec, err := resource.BGPPeerSpec()
		if err != nil {
			continue
		}
		for _, peerAddress := range spec.Peers {
			peer, ok := byAddress[peerAddress]
			if !ok {
				peer = bgpstate.Peer{Address: peerAddress, ASN: spec.PeerASN, State: "Missing"}
			} else if peer.ASN == 0 {
				peer.ASN = spec.PeerASN
			}
			out[resource.Metadata.Name] = append(out[resource.Metadata.Name], peer)
		}
	}
	return out
}

func PollInterval(router *routerapi.Router) time.Duration {
	out := 15 * time.Second
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != routerapi.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		spec, err := resource.BGPRouterSpec()
		if err != nil || strings.TrimSpace(spec.Watcher.PollInterval) == "" {
			continue
		}
		duration, err := time.ParseDuration(spec.Watcher.PollInterval)
		if err != nil || duration < MinPollInterval {
			continue
		}
		if duration < out {
			out = duration
		}
	}
	return out
}

func (c *Controller) maxPrefixes() int {
	if c.MaxPrefixes > 0 {
		return c.MaxPrefixes
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		spec, err := resource.BGPRouterSpec()
		if err == nil && spec.Watcher.MaxPrefixes > 0 {
			return spec.Watcher.MaxPrefixes
		}
	}
	return bgpstate.DefaultMaxPrefixes
}

func (c *Controller) peerStateChangeThrottle() time.Duration {
	var out time.Duration
	if c.Router == nil {
		return 0
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		spec, err := resource.BGPRouterSpec()
		if err != nil || strings.TrimSpace(spec.Watcher.PeerStateChangeThrottle) == "" {
			continue
		}
		duration, err := time.ParseDuration(spec.Watcher.PeerStateChangeThrottle)
		if err != nil || duration <= 0 {
			continue
		}
		if out == 0 || duration < out {
			out = duration
		}
	}
	return out
}

func (c *Controller) publishBGPEvent(ctx context.Context, event bgpstate.Event) {
	if c.throttleBGPEvent(event) || c.Bus == nil {
		return
	}
	daemonEvent := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "gobgp", Kind: "bgp", Instance: "embedded"}, "routerd.bgp."+strings.ReplaceAll(event.Type, " ", "."), daemonapi.SeverityInfo)
	daemonEvent.Attributes = map[string]string{
		"peer":     event.Peer,
		"prefix":   event.Prefix,
		"previous": event.Previous,
		"current":  event.Current,
	}
	_ = c.Bus.Publish(ctx, daemonEvent)
}

func (c *Controller) throttleBGPEvent(event bgpstate.Event) bool {
	if event.Peer == "" || (event.Type != bgpstate.EventPeerUp && event.Type != bgpstate.EventPeerDown) {
		return false
	}
	window := c.peerStateChangeThrottle()
	if window <= 0 {
		return false
	}
	if c.peerEvents == nil {
		c.peerEvents = map[string]time.Time{}
	}
	key := event.Type + "|" + event.Peer
	now := time.Now()
	if previous, ok := c.peerEvents[key]; ok && now.Sub(previous) < window {
		return true
	}
	c.peerEvents[key] = now
	return false
}

func (c *Controller) applyPeerHistory(peers []bgpstate.Peer, now string) []bgpstate.Peer {
	previous := c.previousPeers()
	out := append([]bgpstate.Peer(nil), peers...)
	for i, peer := range out {
		prev := previous[peer.Address]
		if peer.Established {
			if peer.LastEstablishedAt == "" {
				if prev.Established && prev.LastEstablishedAt != "" {
					peer.LastEstablishedAt = prev.LastEstablishedAt
				} else {
					peer.LastEstablishedAt = now
				}
			}
			if peer.LastErrorAt == "" {
				peer.LastErrorAt = prev.LastErrorAt
			}
			if peer.LastErrorReason == "" {
				peer.LastErrorReason = prev.LastErrorReason
			}
		} else {
			if peer.LastEstablishedAt == "" {
				peer.LastEstablishedAt = prev.LastEstablishedAt
			}
			reason := firstNonEmpty(peer.LastErrorReason, peer.State, "NotEstablished")
			peer.LastErrorReason = reason
			if peer.LastErrorAt == "" {
				if prev.LastErrorReason == reason && prev.LastErrorAt != "" {
					peer.LastErrorAt = prev.LastErrorAt
				} else {
					peer.LastErrorAt = now
				}
			}
		}
		out[i] = peer
	}
	return out
}

func (c *Controller) previousPeers() map[string]bgpstate.Peer {
	out := map[string]bgpstate.Peer{}
	if c.Store == nil || c.Router == nil {
		return out
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.NetAPIVersion || (resource.Kind != "BGPRouter" && resource.Kind != "BGPPeer") {
			continue
		}
		for _, peer := range peersFromStatus(c.Store.ObjectStatus(routerapi.NetAPIVersion, resource.Kind, resource.Metadata.Name)["peers"]) {
			if peer.Address != "" {
				out[peer.Address] = peer
			}
		}
	}
	return out
}

func peersFromStatus(value any) []bgpstate.Peer {
	switch typed := value.(type) {
	case []bgpstate.Peer:
		return typed
	case []any:
		out := make([]bgpstate.Peer, 0, len(typed))
		for _, raw := range typed {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, bgpstate.Peer{
				Address:           statusString(item["address"]),
				ASN:               uint32(statusInt(item["asn"])),
				State:             statusString(item["state"]),
				Established:       statusBool(item["established"]),
				PrefixesReceived:  statusInt(item["prefixesReceived"]),
				LastEstablishedAt: statusString(item["lastEstablishedAt"]),
				LastErrorAt:       statusString(item["lastErrorAt"]),
				LastErrorReason:   statusString(item["lastErrorReason"]),
			})
		}
		return out
	default:
		return nil
	}
}

func observedCommunities(prefixes []bgpstate.Prefix) []string {
	seen := map[string]bool{}
	var out []string
	for _, prefix := range prefixes {
		for _, community := range prefix.Communities {
			community = strings.TrimSpace(community)
			if community == "" || seen[community] {
				continue
			}
			seen[community] = true
			out = append(out, community)
		}
	}
	sort.Strings(out)
	return out
}

func hasBGP(router *routerapi.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == routerapi.NetAPIVersion && (resource.Kind == "BGPRouter" || resource.Kind == "BGPPeer") {
			return true
		}
	}
	return false
}

func normalizePrefix(value string) (string, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	return prefix.Masked().String(), true
}

func durationSeconds(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	duration, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || duration <= 0 {
		return fallback
	}
	return int(duration.Seconds())
}

func establishedPeers(peers []bgpstate.Peer) int {
	var out int
	for _, peer := range peers {
		if peer.Established {
			out++
		}
	}
	return out
}

func secretValue(plain string, source routerapi.SecretValueSourceSpec) (string, error) {
	if strings.TrimSpace(plain) != "" {
		return plain, nil
	}
	if strings.TrimSpace(source.File) == "" && strings.TrimSpace(source.Env) == "" {
		return "", nil
	}
	var value string
	switch {
	case strings.TrimSpace(source.File) != "":
		data, err := os.ReadFile(strings.TrimSpace(source.File))
		if err != nil {
			return "", fmt.Errorf("read secret file %q: %w", strings.TrimSpace(source.File), err)
		}
		value = string(data)
	case strings.TrimSpace(source.Env) != "":
		env := strings.TrimSpace(source.Env)
		var ok bool
		value, ok = os.LookupEnv(env)
		if !ok {
			return "", fmt.Errorf("read secret env %q: not set", env)
		}
	}
	value = strings.TrimRight(value, "\r\n")
	if source.Base64 {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
		if err != nil {
			return "", fmt.Errorf("decode base64 secret: %w", err)
		}
		value = strings.TrimRight(string(decoded), "\r\n")
	}
	return value, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonZero(values ...uint32) uint32 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func cBool(value *bool) bool {
	return value != nil && *value
}

func statusString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func statusInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case uint:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var out int
		_, _ = fmt.Sscanf(strings.TrimSpace(typed), "%d", &out)
		return out
	default:
		return 0
	}
}

func statusBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func (c *Controller) Close() {
	if c.Server != nil {
		c.Server.Stop()
	}
}
