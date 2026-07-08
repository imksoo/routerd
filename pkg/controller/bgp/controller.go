// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gobgpapi "github.com/osrg/gobgp/v3/api"
	"google.golang.org/protobuf/types/known/anypb"

	routerapi "github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/mobilityfib"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/manageddaemon"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type GoBGPServer interface {
	Serve()
	Stop()
	GetBgp(context.Context, *gobgpapi.GetBgpRequest) (*gobgpapi.GetBgpResponse, error)
	StartBgp(context.Context, *gobgpapi.StartBgpRequest) error
	StopBgp(context.Context, *gobgpapi.StopBgpRequest) error
	AddPeer(context.Context, *gobgpapi.AddPeerRequest) error
	UpdatePeer(context.Context, *gobgpapi.UpdatePeerRequest) (*gobgpapi.UpdatePeerResponse, error)
	ResetPeer(context.Context, *gobgpapi.ResetPeerRequest) error
	DeletePeer(context.Context, *gobgpapi.DeletePeerRequest) error
	ListPeer(context.Context, *gobgpapi.ListPeerRequest, func(*gobgpapi.Peer)) error
	ListDefinedSet(context.Context, *gobgpapi.ListDefinedSetRequest, func(*gobgpapi.DefinedSet)) error
	ListPolicy(context.Context, *gobgpapi.ListPolicyRequest, func(*gobgpapi.Policy)) error
	ListPolicyAssignment(context.Context, *gobgpapi.ListPolicyAssignmentRequest, func(*gobgpapi.PolicyAssignment)) error
	SetPolicies(context.Context, *gobgpapi.SetPoliciesRequest) error
	SetPolicyAssignment(context.Context, *gobgpapi.SetPolicyAssignmentRequest) error
	AddPeerGroup(context.Context, *gobgpapi.AddPeerGroupRequest) error
	DeletePeerGroup(context.Context, *gobgpapi.DeletePeerGroupRequest) error
	ListPeerGroup(context.Context, *gobgpapi.ListPeerGroupRequest, func(*gobgpapi.PeerGroup)) error
	AddDynamicNeighbor(context.Context, *gobgpapi.AddDynamicNeighborRequest) error
	DeleteDynamicNeighbor(context.Context, *gobgpapi.DeleteDynamicNeighborRequest) error
	ListDynamicNeighbor(context.Context, *gobgpapi.ListDynamicNeighborRequest, func(*gobgpapi.DynamicNeighbor)) error
	AddPath(context.Context, *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error)
	DeletePath(context.Context, *gobgpapi.DeletePathRequest) error
	ListPath(context.Context, *gobgpapi.ListPathRequest, func(*gobgpapi.Destination)) error
	WatchEvent(context.Context, *gobgpapi.WatchEventRequest, func(*gobgpapi.WatchEventResponse) error) error
	AppliedConfig(context.Context) (bgpdaemon.AppliedConfig, error)
	SaveAppliedConfig(context.Context, bgpdaemon.AppliedConfig) error
}

type FIBSyncer interface {
	SyncBGP(ctx context.Context, routes []FIBRoute) (FIBSyncResult, error)
}

type FIBRoute struct {
	Prefix          string
	NextHops        []string
	PreferredSource string
	RetainOnMissing bool
}

type FIBSyncResult struct {
	Installed                    map[string]bool
	Unsupported                  map[string]string
	Retained                     map[string]bool
	RetainedNextHops             map[string][]string
	PreferredSource              map[string]string
	PreferredSourceSkipped       map[string]bool
	PreferredSourceSkippedReason map[string]string
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
	Daemon    manageddaemon.Spec
	FIB       FIBSyncer

	MaxPrefixes         int
	WatchReconnectDelay time.Duration

	mu                   sync.Mutex
	started              bool
	startedAt            time.Time
	globalKey            string
	desiredPeerKeys      map[string]desiredPeer
	appliedPeerKeys      map[string]desiredPeer
	appliedConfig        bgpdaemon.AppliedConfig
	importPolicyKey      string
	pathUUIDs            map[string][]byte
	observed             bool
	lastState            bgpstate.State
	peerEvents           map[string]time.Time
	lastFIBRoutesSig     string
	lastFIBResult        FIBSyncResult
	lastFIBValid         bool
	bfdPeerSeenUp        map[string]bool
	bfdPeerDownSince     map[string]time.Time
	bfdPeerResetPending  map[string]bool
	bfdPeerLastResetAt   map[string]time.Time
	bfdPeerResetError    map[string]string
	bfdPeerResetAttempts map[string]int

	lastDynamicPeers     []dynamicPeerObservation
	lastDynamicAdmission dynamicRouteAdmissionSummary
}

type desiredPeer struct {
	Address                 string
	ASN                     uint32
	LocalASN                uint32
	Password                string
	BFD                     string
	EbgpMultihop            int
	RouteReflectorClient    bool
	RouteReflectorClusterID string
	Timers                  routerapi.BGPTimersSpec
	GracefulRestart         routerapi.BGPGracefulRestartSpec
	ConvergenceProfile      string
	ImportPolicy            routerapi.BGPImportPolicySpec
	ImportPolicyName        string
	ExportPolicy            routerapi.BGPExportPolicySpec
	ExportPolicyName        string
}

type desiredDynamicPeer struct {
	Name                    string
	PeerGroupName           string
	Prefixes                []string
	ASN                     uint32
	LocalASN                uint32
	Password                string
	EbgpMultihop            int
	RouteReflectorClient    bool
	RouteReflectorClusterID string
	Timers                  routerapi.BGPTimersSpec
	GracefulRestart         routerapi.BGPGracefulRestartSpec
	ImportPolicy            routerapi.BGPImportPolicySpec
	ImportPolicyName        string
	ExportPolicy            routerapi.BGPExportPolicySpec
	ExportPolicyName        string
}

type bfdPeerResetTarget struct {
	Key     string
	Address string
}

const (
	bfdResetBackoffBase = time.Second
	bfdResetBackoffMax  = 30 * time.Second
)

func (c *Controller) Reconcile(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reconcileLocked(ctx)
}

func (c *Controller) reconcileLocked(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	if !hasBGP(c.Router) {
		c.stopServerLocked()
		return nil
	}
	routers := c.bgpRouters()
	if len(routers) == 0 {
		return nil
	}
	if len(routers) > 1 {
		err := fmt.Errorf("routerd-bgp MVP supports one BGPRouter per router; found %d", len(routers))
		return c.savePendingAll("GoBGPMultipleRoutersUnsupported", err)
	}
	routerResource := routers[0]
	routerSpec, err := routerResource.BGPRouterSpec()
	if err != nil {
		return err
	}
	if strings.TrimSpace(routerSpec.VRF) != "" {
		err := fmt.Errorf("routerd-bgp MVP does not yet support BGPRouter.spec.vrf")
		return c.savePendingAll("GoBGPVRFUnsupported", err)
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
	applied, err := c.Server.AppliedConfig(ctx)
	if err != nil {
		return c.savePendingAll("GoBGPAppliedStateUnavailable", err)
	}
	c.hydrateAppliedState(applied)
	desired, err := c.desiredPeers(routerResource.Metadata.Name, routerSpec.ASN)
	if err != nil {
		return c.savePendingAll("GoBGPPeerConfigInvalid", err)
	}
	desiredDynamic, err := c.desiredDynamicPeers(routerResource.Metadata.Name, routerSpec.ASN)
	if err != nil {
		return c.savePendingAll("GoBGPDynamicPeerConfigInvalid", err)
	}
	liveEstablishedPeers, err := c.liveEstablishedPeers(ctx)
	if err != nil {
		return c.savePendingAll("GoBGPPeerObserveFailed", err)
	}
	bfdDownTransitions := c.observeBFDPeerStates(desired, liveEstablishedPeers)
	staticExportPrefixes := mapKeys(advertisedPrefixes(routerSpec))
	dynamicExportPrefixes := dynamicPathExportPrefixes(applied.Paths)
	effectiveImportPolicy := effectiveGlobalImportPolicy(routerSpec.ImportPolicy, dynamicExportPrefixes)
	desired = applyRouterBGPDefaults(routerResource.Metadata.Name, routerSpec, desired, staticExportPrefixes, dynamicExportPrefixes)
	desiredDynamic = applyRouterBGPDynamicDefaults(routerResource.Metadata.Name, routerSpec, desiredDynamic, staticExportPrefixes, dynamicExportPrefixes)
	adoptedRestoredPolicies, err := c.reconcilePolicies(ctx, routerResource.Metadata.Name, effectiveImportPolicy, desired, desiredDynamic)
	if err != nil {
		return c.savePendingAll("GoBGPPolicyApplyFailed", err)
	}
	if err := c.reconcileDynamicPeers(ctx, desiredDynamic); err != nil {
		return c.savePendingAll("GoBGPDynamicPeerApplyFailed", err)
	}
	exportPolicyRefreshPeers := exportPolicyChangedPeers(c.appliedPeerKeys, desired)
	changed, err := c.reconcilePeers(ctx, desired)
	if err != nil {
		return c.savePendingAll("GoBGPPeerApplyFailed", err)
	}
	if err := c.hardResetBFDDownPeers(ctx, bfdDownTransitions); err != nil {
		return c.savePendingAll("GoBGPBFDResetFailed", err)
	}
	if len(exportPolicyRefreshPeers) > 0 {
		if err := c.softResetExportPolicy(ctx, exportPolicyRefreshPeers); err != nil {
			return c.savePendingAll("GoBGPExportPolicyRefreshFailed", err)
		}
	}
	if err := c.reconcileAdvertisements(ctx, routerSpec, applied.Paths); err != nil {
		return c.savePendingAll("GoBGPPathApplyFailed", err)
	}
	appliedSpec := routerSpec
	appliedSpec.ImportPolicy = effectiveImportPolicy
	applied = c.buildAppliedConfig(appliedSpec, desired, advertisedPrefixes(routerSpec), applied.Paths)
	if err := c.Server.SaveAppliedConfig(ctx, applied); err != nil {
		return c.savePendingAll("GoBGPAppliedStatePersistFailed", err)
	}
	c.appliedConfig = applied
	allowedImportPrefixes := importAllowedPrefixesFromAppliedAndDynamic(applied, desiredDynamic)
	state, routes, livenessMarkers, err := c.observeState(ctx, allowedImportPrefixes, desired)
	if err != nil {
		return c.savePendingAll("GoBGPObserveFailed", err)
	}
	if !adoptedRestoredPolicies {
		importDrift, err := c.importPolicyDrift(ctx, routerResource.Metadata.Name, effectiveImportPolicy, desired, desiredDynamic)
		if err != nil {
			return c.savePendingAll("GoBGPPolicyObserveFailed", err)
		}
		if importDrift.RefreshNeeded() {
			if err := c.applyBGPPolicies(ctx, routerResource.Metadata.Name, effectiveImportPolicy, desired, desiredDynamic); err != nil {
				return c.savePendingAll("GoBGPPolicyApplyFailed", err)
			}
			c.importPolicyKey = bgpPoliciesKey(effectiveImportPolicy, desired, desiredDynamic)
			if err := c.refreshPeerImportPolicyAssignments(ctx, desired, importDrift.PeerAddresses); err != nil {
				return c.savePendingAll("GoBGPPeerApplyFailed", err)
			}
			if err := c.softResetImportPolicy(ctx, desired); err != nil {
				return c.savePendingAll("GoBGPImportPolicyRefreshFailed", err)
			}
			state, routes, livenessMarkers, err = c.observeState(ctx, allowedImportPrefixes, desired)
			if err != nil {
				return c.savePendingAll("GoBGPObserveFailed", err)
			}
		}
	}
	if c.FIB == nil {
		c.FIB = defaultFIBSyncer()
	}
	fibResult, err := c.syncBGPFIBLocked(ctx, routes)
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
	if err := c.saveObservedStatuses(routerResource.Metadata.Name, routerSpec, state, routes, changed, fibResult, livenessMarkers); err != nil {
		return err
	}
	for _, event := range events {
		c.publishBGPEvent(ctx, event)
	}
	return nil
}

func (c *Controller) stopServerLocked() {
	if c.Server != nil {
		c.Server.Stop()
		c.Server = nil
	}
	c.started = false
	c.startedAt = time.Time{}
	c.globalKey = ""
	c.desiredPeerKeys = nil
	c.appliedPeerKeys = nil
	c.appliedConfig = bgpdaemon.AppliedConfig{}
	c.importPolicyKey = ""
	c.pathUUIDs = nil
	c.observed = false
	c.lastState = bgpstate.State{}
	c.lastFIBRoutesSig = ""
	c.lastFIBValid = false
	c.lastDynamicPeers = nil
	c.lastDynamicAdmission = dynamicRouteAdmissionSummary{}
}

func (c *Controller) Start(ctx context.Context) {
	go c.watchEventLoop(ctx)
}

func (c *Controller) watchEventLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.watchBestPathEvents(ctx); err != nil {
			c.logDebug("bgp watch event stream unavailable; poll fallback remains active", "error", err)
		}
		if !sleepContext(ctx, c.watchReconnectDelay()) {
			return
		}
	}
}

func (c *Controller) watchBestPathEvents(ctx context.Context) error {
	c.mu.Lock()
	server := c.Server
	watchable := c.Router != nil && c.Store != nil && hasBGP(c.Router) && !c.DryRun && server != nil && c.started
	c.mu.Unlock()
	if !watchable {
		return nil
	}
	req := &gobgpapi.WatchEventRequest{
		Peer: &gobgpapi.WatchEventRequest_Peer{},
		Table: &gobgpapi.WatchEventRequest_Table{
			Filters: []*gobgpapi.WatchEventRequest_Table_Filter{{
				Type: gobgpapi.WatchEventRequest_Table_Filter_BEST,
				Init: false,
			}},
		},
		BatchSize: 1,
	}
	return server.WatchEvent(ctx, req, func(resp *gobgpapi.WatchEventResponse) error {
		if !watchEventHasBestPathChange(resp) && !watchEventHasPeerStateChange(resp) {
			return nil
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.observeAndSyncFromWatchLocked(ctx)
	})
}

func (c *Controller) observeAndSyncFromWatchLocked(ctx context.Context) error {
	if c.Router == nil || c.Store == nil || c.Server == nil || c.DryRun {
		return nil
	}
	routers := c.bgpRouters()
	if len(routers) != 1 {
		return nil
	}
	routerResource := routers[0]
	routerSpec, err := routerResource.BGPRouterSpec()
	if err != nil {
		return err
	}
	desired, err := c.desiredPeers(routerResource.Metadata.Name, routerSpec.ASN)
	if err != nil {
		return err
	}
	desiredDynamic, err := c.desiredDynamicPeers(routerResource.Metadata.Name, routerSpec.ASN)
	if err != nil {
		return err
	}
	applied := c.appliedConfig
	dynamicExportPrefixes := dynamicPathExportPrefixes(applied.Paths)
	desired = applyRouterBGPDefaults(routerResource.Metadata.Name, routerSpec, desired, mapKeys(advertisedPrefixes(routerSpec)), dynamicExportPrefixes)
	desiredDynamic = applyRouterBGPDynamicDefaults(routerResource.Metadata.Name, routerSpec, desiredDynamic, mapKeys(advertisedPrefixes(routerSpec)), dynamicExportPrefixes)
	appliedSpec := routerSpec
	appliedSpec.ImportPolicy = effectiveGlobalImportPolicy(routerSpec.ImportPolicy, dynamicExportPrefixes)
	allowedImportPrefixes := importAllowedPrefixesFromAppliedAndDynamic(c.buildAppliedConfig(appliedSpec, desired, advertisedPrefixes(routerSpec), applied.Paths), desiredDynamic)
	state, routes, livenessMarkers, err := c.observeState(ctx, allowedImportPrefixes, desired)
	if err != nil {
		return c.savePendingAll("GoBGPWatchObserveFailed", err)
	}
	if c.FIB == nil {
		c.FIB = defaultFIBSyncer()
	}
	fibResult, err := c.syncBGPFIBLocked(ctx, routes)
	if err != nil {
		return c.savePendingAll("GoBGPWatchFIBSyncFailed", err)
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
	if err := c.saveObservedStatuses(routerResource.Metadata.Name, routerSpec, state, routes, false, fibResult, livenessMarkers); err != nil {
		return err
	}
	for _, event := range events {
		c.publishBGPEvent(ctx, event)
	}
	return nil
}

func (c *Controller) syncBGPFIBLocked(ctx context.Context, routes []FIBRoute) (FIBSyncResult, error) {
	if c.FIB == nil {
		c.FIB = defaultFIBSyncer()
	}
	sig := fibRoutesSignature(routes)
	result, err := c.FIB.SyncBGP(ctx, routes)
	if err != nil {
		return result, err
	}
	c.lastFIBRoutesSig = sig
	c.lastFIBResult = normalizeFIBSyncResult(result)
	c.lastFIBValid = true
	return c.lastFIBResult, nil
}

func watchEventHasBestPathChange(resp *gobgpapi.WatchEventResponse) bool {
	table := resp.GetTable()
	if table == nil {
		return false
	}
	return len(table.GetPaths()) > 0
}

func watchEventHasPeerStateChange(resp *gobgpapi.WatchEventResponse) bool {
	pe := resp.GetPeer()
	return pe != nil && pe.GetType() == gobgpapi.WatchEventResponse_PeerEvent_STATE
}

func (c *Controller) watchReconnectDelay() time.Duration {
	if c.WatchReconnectDelay > 0 {
		return c.WatchReconnectDelay
	}
	return time.Second
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *Controller) hydrateAppliedState(applied bgpdaemon.AppliedConfig) {
	applied = bgpdaemon.Normalize(applied)
	c.appliedConfig = applied
	c.appliedPeerKeys = desiredPeersFromApplied(applied.Global.ASN, applied.Peers)
}

func (c *Controller) ensureServer(ctx context.Context, spec routerapi.BGPRouterSpec) error {
	key := bgpGlobalKey(spec)
	if c.Server == nil {
		if c.NewServer != nil {
			c.Server = c.NewServer()
		} else {
			c.Server = newRemoteGoBGPServer(c.daemonSpec())
		}
		c.Server.Serve()
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
	live, err := c.Server.GetBgp(ctx, &gobgpapi.GetBgpRequest{})
	if err != nil {
		return fmt.Errorf("connect to managed GoBGP daemon: %w", err)
	}
	if globalStarted(live.GetGlobal()) {
		if !globalMatches(live.GetGlobal(), req.GetGlobal()) {
			return fmt.Errorf("managed GoBGP global config differs from desired BGPRouter; restart routerd-bgp during a maintenance window to change ASN/router-id/listen socket")
		}
		c.started = true
		c.globalKey = key
		return nil
	}
	if err := c.Server.StartBgp(ctx, req); err != nil {
		return err
	}
	c.started = true
	c.startedAt = time.Now().UTC()
	c.globalKey = key
	c.desiredPeerKeys = nil
	c.pathUUIDs = map[string][]byte{}
	return nil
}

func (c *Controller) daemonSpec() manageddaemon.Spec {
	if c.Daemon.Name != "" || c.Daemon.SocketPath != "" {
		spec := DefaultDaemonSpec()
		if c.Daemon.Name != "" {
			spec.Name = c.Daemon.Name
		}
		if c.Daemon.Binary != "" {
			spec.Binary = c.Daemon.Binary
		}
		if c.Daemon.UnitName != "" {
			spec.UnitName = c.Daemon.UnitName
		}
		if c.Daemon.SocketPath != "" {
			spec.SocketPath = c.Daemon.SocketPath
			if c.Daemon.ControlSocketPath == "" {
				spec.ControlSocketPath = filepath.Join(filepath.Dir(c.Daemon.SocketPath), "control.sock")
			}
		}
		if c.Daemon.ControlSocketPath != "" {
			spec.ControlSocketPath = c.Daemon.ControlSocketPath
		}
		if c.Daemon.StatePath != "" {
			spec.StatePath = c.Daemon.StatePath
		}
		return spec
	}
	return DefaultDaemonSpec()
}

func DefaultDaemonSpec() manageddaemon.Spec {
	return manageddaemon.Spec{
		Name:              "routerd-bgp",
		Binary:            "routerd-bgp",
		UnitName:          "routerd-bgp.service",
		SocketPath:        "/run/routerd/bgp/gobgp.sock",
		ControlSocketPath: "/run/routerd/bgp/control.sock",
		StatePath:         "/var/lib/routerd/bgp/applied.json",
	}
}

func globalStarted(global *gobgpapi.Global) bool {
	return global != nil && global.GetAsn() != 0 && strings.TrimSpace(global.GetRouterId()) != ""
}

func globalMatches(live, desired *gobgpapi.Global) bool {
	if live.GetAsn() != desired.GetAsn() || strings.TrimSpace(live.GetRouterId()) != strings.TrimSpace(desired.GetRouterId()) {
		return false
	}
	if live.GetListenPort() != desired.GetListenPort() {
		return false
	}
	liveListen := live.GetListenAddresses()
	desiredListen := desired.GetListenAddresses()
	if len(liveListen) == 0 {
		liveListen = []string{"0.0.0.0", "::"}
	}
	if len(desiredListen) == 0 {
		desiredListen = []string{"0.0.0.0", "::"}
	}
	return sameStringSet(liveListen, desiredListen)
}

func (c *Controller) desiredPeers(routerName string, localASN uint32) (map[string]desiredPeer, error) {
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
		peers, err := c.resolveBGPPeerAddresses(spec)
		if err != nil {
			return nil, err
		}
		for _, peer := range peers {
			peer = strings.TrimSpace(peer)
			out[peer] = desiredPeer{
				Address:                 peer,
				ASN:                     spec.PeerASN,
				LocalASN:                localASN,
				Password:                password,
				BFD:                     strings.TrimSpace(spec.BFD),
				EbgpMultihop:            spec.EbgpMultihop,
				RouteReflectorClient:    spec.RouteReflectorClient,
				RouteReflectorClusterID: strings.TrimSpace(spec.RouteReflectorClusterID),
				ImportPolicy:            spec.ImportPolicy,
				ExportPolicy:            spec.ExportPolicy,
				Timers:                  spec.Timers,
			}
		}
	}
	return out, nil
}

func (c *Controller) resolveBGPPeerAddresses(spec routerapi.BGPPeerSpec) ([]string, error) {
	out := append([]string(nil), spec.Peers...)
	seen := map[string]bool{}
	for _, peer := range out {
		seen[strings.TrimSpace(peer)] = true
	}
	for _, source := range spec.PeersFrom {
		ref := strings.TrimSpace(source.Resource)
		kind, name, ok := strings.Cut(ref, "/")
		if !ok || kind != "SAMRRSet" || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("BGPPeer peersFrom resource must reference SAMRRSet/<name>")
		}
		rrSet, found, err := c.lookupSAMRRSet(name)
		if err != nil {
			return nil, err
		}
		if !found {
			if source.Optional {
				continue
			}
			return nil, fmt.Errorf("SAMRRSet/%s not found", name)
		}
		for _, member := range rrSet.Members {
			addr, err := parseDynamicPeerPrefixAddress(member.TunnelAddress)
			if err != nil {
				return nil, fmt.Errorf("SAMRRSet/%s member %s tunnelAddress: %w", name, member.NodeRef, err)
			}
			if !seen[addr] {
				seen[addr] = true
				out = append(out, addr)
			}
		}
	}
	return out, nil
}

func (c *Controller) lookupSAMRRSet(name string) (routerapi.SAMRRSetSpec, bool, error) {
	if c.Router == nil {
		return routerapi.SAMRRSetSpec{}, false, nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.MobilityAPIVersion || resource.Kind != "SAMRRSet" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.SAMRRSetSpec()
		if err != nil {
			return routerapi.SAMRRSetSpec{}, true, err
		}
		return spec, true, nil
	}
	return routerapi.SAMRRSetSpec{}, false, nil
}

func parseDynamicPeerPrefixAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return "", err
		}
		return prefix.Addr().String(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return "", err
	}
	return addr.String(), nil
}

func (c *Controller) desiredDynamicPeers(routerName string, localASN uint32) (map[string]desiredDynamicPeer, error) {
	out := map[string]desiredDynamicPeer{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.NetAPIVersion || resource.Kind != "BGPDynamicPeer" {
			continue
		}
		spec, err := resource.BGPDynamicPeerSpec()
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
		cleanPrefixes := cleanStrings(spec.Listen.SourcePrefixes)
		sort.Strings(cleanPrefixes)
		key := "routerd-dynamic-" + sanitizeBGPPolicyName(resource.Metadata.Name)
		out[key] = desiredDynamicPeer{
			Name:                    resource.Metadata.Name,
			PeerGroupName:           key,
			Prefixes:                cleanPrefixes,
			ASN:                     spec.PeerASN,
			LocalASN:                localASN,
			Password:                password,
			EbgpMultihop:            spec.EbgpMultihop,
			RouteReflectorClient:    spec.RouteReflectorClient,
			RouteReflectorClusterID: strings.TrimSpace(spec.RouteReflectorClusterID),
			Timers:                  spec.Timers,
			ImportPolicy:            spec.ImportPolicy,
			ExportPolicy:            spec.ExportPolicy,
		}
	}
	return out, nil
}

func applyRouterBGPDefaults(routerName string, routerSpec routerapi.BGPRouterSpec, peers map[string]desiredPeer, staticExportPrefixes, dynamicExportPrefixes []string) map[string]desiredPeer {
	globalImportPolicy := effectiveGlobalImportPolicy(routerSpec.ImportPolicy, dynamicExportPrefixes)
	for address, peer := range peers {
		peer.ConvergenceProfile = routerSpec.ConvergenceProfile
		peer.GracefulRestart = canonicalGracefulRestartSpec(routerSpec.GracefulRestart, peer.ConvergenceProfile)
		if peerHasImportPolicy(peer.ImportPolicy) {
			peer.ImportPolicy.AllowedPrefixes = mergeAllowedPrefixes(peer.ImportPolicy.AllowedPrefixes, dynamicExportPrefixes)
			peer.ImportPolicyName = peerImportPolicyName(routerName, address)
		} else {
			peer.ImportPolicy = globalImportPolicy
			peer.ImportPolicyName = bgpPolicyName(routerName, "import")
		}
		peer.ExportPolicyName = peerExportPolicyName(routerName, address)
		peer.ExportPolicy.AllowedPrefixes = mergeAllowedPrefixes(peer.ExportPolicy.AllowedPrefixes, staticExportPrefixes, dynamicExportPrefixes, routeReflectorExportPrefixes(peer, globalImportPolicy))
		peers[address] = peer
	}
	return peers
}

func applyRouterBGPDynamicDefaults(routerName string, routerSpec routerapi.BGPRouterSpec, peers map[string]desiredDynamicPeer, staticExportPrefixes, dynamicExportPrefixes []string) map[string]desiredDynamicPeer {
	globalImportPolicy := effectiveGlobalImportPolicy(routerSpec.ImportPolicy, dynamicExportPrefixes)
	for key, peer := range peers {
		peer.GracefulRestart = canonicalGracefulRestartSpec(routerSpec.GracefulRestart, routerSpec.ConvergenceProfile)
		if peerHasImportPolicy(peer.ImportPolicy) {
			peer.ImportPolicy.AllowedPrefixes = mergeAllowedPrefixes(peer.ImportPolicy.AllowedPrefixes, dynamicExportPrefixes)
			peer.ImportPolicyName = dynamicPeerImportPolicyName(routerName, key)
		} else {
			peer.ImportPolicy = globalImportPolicy
			peer.ImportPolicyName = bgpPolicyName(routerName, "import")
		}
		peer.ExportPolicyName = dynamicPeerExportPolicyName(routerName, key)
		peer.ExportPolicy.AllowedPrefixes = mergeAllowedPrefixes(peer.ExportPolicy.AllowedPrefixes, staticExportPrefixes, dynamicExportPrefixes, dynamicRouteReflectorExportPrefixes(peer, globalImportPolicy))
		peers[key] = peer
	}
	return peers
}

func routeReflectorExportPrefixes(peer desiredPeer, importPolicy routerapi.BGPImportPolicySpec) []string {
	if !peer.RouteReflectorClient {
		return nil
	}
	return importPolicy.AllowedPrefixes
}

func dynamicRouteReflectorExportPrefixes(peer desiredDynamicPeer, importPolicy routerapi.BGPImportPolicySpec) []string {
	if !peer.RouteReflectorClient {
		return nil
	}
	return importPolicy.AllowedPrefixes
}

func effectiveGlobalImportPolicy(spec routerapi.BGPImportPolicySpec, dynamicPrefixes []string) routerapi.BGPImportPolicySpec {
	if len(cleanStrings(spec.AllowedPrefixes)) == 0 {
		return spec
	}
	spec.AllowedPrefixes = mergeAllowedPrefixes(spec.AllowedPrefixes, dynamicPrefixes)
	return spec
}

func peerHasImportPolicy(spec routerapi.BGPImportPolicySpec) bool {
	return len(cleanStrings(spec.AllowedPrefixes)) > 0 ||
		len(cleanStrings(spec.RequiredCommunities)) > 0 ||
		len(cleanStrings(spec.ForbiddenCommunities)) > 0 ||
		strings.TrimSpace(spec.NextHopRewrite) != ""
}

func (c *Controller) reconcilePolicies(ctx context.Context, routerName string, spec routerapi.BGPImportPolicySpec, peers map[string]desiredPeer, dynamicPeers map[string]desiredDynamicPeer) (bool, error) {
	key := bgpPoliciesKey(spec, peers, dynamicPeers)
	if c.importPolicyKey == key {
		return false, nil
	}
	if c.importPolicyKey == "" && len(c.appliedConfig.Peers) > 0 {
		drift, err := c.importPolicyDrift(ctx, routerName, spec, peers, dynamicPeers)
		if err != nil {
			return false, err
		}
		if !drift.RefreshNeeded() {
			c.importPolicyKey = key
			return true, nil
		}
	}
	if err := c.applyBGPPolicies(ctx, routerName, spec, peers, dynamicPeers); err != nil {
		return false, err
	}
	c.importPolicyKey = key
	return false, nil
}

func (c *Controller) applyBGPPolicies(ctx context.Context, routerName string, spec routerapi.BGPImportPolicySpec, peers map[string]desiredPeer, dynamicPeers map[string]desiredDynamicPeer) error {
	plan := buildBGPPolicyPlan(routerName, spec, peers, dynamicPeers)
	if err := c.Server.SetPolicies(ctx, plan.SetPolicies); err != nil {
		return err
	}
	return c.Server.SetPolicyAssignment(ctx, &gobgpapi.SetPolicyAssignmentRequest{Assignment: plan.GlobalImportAssignment})
}

type bgpPolicyPlan struct {
	SetPolicies            *gobgpapi.SetPoliciesRequest
	GlobalImportAssignment *gobgpapi.PolicyAssignment
}

func buildBGPPolicyPlan(routerName string, spec routerapi.BGPImportPolicySpec, peers map[string]desiredPeer, dynamicPeers map[string]desiredDynamicPeer) bgpPolicyPlan {
	name := bgpPolicyName(routerName, "import")
	req := &gobgpapi.SetPoliciesRequest{}
	prefixes := importPolicyPrefixes(spec)
	assignment := globalImportPolicyAssignment(name, len(prefixes) > 0)
	if len(prefixes) > 0 {
		appendImportPolicy(req, name, bgpPolicyName(routerName, "import-prefixes"), spec)
	}
	importPolicies := map[string]bool{name: true}
	for _, peer := range sortedDesiredPeers(peers) {
		importPolicyName := strings.TrimSpace(peer.ImportPolicyName)
		if importPolicyName != "" && !importPolicies[importPolicyName] && len(importPolicyPrefixes(peer.ImportPolicy)) > 0 {
			appendImportPolicy(req, importPolicyName, importPolicyName+"-prefixes", peer.ImportPolicy)
			importPolicies[importPolicyName] = true
		}
		prefixes := exportPolicyPrefixes(peer.ExportPolicy)
		if len(prefixes) == 0 || strings.TrimSpace(peer.ExportPolicyName) == "" {
			continue
		}
		prefixSetName := peer.ExportPolicyName + "-prefixes"
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_PREFIX,
			Name:        prefixSetName,
			Prefixes:    prefixes,
		})
		req.Policies = append(req.Policies, &gobgpapi.Policy{
			Name: peer.ExportPolicyName,
			Statements: []*gobgpapi.Statement{{
				Name: bgpPolicyStatementName(peer.ExportPolicyName, "allow-export"),
				Conditions: &gobgpapi.Conditions{PrefixSet: &gobgpapi.MatchSet{
					Type: gobgpapi.MatchSet_ANY,
					Name: prefixSetName,
				}},
				Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ACCEPT},
			}},
		})
	}
	for _, peer := range sortedDesiredDynamicPeers(dynamicPeers) {
		importPolicyName := strings.TrimSpace(peer.ImportPolicyName)
		if importPolicyName != "" && !importPolicies[importPolicyName] && len(importPolicyPrefixes(peer.ImportPolicy)) > 0 {
			appendImportPolicy(req, importPolicyName, importPolicyName+"-prefixes", peer.ImportPolicy)
			importPolicies[importPolicyName] = true
		}
		prefixes := exportPolicyPrefixes(peer.ExportPolicy)
		if len(prefixes) == 0 || strings.TrimSpace(peer.ExportPolicyName) == "" {
			continue
		}
		prefixSetName := peer.ExportPolicyName + "-prefixes"
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_PREFIX,
			Name:        prefixSetName,
			Prefixes:    prefixes,
		})
		req.Policies = append(req.Policies, &gobgpapi.Policy{
			Name: peer.ExportPolicyName,
			Statements: []*gobgpapi.Statement{{
				Name: bgpPolicyStatementName(peer.ExportPolicyName, "allow-export"),
				Conditions: &gobgpapi.Conditions{PrefixSet: &gobgpapi.MatchSet{
					Type: gobgpapi.MatchSet_ANY,
					Name: prefixSetName,
				}},
				Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ACCEPT},
			}},
		})
	}
	return bgpPolicyPlan{SetPolicies: req, GlobalImportAssignment: assignment}
}

func appendImportPolicy(req *gobgpapi.SetPoliciesRequest, policyName, prefixSetName string, spec routerapi.BGPImportPolicySpec) {
	prefixes := importPolicyPrefixes(spec)
	if len(prefixes) == 0 || strings.TrimSpace(policyName) == "" || strings.TrimSpace(prefixSetName) == "" {
		return
	}
	policyName = strings.TrimSpace(policyName)
	prefixSetName = strings.TrimSpace(prefixSetName)
	req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
		DefinedType: gobgpapi.DefinedType_PREFIX,
		Name:        prefixSetName,
		Prefixes:    prefixes,
	})
	requiredSetName := policyName + "-required-communities"
	requiredCommunities := cleanCommunityPolicyValues(spec.RequiredCommunities)
	if len(requiredCommunities) > 0 {
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_COMMUNITY,
			Name:        requiredSetName,
			List:        requiredCommunities,
		})
	}
	forbiddenSetName := policyName + "-forbidden-communities"
	forbiddenCommunities := cleanCommunityPolicyValues(spec.ForbiddenCommunities)
	if len(forbiddenCommunities) > 0 {
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_COMMUNITY,
			Name:        forbiddenSetName,
			List:        forbiddenCommunities,
		})
	}
	statements := []*gobgpapi.Statement{}
	if len(forbiddenCommunities) > 0 {
		statements = append(statements, &gobgpapi.Statement{
			Name: bgpPolicyStatementName(policyName, "reject-forbidden-community"),
			Conditions: &gobgpapi.Conditions{CommunitySet: &gobgpapi.MatchSet{
				Type: gobgpapi.MatchSet_ANY,
				Name: forbiddenSetName,
			}},
			Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_REJECT},
		})
	}
	acceptConditions := &gobgpapi.Conditions{PrefixSet: &gobgpapi.MatchSet{
		Type: gobgpapi.MatchSet_ANY,
		Name: prefixSetName,
	}}
	if len(requiredCommunities) > 0 {
		acceptConditions.CommunitySet = &gobgpapi.MatchSet{
			Type: gobgpapi.MatchSet_ALL,
			Name: requiredSetName,
		}
	}
	statements = append(statements, &gobgpapi.Statement{
		Name:       bgpPolicyStatementName(policyName, "allow-import"),
		Conditions: acceptConditions,
		Actions: &gobgpapi.Actions{
			RouteAction: gobgpapi.RouteAction_ACCEPT,
			Nexthop:     nextHopRewriteAction(spec),
		},
	})
	req.Policies = append(req.Policies, &gobgpapi.Policy{
		Name:       policyName,
		Statements: statements,
	})
}

type importPolicyDrift struct {
	PolicyState   bool
	PeerAddresses []string
}

func (d importPolicyDrift) RefreshNeeded() bool {
	return d.PolicyState || len(d.PeerAddresses) > 0
}

type canonicalImportPolicyState struct {
	DefinedSets      map[string]canonicalDefinedSet
	Policies         map[string]canonicalPolicy
	GlobalAssignment canonicalPolicyAssignment
	PeerAssignments  map[string]canonicalPolicyAssignment
}

type canonicalDefinedSet struct {
	Name     string
	Type     int32
	Prefixes []canonicalPrefix
}

type canonicalPrefix struct {
	Prefix string
	Min    uint32
	Max    uint32
}

type canonicalPolicy struct {
	Name       string
	Statements []canonicalStatement
}

type canonicalStatement struct {
	Name          string
	PrefixSetName string
	PrefixSetType int32
	RouteAction   int32
	NextHop       string
}

type canonicalPolicyAssignment struct {
	Name          string
	Direction     int32
	DefaultAction int32
	Policies      []string
}

func (c *Controller) importPolicyDrift(ctx context.Context, routerName string, spec routerapi.BGPImportPolicySpec, peers map[string]desiredPeer, dynamicPeers map[string]desiredDynamicPeer) (importPolicyDrift, error) {
	desired := desiredImportPolicyState(buildBGPPolicyPlan(routerName, spec, peers, dynamicPeers), peers)
	actual, err := c.actualImportPolicyState(ctx, desired)
	if err != nil {
		return importPolicyDrift{}, err
	}
	drift := importPolicyDrift{}
	if !reflect.DeepEqual(desired.DefinedSets, actual.DefinedSets) ||
		!reflect.DeepEqual(desired.Policies, actual.Policies) ||
		!reflect.DeepEqual(desired.GlobalAssignment, actual.GlobalAssignment) {
		drift.PolicyState = true
	}
	for _, peer := range sortedDesiredPeers(peers) {
		address := strings.TrimSpace(peer.Address)
		if address == "" {
			continue
		}
		if !reflect.DeepEqual(desired.PeerAssignments[address], actual.PeerAssignments[address]) {
			drift.PeerAddresses = append(drift.PeerAddresses, address)
		}
	}
	sort.Strings(drift.PeerAddresses)
	return drift, nil
}

func desiredImportPolicyState(plan bgpPolicyPlan, peers map[string]desiredPeer) canonicalImportPolicyState {
	state := canonicalImportPolicyState{
		DefinedSets:     map[string]canonicalDefinedSet{},
		Policies:        map[string]canonicalPolicy{},
		PeerAssignments: map[string]canonicalPolicyAssignment{},
	}
	importPolicyNames := map[string]bool{}
	importDefinedSetNames := map[string]bool{}
	for _, policy := range plan.SetPolicies.GetPolicies() {
		if !policyHasImportAction(policy) {
			continue
		}
		name := strings.TrimSpace(policy.GetName())
		if name == "" {
			continue
		}
		importPolicyNames[name] = true
		state.Policies[name] = canonicalizePolicy(policy)
		for _, statement := range policy.GetStatements() {
			if setName := strings.TrimSpace(statement.GetConditions().GetPrefixSet().GetName()); setName != "" {
				importDefinedSetNames[setName] = true
			}
		}
	}
	for _, set := range plan.SetPolicies.GetDefinedSets() {
		name := strings.TrimSpace(set.GetName())
		if !importDefinedSetNames[name] {
			continue
		}
		state.DefinedSets[name] = canonicalizeDefinedSet(set)
	}
	if len(importPolicyNames) > 0 {
		state.GlobalAssignment = canonicalizePolicyAssignment(plan.GlobalImportAssignment)
	}
	for _, peer := range sortedDesiredPeers(peers) {
		address := strings.TrimSpace(peer.Address)
		if address == "" {
			continue
		}
		state.PeerAssignments[address] = canonicalizePolicyAssignment(goBGPPeer(peer).GetApplyPolicy().GetImportPolicy())
	}
	return state
}

func policyHasImportAction(policy *gobgpapi.Policy) bool {
	for _, statement := range policy.GetStatements() {
		if statement.GetActions().GetNexthop() != nil {
			return true
		}
	}
	return false
}

func (c *Controller) actualImportPolicyState(ctx context.Context, desired canonicalImportPolicyState) (canonicalImportPolicyState, error) {
	actual := canonicalImportPolicyState{
		DefinedSets:     map[string]canonicalDefinedSet{},
		Policies:        map[string]canonicalPolicy{},
		PeerAssignments: map[string]canonicalPolicyAssignment{},
	}
	for name := range desired.DefinedSets {
		set, err := c.definedSetByName(ctx, name)
		if err != nil {
			return canonicalImportPolicyState{}, err
		}
		if set != nil {
			actual.DefinedSets[name] = canonicalizeDefinedSet(set)
		}
	}
	for name := range desired.Policies {
		policy, err := c.policyByName(ctx, name)
		if err != nil {
			return canonicalImportPolicyState{}, err
		}
		if policy != nil {
			actual.Policies[name] = canonicalizePolicy(policy)
		}
	}
	if desired.GlobalAssignment.Name != "" {
		assignment, err := c.policyAssignment(ctx, desired.GlobalAssignment.Name, gobgpapi.PolicyDirection_IMPORT)
		if err != nil {
			return canonicalImportPolicyState{}, err
		}
		actual.GlobalAssignment = canonicalizePolicyAssignment(assignment)
	}
	if len(desired.PeerAssignments) > 0 {
		if err := c.Server.ListPeer(ctx, &gobgpapi.ListPeerRequest{}, func(peer *gobgpapi.Peer) {
			address := strings.TrimSpace(peerAddress(peer))
			if _, ok := desired.PeerAssignments[address]; !ok {
				return
			}
			actual.PeerAssignments[address] = canonicalizePolicyAssignment(peer.GetApplyPolicy().GetImportPolicy())
		}); err != nil {
			return canonicalImportPolicyState{}, err
		}
		for address := range desired.PeerAssignments {
			if _, ok := actual.PeerAssignments[address]; !ok {
				actual.PeerAssignments[address] = canonicalPolicyAssignment{}
			}
		}
	}
	return actual, nil
}

func (c *Controller) definedSetByName(ctx context.Context, name string) (*gobgpapi.DefinedSet, error) {
	var out *gobgpapi.DefinedSet
	err := c.Server.ListDefinedSet(ctx, &gobgpapi.ListDefinedSetRequest{DefinedType: gobgpapi.DefinedType_PREFIX, Name: name}, func(set *gobgpapi.DefinedSet) {
		if strings.TrimSpace(set.GetName()) == name {
			out = set
		}
	})
	return out, err
}

func (c *Controller) policyByName(ctx context.Context, name string) (*gobgpapi.Policy, error) {
	var out *gobgpapi.Policy
	err := c.Server.ListPolicy(ctx, &gobgpapi.ListPolicyRequest{Name: name}, func(policy *gobgpapi.Policy) {
		if strings.TrimSpace(policy.GetName()) == name {
			out = policy
		}
	})
	return out, err
}

func (c *Controller) policyAssignment(ctx context.Context, name string, direction gobgpapi.PolicyDirection) (*gobgpapi.PolicyAssignment, error) {
	var out *gobgpapi.PolicyAssignment
	err := c.Server.ListPolicyAssignment(ctx, &gobgpapi.ListPolicyAssignmentRequest{Name: name, Direction: direction}, func(assignment *gobgpapi.PolicyAssignment) {
		if strings.TrimSpace(assignment.GetName()) == name && assignment.GetDirection() == direction {
			out = assignment
		}
	})
	return out, err
}

func canonicalizeDefinedSet(set *gobgpapi.DefinedSet) canonicalDefinedSet {
	if set == nil {
		return canonicalDefinedSet{}
	}
	out := canonicalDefinedSet{Name: strings.TrimSpace(set.GetName()), Type: int32(set.GetDefinedType())}
	for _, prefix := range set.GetPrefixes() {
		out.Prefixes = append(out.Prefixes, canonicalPrefix{
			Prefix: strings.TrimSpace(prefix.GetIpPrefix()),
			Min:    prefix.GetMaskLengthMin(),
			Max:    prefix.GetMaskLengthMax(),
		})
	}
	sort.Slice(out.Prefixes, func(i, j int) bool {
		if out.Prefixes[i].Prefix != out.Prefixes[j].Prefix {
			return out.Prefixes[i].Prefix < out.Prefixes[j].Prefix
		}
		if out.Prefixes[i].Min != out.Prefixes[j].Min {
			return out.Prefixes[i].Min < out.Prefixes[j].Min
		}
		return out.Prefixes[i].Max < out.Prefixes[j].Max
	})
	return out
}

func canonicalizePolicy(policy *gobgpapi.Policy) canonicalPolicy {
	if policy == nil {
		return canonicalPolicy{}
	}
	out := canonicalPolicy{Name: strings.TrimSpace(policy.GetName())}
	for _, statement := range policy.GetStatements() {
		prefixSet := statement.GetConditions().GetPrefixSet()
		out.Statements = append(out.Statements, canonicalStatement{
			Name:          strings.TrimSpace(statement.GetName()),
			PrefixSetName: strings.TrimSpace(prefixSet.GetName()),
			PrefixSetType: int32(prefixSet.GetType()),
			RouteAction:   int32(statement.GetActions().GetRouteAction()),
			NextHop:       canonicalNextHopAction(statement.GetActions().GetNexthop()),
		})
	}
	sort.Slice(out.Statements, func(i, j int) bool { return out.Statements[i].Name < out.Statements[j].Name })
	return out
}

func canonicalNextHopAction(action *gobgpapi.NexthopAction) string {
	switch {
	case action == nil:
		return ""
	case action.GetPeerAddress():
		return "peer-address"
	case action.GetUnchanged():
		return "unchanged"
	default:
		return ""
	}
}

func canonicalizePolicyAssignment(assignment *gobgpapi.PolicyAssignment) canonicalPolicyAssignment {
	if assignment == nil {
		return canonicalPolicyAssignment{}
	}
	out := canonicalPolicyAssignment{
		Name:          strings.TrimSpace(assignment.GetName()),
		Direction:     int32(assignment.GetDirection()),
		DefaultAction: int32(assignment.GetDefaultAction()),
	}
	for _, policy := range assignment.GetPolicies() {
		if name := strings.TrimSpace(policy.GetName()); name != "" {
			out.Policies = append(out.Policies, name)
		}
	}
	sort.Strings(out.Policies)
	return out
}

func (c *Controller) refreshPeerImportPolicyAssignments(ctx context.Context, desired map[string]desiredPeer, addresses []string) error {
	addresses = append([]string(nil), addresses...)
	sort.Strings(addresses)
	for _, address := range addresses {
		peer, ok := desired[address]
		if !ok {
			continue
		}
		if _, err := c.Server.UpdatePeer(ctx, &gobgpapi.UpdatePeerRequest{Peer: goBGPPeer(peer)}); err != nil {
			return fmt.Errorf("refresh import policy assignment for peer %s: %w", address, err)
		}
	}
	return nil
}

func sortedDesiredPeers(peers map[string]desiredPeer) []desiredPeer {
	addresses := make([]string, 0, len(peers))
	for address := range peers {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	out := make([]desiredPeer, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, peers[address])
	}
	return out
}

func sortedDesiredDynamicPeers(peers map[string]desiredDynamicPeer) []desiredDynamicPeer {
	names := make([]string, 0, len(peers))
	for name := range peers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]desiredDynamicPeer, 0, len(names))
	for _, name := range names {
		out = append(out, peers[name])
	}
	return out
}

func (c *Controller) softResetImportPolicy(ctx context.Context, desired map[string]desiredPeer) error {
	var addresses []string
	for address := range desired {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	for _, address := range addresses {
		if err := c.Server.ResetPeer(ctx, &gobgpapi.ResetPeerRequest{
			Address:   address,
			Soft:      true,
			Direction: gobgpapi.ResetPeerRequest_IN,
		}); err != nil {
			return fmt.Errorf("soft reset import policy for peer %s: %w", address, err)
		}
	}
	return nil
}

func exportPolicyChangedPeers(applied, desired map[string]desiredPeer) []string {
	var addresses []string
	for address, peer := range desired {
		appliedPeer, ok := applied[address]
		if !ok {
			continue
		}
		if exportPolicyEqual(appliedPeer, peer) {
			continue
		}
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	return addresses
}

func exportPolicyEqual(a, b desiredPeer) bool {
	return strings.TrimSpace(a.ExportPolicyName) == strings.TrimSpace(b.ExportPolicyName) &&
		sameStringSet(cleanStrings(a.ExportPolicy.AllowedPrefixes), cleanStrings(b.ExportPolicy.AllowedPrefixes))
}

func (c *Controller) softResetExportPolicy(ctx context.Context, addresses []string) error {
	addresses = append([]string(nil), addresses...)
	sort.Strings(addresses)
	for _, address := range addresses {
		if err := c.Server.ResetPeer(ctx, &gobgpapi.ResetPeerRequest{
			Address:   address,
			Soft:      true,
			Direction: gobgpapi.ResetPeerRequest_OUT,
		}); err != nil {
			return fmt.Errorf("soft reset export policy for peer %s: %w", address, err)
		}
	}
	return nil
}

func desiredPeersFromApplied(localASN uint32, peers map[string]bgpdaemon.AppliedPeer) map[string]desiredPeer {
	out := map[string]desiredPeer{}
	for address, peer := range peers {
		gr := disabledGracefulRestartSpec()
		if peer.GracefulRestart != nil {
			enabled := peer.GracefulRestart.Enabled
			gr.Enabled = &enabled
			gr.RestartTime = fmt.Sprintf("%ds", peer.GracefulRestart.RestartTime)
			gr.StalePathTime = fmt.Sprintf("%ds", peer.GracefulRestart.StaleRoutesTime)
		}
		out[address] = desiredPeer{
			Address:                 peer.Address,
			ASN:                     peer.ASN,
			LocalASN:                localASN,
			Password:                peer.Password,
			BFD:                     peer.BFD,
			EbgpMultihop:            peer.EbgpMultihop,
			RouteReflectorClient:    peer.RouteReflectorClient,
			RouteReflectorClusterID: peer.RouteReflectorClusterID,
			Timers:                  routerapi.BGPTimersSpec{Profile: peer.TimersProfile},
			GracefulRestart:         gr,
			ConvergenceProfile:      peer.ConvergenceProfile,
			ImportPolicy: routerapi.BGPImportPolicySpec{
				AllowedPrefixes: peer.ImportPolicy.AllowedPrefixes,
				NextHopRewrite:  peer.ImportPolicy.NextHopRewrite,
			},
			ImportPolicyName: peer.ImportPolicyName,
			ExportPolicy: routerapi.BGPExportPolicySpec{
				AllowedPrefixes: peer.ExportPolicy.AllowedPrefixes,
			},
			ExportPolicyName: peer.ExportPolicyName,
		}
	}
	return out
}

func (c *Controller) buildAppliedConfig(spec routerapi.BGPRouterSpec, peers map[string]desiredPeer, advertisements map[string]bool, existingPaths []bgpdaemon.AppliedPath) bgpdaemon.AppliedConfig {
	out := bgpdaemon.AppliedConfig{
		Version:        bgpdaemon.AppliedVersion,
		Global:         appliedGlobalFromSpec(spec, c.Router),
		Peers:          map[string]bgpdaemon.AppliedPeer{},
		Advertisements: mapKeys(advertisements),
		Paths:          bgpdaemon.NonStaticPaths(existingPaths),
	}
	for prefix := range advertisements {
		out.Paths = append(out.Paths, bgpdaemon.StaticAppliedPath(prefix, c.pathUUIDs[prefix]))
	}
	for address, peer := range peers {
		out.Peers[address] = appliedPeer(peer)
	}
	return bgpdaemon.Normalize(out)
}

func appliedGlobalFromSpec(spec routerapi.BGPRouterSpec, router *routerapi.Router) bgpdaemon.AppliedGlobal {
	global := bgpdaemon.AppliedGlobal{
		ASN:              spec.ASN,
		RouterID:         strings.TrimSpace(spec.RouterID),
		ListenPort:       bgpListenPort(spec.Listen),
		ListenAddresses:  bgpListenAddresses(spec.Listen),
		Families:         []string{"ipv4-unicast"},
		UseMultiplePaths: true,
		ImportPolicy: bgpdaemon.AppliedImportPolicy{
			AllowedPrefixes:        cleanStrings(spec.ImportPolicy.AllowedPrefixes),
			AllowedPrefixLengthMin: spec.ImportPolicy.AllowedPrefixLengthMin,
			AllowedPrefixLengthMax: spec.ImportPolicy.AllowedPrefixLengthMax,
			RequiredCommunities:    cleanStrings(spec.ImportPolicy.RequiredCommunities),
			ForbiddenCommunities:   cleanStrings(spec.ImportPolicy.ForbiddenCommunities),
			NextHopRewrite:         importNextHopRewrite(spec.ImportPolicy),
		},
	}
	for _, family := range bgpFamiliesForRouter(router) {
		if family.GetAfi() == gobgpapi.Family_AFI_IP6 {
			global.Families = append(global.Families, "ipv6-unicast")
		}
	}
	if gr := gobgpGracefulRestart(spec); gr != nil {
		global.GracefulRestart = &bgpdaemon.AppliedGracefulRestart{Enabled: true, RestartTime: gr.GetRestartTime(), StaleRoutesTime: gr.GetStaleRoutesTime()}
	}
	return global
}

func appliedPeer(peer desiredPeer) bgpdaemon.AppliedPeer {
	out := bgpdaemon.AppliedPeer{
		Address:                 peer.Address,
		ASN:                     peer.ASN,
		Password:                peer.Password,
		BFD:                     peer.BFD,
		EbgpMultihop:            peer.EbgpMultihop,
		RouteReflectorClient:    peer.RouteReflectorClient,
		RouteReflectorClusterID: peer.RouteReflectorClusterID,
		TimersProfile:           strings.TrimSpace(peer.Timers.Profile),
		ConvergenceProfile:      peer.ConvergenceProfile,
		ImportPolicyName:        peer.ImportPolicyName,
		ImportPolicy: bgpdaemon.AppliedImportPolicy{
			AllowedPrefixes:        cleanStrings(peer.ImportPolicy.AllowedPrefixes),
			AllowedPrefixLengthMin: peer.ImportPolicy.AllowedPrefixLengthMin,
			AllowedPrefixLengthMax: peer.ImportPolicy.AllowedPrefixLengthMax,
			RequiredCommunities:    cleanStrings(peer.ImportPolicy.RequiredCommunities),
			ForbiddenCommunities:   cleanStrings(peer.ImportPolicy.ForbiddenCommunities),
			NextHopRewrite:         importNextHopRewrite(peer.ImportPolicy),
		},
		ExportPolicyName: peer.ExportPolicyName,
		ExportPolicy: bgpdaemon.AppliedExportPolicy{
			AllowedPrefixes: cleanStrings(peer.ExportPolicy.AllowedPrefixes),
		},
	}
	if gr := gobgpPeerGracefulRestart(peer); gr != nil {
		out.GracefulRestart = &bgpdaemon.AppliedGracefulRestart{Enabled: true, RestartTime: gr.GetRestartTime(), StaleRoutesTime: gr.GetStaleRoutesTime()}
	}
	return out
}

func dynamicPathExportPrefixes(paths []bgpdaemon.AppliedPath) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range paths {
		if strings.TrimSpace(path.Source) == "" || strings.TrimSpace(path.Source) == bgpdaemon.AppliedPathSourceStatic {
			continue
		}
		prefix := strings.TrimSpace(path.Prefix)
		if prefix == "" || seen[prefix] {
			continue
		}
		seen[prefix] = true
		out = append(out, prefix)
	}
	sort.Strings(out)
	return out
}

func mergeAllowedPrefixes(groups ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, group := range groups {
		for _, prefix := range cleanStrings(group) {
			if seen[prefix] {
				continue
			}
			seen[prefix] = true
			out = append(out, prefix)
		}
	}
	return out
}

func (c *Controller) observeBFDPeerStates(desired map[string]desiredPeer, liveEstablished map[string]bool) []bfdPeerResetTarget {
	if c.Store == nil || len(desired) == 0 {
		return nil
	}
	if c.bfdPeerSeenUp == nil {
		c.bfdPeerSeenUp = map[string]bool{}
	}
	if c.bfdPeerDownSince == nil {
		c.bfdPeerDownSince = map[string]time.Time{}
	}
	if c.bfdPeerResetPending == nil {
		c.bfdPeerResetPending = map[string]bool{}
	}
	if c.bfdPeerLastResetAt == nil {
		c.bfdPeerLastResetAt = map[string]time.Time{}
	}
	if c.bfdPeerResetError == nil {
		c.bfdPeerResetError = map[string]string{}
	}
	if c.bfdPeerResetAttempts == nil {
		c.bfdPeerResetAttempts = map[string]int{}
	}
	now := time.Now()
	var resetTargets []bfdPeerResetTarget
	for address, peer := range desired {
		state := c.bfdPeerState(peer.BFD, address)
		key := bfdPeerGateKey(peer.BFD, address)
		if strings.EqualFold(state, "Up") {
			c.bfdPeerSeenUp[key] = true
			delete(c.bfdPeerDownSince, key)
			delete(c.bfdPeerResetPending, key)
			delete(c.bfdPeerResetError, key)
			delete(c.bfdPeerResetAttempts, key)
			continue
		}
		if strings.EqualFold(state, "Down") && (c.bfdPeerSeenUp[key] || liveEstablished[address]) {
			if _, ok := c.bfdPeerDownSince[key]; !ok {
				c.bfdPeerDownSince[key] = now
				c.bfdPeerResetPending[key] = true
			}
			if liveEstablished[address] {
				c.bfdPeerResetPending[key] = true
			}
			if c.bfdPeerResetPending[key] {
				resetTargets = append(resetTargets, bfdPeerResetTarget{Key: key, Address: address})
			}
			continue
		}
		if c.bfdPeerResetPending[key] {
			resetTargets = append(resetTargets, bfdPeerResetTarget{Key: key, Address: address})
			continue
		}
		delete(c.bfdPeerDownSince, key)
		delete(c.bfdPeerResetPending, key)
		delete(c.bfdPeerResetError, key)
		delete(c.bfdPeerResetAttempts, key)
	}
	sort.Slice(resetTargets, func(i, j int) bool {
		if resetTargets[i].Address == resetTargets[j].Address {
			return resetTargets[i].Key < resetTargets[j].Key
		}
		return resetTargets[i].Address < resetTargets[j].Address
	})
	return resetTargets
}

func (c *Controller) liveEstablishedPeers(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	if c.Server == nil {
		return out, nil
	}
	if err := c.Server.ListPeer(ctx, &gobgpapi.ListPeerRequest{}, func(peer *gobgpapi.Peer) {
		address := peerAddress(peer)
		if address == "" {
			return
		}
		out[address] = peer.GetState().GetSessionState() == gobgpapi.PeerState_ESTABLISHED
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Controller) hardResetBFDDownPeers(ctx context.Context, targets []bfdPeerResetTarget) error {
	if c.bfdPeerResetPending == nil {
		c.bfdPeerResetPending = map[string]bool{}
	}
	if c.bfdPeerLastResetAt == nil {
		c.bfdPeerLastResetAt = map[string]time.Time{}
	}
	if c.bfdPeerResetError == nil {
		c.bfdPeerResetError = map[string]string{}
	}
	if c.bfdPeerResetAttempts == nil {
		c.bfdPeerResetAttempts = map[string]int{}
	}
	var resetErr error
	now := time.Now()
	for _, target := range targets {
		if !c.bfdPeerResetPending[target.Key] {
			continue
		}
		address := strings.TrimSpace(target.Address)
		if address == "" {
			continue
		}
		if !c.bfdResetDue(target.Key, now) {
			continue
		}
		c.bfdPeerResetAttempts[target.Key]++
		c.bfdPeerLastResetAt[target.Key] = now
		if err := c.Server.ResetPeer(ctx, &gobgpapi.ResetPeerRequest{
			Address:       address,
			Soft:          false,
			Direction:     gobgpapi.ResetPeerRequest_BOTH,
			Communication: "BFD session down",
		}); err != nil {
			c.bfdPeerResetError[target.Key] = err.Error()
			resetErr = errors.Join(resetErr, fmt.Errorf("hard reset peer %s after BFD Down: %w", address, err))
			continue
		}
		delete(c.bfdPeerResetPending, target.Key)
		delete(c.bfdPeerResetError, target.Key)
	}
	return resetErr
}

func (c *Controller) bfdResetDue(key string, now time.Time) bool {
	attempts := c.bfdPeerResetAttempts[key]
	if attempts <= 0 {
		return true
	}
	last := c.bfdPeerLastResetAt[key]
	if last.IsZero() {
		return true
	}
	return !now.Before(last.Add(bfdResetBackoff(attempts)))
}

func bfdResetBackoff(attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	backoff := bfdResetBackoffBase
	for i := 1; i < attempts; i++ {
		backoff *= 2
		if backoff >= bfdResetBackoffMax {
			return bfdResetBackoffMax
		}
	}
	if backoff > bfdResetBackoffMax {
		return bfdResetBackoffMax
	}
	return backoff
}

func bfdPeerGateKey(ref, address string) string {
	return strings.TrimSpace(ref) + "|" + strings.TrimSpace(address)
}

func (c *Controller) bfdPeerState(ref, address string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	kind, name, ok := strings.Cut(ref, "/")
	if !ok || kind != "BFD" || strings.TrimSpace(name) == "" {
		return ""
	}
	status := c.Store.ObjectStatus(routerapi.NetAPIVersion, "BFD", strings.TrimSpace(name))
	return bfdPeerState(status, address)
}

func bfdPeerState(status map[string]any, address string) string {
	address = strings.TrimSpace(address)
	peerStates, ok := status["peerStates"].(map[string]any)
	if ok {
		return strings.TrimSpace(fmt.Sprint(peerStates[address]))
	}
	if typed, ok := status["peerStates"].(map[string]string); ok {
		return strings.TrimSpace(typed[address])
	}
	for _, item := range statusSlice(status["peers"]) {
		itemAddress := strings.TrimSpace(fmt.Sprint(item["address"]))
		if itemAddress == address {
			return strings.TrimSpace(fmt.Sprint(item["state"]))
		}
	}
	return ""
}

func statusSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func mapKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func desiredPeerHash(peer desiredPeer) string {
	data, err := json.Marshal(peer)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func desiredDynamicPeerGroupHash(peer desiredDynamicPeer) string {
	return dynamicPeerGroupHash(goBGPDynamicPeerGroup(peer))
}

func dynamicPeerGroupHash(group *gobgpapi.PeerGroup) string {
	if group == nil {
		return ""
	}
	normalized := struct {
		Name                    string
		PeerASN                 uint32
		LocalASN                uint32
		Password                string
		EbgpMultihop            uint32
		RouteReflectorClient    bool
		RouteReflectorClusterID string
		TimersProfile           string
		ImportPolicies          []string
		ExportPolicies          []string
	}{
		Name:                    strings.TrimSpace(group.GetConf().GetPeerGroupName()),
		PeerASN:                 group.GetConf().GetPeerAsn(),
		LocalASN:                group.GetConf().GetLocalAsn(),
		Password:                group.GetConf().GetAuthPassword(),
		RouteReflectorClient:    group.GetRouteReflector().GetRouteReflectorClient(),
		RouteReflectorClusterID: strings.TrimSpace(group.GetRouteReflector().GetRouteReflectorClusterId()),
		TimersProfile:           timersProfile(group.GetTimers().GetConfig()),
	}
	if mh := group.GetEbgpMultihop(); mh.GetEnabled() {
		normalized.EbgpMultihop = mh.GetMultihopTtl()
	}
	for _, policy := range group.GetApplyPolicy().GetImportPolicy().GetPolicies() {
		if name := strings.TrimSpace(policy.GetName()); name != "" {
			normalized.ImportPolicies = append(normalized.ImportPolicies, name)
		}
	}
	for _, policy := range group.GetApplyPolicy().GetExportPolicy().GetPolicies() {
		if name := strings.TrimSpace(policy.GetName()); name != "" {
			normalized.ExportPolicies = append(normalized.ExportPolicies, name)
		}
	}
	sort.Strings(normalized.ImportPolicies)
	sort.Strings(normalized.ExportPolicies)
	data, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
		if isRouterdDynamicPeer(current) {
			continue
		}
		peer, ok := desired[address]
		if !ok {
			if err := c.Server.DeletePeer(ctx, &gobgpapi.DeletePeerRequest{Address: address}); err != nil {
				return changed, err
			}
			delete(live, address)
			delete(c.desiredPeerKeys, address)
			changed = true
			continue
		}
		if c.desiredPeerMatches(address, current, peer) {
			c.desiredPeerKeys[address] = peer
			continue
		}
		if _, err := c.Server.UpdatePeer(ctx, &gobgpapi.UpdatePeerRequest{Peer: goBGPPeer(peer), DoSoftResetIn: true}); err != nil {
			return changed, err
		}
		c.desiredPeerKeys[address] = peer
		changed = true
	}
	for address, peer := range desired {
		if current, ok := live[address]; ok {
			if c.desiredPeerMatches(address, current, peer) {
				c.desiredPeerKeys[address] = peer
				continue
			}
		}
		if err := c.Server.AddPeer(ctx, &gobgpapi.AddPeerRequest{Peer: goBGPPeer(peer)}); err != nil {
			return changed, err
		}
		c.desiredPeerKeys[address] = peer
		changed = true
	}
	return changed, nil
}

func isRouterdDynamicPeer(peer *gobgpapi.Peer) bool {
	return strings.HasPrefix(strings.TrimSpace(peer.GetConf().GetPeerGroup()), "routerd-dynamic-")
}

func (c *Controller) reconcileDynamicPeers(ctx context.Context, desired map[string]desiredDynamicPeer) error {
	liveGroups := map[string]*gobgpapi.PeerGroup{}
	if err := c.Server.ListPeerGroup(ctx, &gobgpapi.ListPeerGroupRequest{}, func(group *gobgpapi.PeerGroup) {
		name := strings.TrimSpace(group.GetConf().GetPeerGroupName())
		if strings.HasPrefix(name, "routerd-dynamic-") {
			liveGroups[name] = group
		}
	}); err != nil {
		return err
	}
	liveDynamic := map[string]map[string]bool{}
	if err := c.Server.ListDynamicNeighbor(ctx, &gobgpapi.ListDynamicNeighborRequest{}, func(neighbor *gobgpapi.DynamicNeighbor) {
		group := strings.TrimSpace(neighbor.GetPeerGroup())
		prefix := strings.TrimSpace(neighbor.GetPrefix())
		if group == "" || prefix == "" {
			return
		}
		if liveDynamic[group] == nil {
			liveDynamic[group] = map[string]bool{}
		}
		liveDynamic[group][prefix] = true
	}); err != nil {
		return err
	}
	desiredGroups := map[string]bool{}
	for _, peer := range sortedDesiredDynamicPeers(desired) {
		groupName := strings.TrimSpace(peer.PeerGroupName)
		if groupName == "" {
			continue
		}
		desiredGroups[groupName] = true
		if current := liveGroups[groupName]; current != nil {
			if dynamicPeerGroupHash(current) != desiredDynamicPeerGroupHash(peer) {
				if err := c.deleteDynamicPeerGroup(ctx, groupName, liveDynamic[groupName]); err != nil {
					return err
				}
				delete(liveDynamic, groupName)
				delete(liveGroups, groupName)
			}
		}
		if liveGroups[groupName] == nil {
			if err := c.Server.AddPeerGroup(ctx, &gobgpapi.AddPeerGroupRequest{PeerGroup: goBGPDynamicPeerGroup(peer)}); err != nil {
				return err
			}
		}
		wantPrefixes := map[string]bool{}
		for _, prefix := range peer.Prefixes {
			prefix = strings.TrimSpace(prefix)
			if prefix == "" {
				continue
			}
			wantPrefixes[prefix] = true
			if !liveDynamic[groupName][prefix] {
				if err := c.Server.AddDynamicNeighbor(ctx, &gobgpapi.AddDynamicNeighborRequest{DynamicNeighbor: &gobgpapi.DynamicNeighbor{Prefix: prefix, PeerGroup: groupName}}); err != nil {
					return err
				}
			}
		}
		for prefix := range liveDynamic[groupName] {
			if !wantPrefixes[prefix] {
				if err := c.Server.DeleteDynamicNeighbor(ctx, &gobgpapi.DeleteDynamicNeighborRequest{Prefix: prefix, PeerGroup: groupName}); err != nil {
					return err
				}
			}
		}
		if strings.TrimSpace(peer.Name) != "" && c.Store != nil {
			_ = c.Store.SaveObjectStatus(routerapi.NetAPIVersion, "BGPDynamicPeer", peer.Name, map[string]any{
				"phase":             "Ready",
				"peerGroup":         groupName,
				"sourcePrefixes":    append([]string(nil), peer.Prefixes...),
				"sourcePrefixCount": len(wantPrefixes),
				"observedAt":        time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
	}
	for groupName, prefixes := range liveDynamic {
		if desiredGroups[groupName] {
			continue
		}
		if err := c.deleteDynamicPeerGroup(ctx, groupName, prefixes); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) deleteDynamicPeerGroup(ctx context.Context, groupName string, prefixes map[string]bool) error {
	for prefix := range prefixes {
		if err := c.Server.DeleteDynamicNeighbor(ctx, &gobgpapi.DeleteDynamicNeighborRequest{Prefix: prefix, PeerGroup: groupName}); err != nil {
			return err
		}
	}
	return c.Server.DeletePeerGroup(ctx, &gobgpapi.DeletePeerGroupRequest{Name: groupName})
}

func (c *Controller) reconcileAdvertisements(ctx context.Context, spec routerapi.BGPRouterSpec, appliedPaths []bgpdaemon.AppliedPath) error {
	desired := advertisedPrefixes(spec)
	c.pathUUIDs = staticPathUUIDs(appliedPaths)
	for prefix := range c.pathUUIDs {
		if !desired[prefix] {
			if len(c.pathUUIDs[prefix]) > 0 {
				if err := c.Server.DeletePath(ctx, &gobgpapi.DeletePathRequest{TableType: gobgpapi.TableType_GLOBAL, Uuid: c.pathUUIDs[prefix]}); err != nil {
					return err
				}
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

func staticPathUUIDs(paths []bgpdaemon.AppliedPath) map[string][]byte {
	out := map[string][]byte{}
	for _, path := range bgpdaemon.Normalize(bgpdaemon.AppliedConfig{Paths: paths}).Paths {
		if path.Source != bgpdaemon.AppliedPathSourceStatic {
			continue
		}
		uuid, err := bgpdaemon.DecodeUUID(path.UUID)
		if err != nil {
			continue
		}
		out[path.Prefix] = uuid
	}
	return out
}

func dynamicImportPolicyExpanded(previous, current bgpdaemon.AppliedConfig) bool {
	previousPrefixes := appliedImportPolicyAllowedPrefixes(previous)
	currentPrefixes := appliedImportPolicyAllowedPrefixes(current)
	if sameStringSet(previousPrefixes, currentPrefixes) {
		return false
	}
	for _, prefix := range dynamicPathExportPrefixes(current.Paths) {
		if !stringSliceContains(previousPrefixes, prefix) && stringSliceContains(currentPrefixes, prefix) {
			return true
		}
	}
	return false
}

func appliedImportPolicyAllowedPrefixes(applied bgpdaemon.AppliedConfig) []string {
	var prefixes []string
	prefixes = append(prefixes, applied.Global.ImportPolicy.AllowedPrefixes...)
	for _, peer := range applied.Peers {
		prefixes = append(prefixes, peer.ImportPolicy.AllowedPrefixes...)
	}
	return cleanStrings(prefixes)
}

func isMissingGoBGPPath(err error) bool {
	return err != nil && strings.Contains(err.Error(), "can't find a specified path")
}

func appliedPathToGoBGPPath(appliedPath bgpdaemon.AppliedPath) (*gobgpapi.Path, error) {
	appliedPath = bgpdaemon.NormalizeAppliedPath(appliedPath)
	parsed, err := netip.ParsePrefix(appliedPath.Prefix)
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
	if appliedPath.Attrs.NextHop != "" {
		nextHop = appliedPath.Attrs.NextHop
	}
	nh, err := anypb.New(&gobgpapi.NextHopAttribute{NextHop: nextHop})
	if err != nil {
		return nil, err
	}
	attrs := []*anypb.Any{origin, nh}
	if appliedPath.Attrs.LocalPref > 0 {
		localPref, err := anypb.New(&gobgpapi.LocalPrefAttribute{LocalPref: appliedPath.Attrs.LocalPref})
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, localPref)
	}
	if appliedPath.Attrs.MED > 0 {
		med, err := anypb.New(&gobgpapi.MultiExitDiscAttribute{Med: appliedPath.Attrs.MED})
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, med)
	}
	communities, err := standardCommunities(appliedPath.Attrs.Communities)
	if err != nil {
		return nil, err
	}
	if len(communities) > 0 {
		attr, err := anypb.New(&gobgpapi.CommunitiesAttribute{Communities: communities})
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, attr)
	}
	return &gobgpapi.Path{Family: familyForPrefix(parsed), Nlri: nlri, Pattrs: attrs}, nil
}

func standardCommunities(values []string) ([]uint32, error) {
	var out []uint32
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, ":") {
			parts := strings.Split(value, ":")
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid community %q", value)
			}
			high, err := strconv.ParseUint(parts[0], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid community %q: %w", value, err)
			}
			low, err := strconv.ParseUint(parts[1], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid community %q: %w", value, err)
			}
			out = append(out, uint32(high)<<16|uint32(low))
			continue
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid community %q: %w", value, err)
		}
		out = append(out, uint32(parsed))
	}
	return out, nil
}

func stringSliceContains(values []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func (c *Controller) observeState(ctx context.Context, allowedImportPrefixes []allowedImportPrefix, desired map[string]desiredPeer) (bgpstate.State, []FIBRoute, map[string]string, error) {
	var state bgpstate.State
	var routes []FIBRoute
	livenessMarkers := map[string]string{}
	fibNextHopRewritePeers := peerAddressFIBRewritePeers(desired)
	fibPolicy := mobilityfib.NewSnapshot(c.Router, c.Store)
	claimAdmission := c.samDynamicClaimAdmission()
	if !routerHasBGPDynamicPeer(c.Router) {
		claimAdmission = samDynamicClaimAdmission{}
	}
	admissionTracker := newDynamicRouteAdmissionTracker(claimAdmission)
	var dynamicPeers []dynamicPeerObservation
	if err := c.Server.ListPeer(ctx, &gobgpapi.ListPeerRequest{EnableAdvertised: true}, func(peer *gobgpapi.Peer) {
		state.Peers = append(state.Peers, statePeer(peer))
		if observation, ok := dynamicPeerObservationFromPeer(peer, claimAdmission); ok {
			dynamicPeers = append(dynamicPeers, observation)
		}
	}); err != nil {
		return bgpstate.State{}, nil, nil, err
	}
	for _, family := range bgpFamiliesForRouter(c.Router) {
		err := c.Server.ListPath(ctx, &gobgpapi.ListPathRequest{TableType: gobgpapi.TableType_GLOBAL, Family: family}, func(dst *gobgpapi.Destination) {
			state.Prefixes = append(state.Prefixes, statePrefixes(dst)...)
			mergeStringMap(livenessMarkers, mobilityLivenessMarkersFromDestination(dst))
			routes = append(routes, fibRoutesFromDestination(dst, allowedImportPrefixes, fibNextHopRewritePeers, func(prefix netip.Prefix, identityAddress, _ string, communities []string) bool {
				if !fibPolicy.AdmitBGPPath(prefix, communities) {
					admissionTracker.Reject(identityAddress, prefix, "mobility-fib-policy")
					return false
				}
				return admissionTracker.Admit(identityAddress, prefix)
			})...)
		})
		if err != nil {
			return bgpstate.State{}, nil, nil, err
		}
	}
	routes = mergeFIBRoutes(routes)
	routes = c.applyMobilityPreferredSources(routes)
	limited, truncated := bgpstate.LimitPrefixes(bgpstate.Normalize(state), c.maxPrefixes())
	if truncated {
		limited.Prefixes = append(limited.Prefixes, bgpstate.Prefix{Prefix: "truncated", SelectionReason: "prefix limit reached"})
	}
	sort.Slice(dynamicPeers, func(i, j int) bool {
		if dynamicPeers[i].PeerGroup != dynamicPeers[j].PeerGroup {
			return dynamicPeers[i].PeerGroup < dynamicPeers[j].PeerGroup
		}
		return dynamicPeers[i].RemoteAddress < dynamicPeers[j].RemoteAddress
	})
	c.lastDynamicPeers = dynamicPeers
	c.lastDynamicAdmission = admissionTracker.Summary()
	return bgpstate.Normalize(limited), routes, livenessMarkers, nil
}

func routerHasBGPDynamicPeer(router *routerapi.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == routerapi.NetAPIVersion && resource.Kind == "BGPDynamicPeer" {
			return true
		}
	}
	return false
}

func (c *Controller) saveObservedStatuses(routerName string, spec routerapi.BGPRouterSpec, state bgpstate.State, routes []FIBRoute, changed bool, fibResult FIBSyncResult, livenessMarkers map[string]string) error {
	observedAt := time.Now().UTC()
	now := observedAt.Format(time.RFC3339Nano)
	peersByResource := c.peersByResource(state)
	fibRoutes := fibInstalledCount(fibResult)
	fibUnsupported := fibUnsupportedCount(fibResult)
	reconverging := c.bgpReconvergingStatus(observedAt, spec, state.Peers)
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
			if phase != "Established" && len(state.Peers) > 0 && fibUnsupported == 0 && reconverging.Active {
				phase = "Reconverging"
			}
			status := map[string]any{
				"phase":                phase,
				"backend":              "gobgp",
				"applyWith":            "routerd-bgp gRPC API",
				"daemon":               c.daemonSpec().Name,
				"daemonSocket":         c.daemonSpec().SocketPath,
				"appliedConfigHash":    bgpdaemon.Hash(c.appliedConfig),
				"changed":              changed,
				"dryRun":               c.DryRun,
				"peers":                state.Peers,
				"prefixes":             state.Prefixes,
				"observedCommunities":  observedCommunities(state.Prefixes),
				"livenessMarkers":      livenessMarkers,
				"establishedPeers":     established,
				"acceptedPrefixes":     len(state.Prefixes),
				"fibRoutes":            fibRoutes,
				"fibUnsupportedRoutes": fibUnsupported,
				"nextHopRewrite":       importNextHopRewrite(spec.ImportPolicy),
				"installedNextHops":    installedNextHops(routes, fibResult),
				"preferredSources":     fibResult.PreferredSource,
				"observedAt":           now,
				"conditions":           []map[string]any{{"type": "Observed", "status": "True", "reason": "GoBGPStatus"}},
			}
			mergeAnyMap(status, c.bfdResetRuntimeStatus())
			if len(fibResult.PreferredSourceSkipped) > 0 {
				status["preferredSourceSkipped"] = fibResult.PreferredSourceSkipped
				status["preferredSourceSkippedReason"] = fibResult.PreferredSourceSkippedReason
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
			} else if phase == "Reconverging" {
				applyBGPReconvergingStatus(status, reconverging)
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
			if phase != "Established" && len(peers) > 0 && reconverging.Active {
				phase = "Reconverging"
			}
			status := map[string]any{
				"phase":            phase,
				"backend":          "gobgp",
				"applyWith":        "routerd-bgp gRPC API",
				"daemon":           c.daemonSpec().Name,
				"daemonSocket":     c.daemonSpec().SocketPath,
				"peerConfigHashes": c.peerConfigHashes(resource),
				"changed":          changed,
				"dryRun":           c.DryRun,
				"peers":            peers,
				"establishedPeers": established,
				"observedAt":       now,
			}
			if phase == "Reconverging" {
				applyBGPReconvergingStatus(status, reconverging)
			}
			if err := c.Store.SaveObjectStatus(routerapi.NetAPIVersion, "BGPPeer", resource.Metadata.Name, status); err != nil {
				return err
			}
		case "BGPDynamicPeer":
			spec, err := resource.BGPDynamicPeerSpec()
			if err != nil {
				return err
			}
			groupName := "routerd-dynamic-" + sanitizeBGPPolicyName(resource.Metadata.Name)
			sourcePrefixes := cleanStrings(spec.Listen.SourcePrefixes)
			peerStatuses := dynamicPeerStatusMaps(groupName, c.lastDynamicPeers, c.lastDynamicAdmission)
			accepted, rejected := dynamicPeerAdmissionTotals(peerStatuses)
			status := map[string]any{
				"phase":                "Ready",
				"backend":              "gobgp",
				"applyWith":            "routerd-bgp gRPC API",
				"daemon":               c.daemonSpec().Name,
				"daemonSocket":         c.daemonSpec().SocketPath,
				"peerGroup":            groupName,
				"sourcePrefixes":       sourcePrefixes,
				"sourcePrefixCount":    len(sourcePrefixes),
				"discoveredPeers":      peerStatuses,
				"discoveredPeerCount":  len(peerStatuses),
				"acceptedRouteCount":   accepted,
				"rejectedRouteCount":   rejected,
				"rejectedRouteSummary": c.lastDynamicAdmission.ReasonCounts,
				"observedAt":           now,
				"conditions": []map[string]any{{
					"type":    "Observed",
					"status":  "True",
					"reason":  "GoBGPDynamicPeerStatus",
					"message": "Route counters are routerd-side admission/FIB observation counters; GoBGP may not expose exact rejected import-policy counters.",
				}},
			}
			if err := c.Store.SaveObjectStatus(routerapi.NetAPIVersion, "BGPDynamicPeer", resource.Metadata.Name, status); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) bfdResetRuntimeStatus() map[string]any {
	if len(c.bfdPeerResetPending) == 0 && len(c.bfdPeerResetError) == 0 {
		return nil
	}
	pendingPeers := make([]string, 0, len(c.bfdPeerResetPending))
	pendingSince := map[string]string{}
	attempts := map[string]int{}
	lastResetAt := map[string]string{}
	lastError := map[string]string{}
	for key, pending := range c.bfdPeerResetPending {
		if !pending {
			continue
		}
		pendingPeers = append(pendingPeers, key)
		if since := c.bfdPeerDownSince[key]; !since.IsZero() {
			pendingSince[key] = since.UTC().Format(time.RFC3339Nano)
		}
		if count := c.bfdPeerResetAttempts[key]; count > 0 {
			attempts[key] = count
		}
		if at := c.bfdPeerLastResetAt[key]; !at.IsZero() {
			lastResetAt[key] = at.UTC().Format(time.RFC3339Nano)
		}
		if err := strings.TrimSpace(c.bfdPeerResetError[key]); err != "" {
			lastError[key] = err
		}
	}
	sort.Strings(pendingPeers)
	status := map[string]any{
		"bfdResetPending":      len(pendingPeers) > 0,
		"bfdResetPendingPeers": pendingPeers,
		"bfdResetPendingCount": len(pendingPeers),
	}
	if len(pendingSince) > 0 {
		status["bfdResetPendingSince"] = pendingSince
	}
	if len(attempts) > 0 {
		status["bfdResetAttemptCount"] = attempts
	}
	if len(lastResetAt) > 0 {
		status["bfdResetLastAttemptAt"] = lastResetAt
	}
	if len(lastError) > 0 {
		status["bfdResetLastError"] = lastError
	}
	return status
}

type bgpReconvergingStatus struct {
	Active    bool
	StartedAt time.Time
	Until     time.Time
}

func (c *Controller) bgpReconvergingStatus(now time.Time, spec routerapi.BGPRouterSpec, peers []bgpstate.Peer) bgpReconvergingStatus {
	gr := gobgpGracefulRestart(spec)
	if gr == nil {
		return bgpReconvergingStatus{}
	}
	window := time.Duration(gr.GetStaleRoutesTime()) * time.Second
	if window <= 0 {
		return bgpReconvergingStatus{}
	}
	if c != nil && !c.startedAt.IsZero() {
		startedAt := c.startedAt.UTC()
		until := startedAt.Add(window)
		if now.Before(until) {
			return bgpReconvergingStatus{
				Active:    true,
				StartedAt: startedAt,
				Until:     until,
			}
		}
	}
	for _, peer := range peers {
		if peer.Established || strings.TrimSpace(peer.LastErrorAt) == "" {
			continue
		}
		errorAt, err := time.Parse(time.RFC3339Nano, peer.LastErrorAt)
		if err != nil {
			continue
		}
		errorAt = errorAt.UTC()
		until := errorAt.Add(window)
		if now.Before(until) {
			return bgpReconvergingStatus{
				Active:    true,
				StartedAt: errorAt,
				Until:     until,
			}
		}
	}
	return bgpReconvergingStatus{}
}

func applyBGPReconvergingStatus(status map[string]any, reconverging bgpReconvergingStatus) {
	if status == nil || !reconverging.Active {
		return
	}
	status["reason"] = "GoBGPReconverging"
	status["pendingReason"] = "GoBGPReconverging"
	status["message"] = "routerd-bgp restarted recently; BGP peers are still within the graceful-restart reconvergence window"
	status["reconvergingSince"] = reconverging.StartedAt.Format(time.RFC3339Nano)
	status["reconvergingUntil"] = reconverging.Until.Format(time.RFC3339Nano)
	condition := map[string]any{
		"type":    "Reconverging",
		"status":  "True",
		"reason":  "GoBGPReconverging",
		"message": "routerd-bgp restarted recently; wait for peers to re-establish before treating reduced ECMP width as a persistent fault",
	}
	if existing, ok := status["conditions"].([]map[string]any); ok {
		status["conditions"] = append(existing, condition)
	} else {
		status["conditions"] = []map[string]any{condition}
	}
}

func mergeAnyMap(dst, src map[string]any) {
	if dst == nil || len(src) == 0 {
		return
	}
	for key, value := range src {
		dst[key] = value
	}
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

func (c *Controller) peerConfigHashes(resource routerapi.Resource) map[string]string {
	spec, err := resource.BGPPeerSpec()
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, address := range spec.Peers {
		address = strings.TrimSpace(address)
		if peer, ok := c.desiredPeerKeys[address]; ok {
			out[address] = desiredPeerHash(peer)
		}
	}
	return out
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
	peerType := gobgpapi.PeerType_EXTERNAL
	if peer.LocalASN != 0 && peer.ASN == peer.LocalASN {
		peerType = gobgpapi.PeerType_INTERNAL
	}
	out := &gobgpapi.Peer{
		Conf: &gobgpapi.PeerConf{
			NeighborAddress: peer.Address,
			PeerAsn:         peer.ASN,
			AuthPassword:    peer.Password,
			Type:            peerType,
			SendCommunity:   3,
		},
		Timers: &gobgpapi.Timers{Config: goBGPTimers(peer.Timers)},
		AfiSafis: []*gobgpapi.AfiSafi{
			goBGPAFISAFI(ipv4Family()),
			goBGPAFISAFI(ipv6Family()),
		},
	}
	if gr := gobgpPeerGracefulRestart(peer); gr != nil {
		out.GracefulRestart = gr
	}
	if peer.EbgpMultihop > 1 {
		out.EbgpMultihop = &gobgpapi.EbgpMultihop{Enabled: true, MultihopTtl: uint32(peer.EbgpMultihop)}
	}
	if peer.RouteReflectorClient {
		out.RouteReflector = &gobgpapi.RouteReflector{
			RouteReflectorClient:    true,
			RouteReflectorClusterId: strings.TrimSpace(peer.RouteReflectorClusterID),
		}
	}
	applyPolicy := &gobgpapi.ApplyPolicy{}
	if len(importPolicyPrefixes(peer.ImportPolicy)) > 0 && strings.TrimSpace(peer.ImportPolicyName) != "" {
		applyPolicy.ImportPolicy = peerImportPolicyAssignment(peer.ImportPolicyName)
	}
	if len(exportPolicyPrefixes(peer.ExportPolicy)) > 0 && strings.TrimSpace(peer.ExportPolicyName) != "" {
		applyPolicy.ExportPolicy = peerExportPolicyAssignment(peer.ExportPolicyName)
	}
	if applyPolicy.ImportPolicy != nil || applyPolicy.ExportPolicy != nil {
		out.ApplyPolicy = applyPolicy
	}
	return out
}

func goBGPDynamicPeerGroup(peer desiredDynamicPeer) *gobgpapi.PeerGroup {
	peerType := gobgpapi.PeerType_EXTERNAL
	if peer.LocalASN != 0 && peer.ASN == peer.LocalASN {
		peerType = gobgpapi.PeerType_INTERNAL
	}
	out := &gobgpapi.PeerGroup{
		Conf: &gobgpapi.PeerGroupConf{
			PeerGroupName: strings.TrimSpace(peer.PeerGroupName),
			PeerAsn:       peer.ASN,
			LocalAsn:      peer.LocalASN,
			AuthPassword:  peer.Password,
			Type:          peerType,
			SendCommunity: 3,
		},
		Timers: &gobgpapi.Timers{Config: goBGPTimers(peer.Timers)},
		AfiSafis: []*gobgpapi.AfiSafi{
			goBGPAFISAFI(ipv4Family()),
			goBGPAFISAFI(ipv6Family()),
		},
	}
	if gr := gobgpDynamicPeerGracefulRestart(peer); gr != nil {
		out.GracefulRestart = gr
	}
	if peer.EbgpMultihop > 1 {
		out.EbgpMultihop = &gobgpapi.EbgpMultihop{Enabled: true, MultihopTtl: uint32(peer.EbgpMultihop)}
	}
	if peer.RouteReflectorClient {
		out.RouteReflector = &gobgpapi.RouteReflector{
			RouteReflectorClient:    true,
			RouteReflectorClusterId: strings.TrimSpace(peer.RouteReflectorClusterID),
		}
	}
	applyPolicy := &gobgpapi.ApplyPolicy{}
	if len(importPolicyPrefixes(peer.ImportPolicy)) > 0 && strings.TrimSpace(peer.ImportPolicyName) != "" {
		applyPolicy.ImportPolicy = peerImportPolicyAssignment(peer.ImportPolicyName)
	}
	if len(exportPolicyPrefixes(peer.ExportPolicy)) > 0 && strings.TrimSpace(peer.ExportPolicyName) != "" {
		applyPolicy.ExportPolicy = peerExportPolicyAssignment(peer.ExportPolicyName)
	}
	if applyPolicy.ImportPolicy != nil || applyPolicy.ExportPolicy != nil {
		out.ApplyPolicy = applyPolicy
	}
	return out
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

func timersProfile(config *gobgpapi.TimersConfig) string {
	switch {
	case config.GetConnectRetry() == 1 && config.GetHoldTime() == 9 && config.GetKeepaliveInterval() == 3:
		return "fast"
	case config.GetConnectRetry() == 30 && config.GetHoldTime() == 180 && config.GetKeepaliveInterval() == 60:
		return "slow"
	default:
		return "default"
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

func gobgpPeerGracefulRestart(peer desiredPeer) *gobgpapi.GracefulRestart {
	enabled := true
	if peer.ConvergenceProfile == "fast" {
		enabled = false
	}
	if peer.GracefulRestart.Enabled != nil {
		enabled = *peer.GracefulRestart.Enabled
	}
	if !enabled {
		return nil
	}
	return &gobgpapi.GracefulRestart{Enabled: true, RestartTime: uint32(durationSeconds(peer.GracefulRestart.RestartTime, 120)), StaleRoutesTime: uint32(durationSeconds(peer.GracefulRestart.StalePathTime, 360))}
}

func gobgpDynamicPeerGracefulRestart(peer desiredDynamicPeer) *gobgpapi.GracefulRestart {
	enabled := true
	if peer.GracefulRestart.Enabled != nil {
		enabled = *peer.GracefulRestart.Enabled
	}
	if !enabled {
		return nil
	}
	return &gobgpapi.GracefulRestart{Enabled: true, RestartTime: uint32(durationSeconds(peer.GracefulRestart.RestartTime, 120)), StaleRoutesTime: uint32(durationSeconds(peer.GracefulRestart.StalePathTime, 360))}
}

func (c *Controller) desiredPeerMatches(address string, _ *gobgpapi.Peer, desired desiredPeer) bool {
	if cached, ok := c.desiredPeerKeys[address]; ok {
		return stableDesiredPeerEqual(cached, desired)
	}
	if applied, ok := c.appliedPeerKeys[address]; ok {
		return stableDesiredPeerEqual(applied, desired)
	}
	// GoBGP's ListPeer response is not a reliable echo of all configured peer
	// fields after routerd reconnects to a long-lived routerd-bgp daemon. If the
	// daemon has no applied-state proof for this peer, do not silently adopt the
	// address-only live peer; reconcilePeers will UpdatePeer explicitly.
	return false
}

func stableDesiredPeerEqual(a, b desiredPeer) bool {
	return reflect.DeepEqual(stableDesiredPeerKey(a), stableDesiredPeerKey(b))
}

func stableDesiredPeerKey(peer desiredPeer) desiredPeer {
	peer.GracefulRestart = canonicalGracefulRestartSpec(peer.GracefulRestart, peer.ConvergenceProfile)
	peer.ImportPolicy.NextHopRewrite = importNextHopRewrite(peer.ImportPolicy)
	peer.ImportPolicy.AllowedPrefixes = nil
	peer.ExportPolicy.AllowedPrefixes = nil
	return peer
}

func canonicalGracefulRestartSpec(spec routerapi.BGPGracefulRestartSpec, convergenceProfile string) routerapi.BGPGracefulRestartSpec {
	enabled := true
	if convergenceProfile == "fast" {
		enabled = false
	}
	if spec.Enabled != nil {
		enabled = *spec.Enabled
	}
	out := routerapi.BGPGracefulRestartSpec{Enabled: boolValue(enabled)}
	if !enabled {
		return out
	}
	out.RestartTime = fmt.Sprintf("%ds", durationSeconds(spec.RestartTime, 120))
	out.StalePathTime = fmt.Sprintf("%ds", durationSeconds(spec.StalePathTime, 360))
	return out
}

func disabledGracefulRestartSpec() routerapi.BGPGracefulRestartSpec {
	return routerapi.BGPGracefulRestartSpec{Enabled: boolValue(false)}
}

func boolValue(value bool) *bool {
	return &value
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, value := range a {
		seen[strings.TrimSpace(value)]++
	}
	for _, value := range b {
		key := strings.TrimSpace(value)
		if seen[key] == 0 {
			return false
		}
		seen[key]--
	}
	return true
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
		if bgpstate.HasCommunity(pathCommunities(path), bgpstate.MobilityCommunityNodeLiveness) {
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

type allowedImportPrefix struct {
	Prefix netip.Prefix
	Min    int
	Max    int
}

func fibRoutesFromStatePrefixes(prefixes []bgpstate.Prefix, allowed []allowedImportPrefix, admit func(netip.Prefix, string, string, []string) bool) []FIBRoute {
	type stateRoute struct {
		nextHops        map[string]bool
		retainOnMissing bool
	}
	byPrefix := map[string]stateRoute{}
	for _, prefix := range prefixes {
		if !prefix.Best || !prefix.Valid || strings.TrimSpace(prefix.Prefix) == "" || bgpstate.HasCommunity(prefix.Communities, bgpstate.MobilityCommunityNodeLiveness) {
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
		if admit != nil && !admit(parsed, nextHop, nextHop, prefix.Communities) {
			continue
		}
		key := parsed.String()
		route := byPrefix[key]
		if route.nextHops == nil {
			route.nextHops = map[string]bool{}
		}
		route.nextHops[nextHop] = true
		byPrefix[key] = route
	}
	var out []FIBRoute
	for prefix, route := range byPrefix {
		var hops []string
		for hop := range route.nextHops {
			hops = append(hops, hop)
		}
		sort.Strings(hops)
		out = append(out, FIBRoute{Prefix: prefix, NextHops: hops, RetainOnMissing: route.retainOnMissing})
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

func fibRoutesFromDestination(dst *gobgpapi.Destination, allowed []allowedImportPrefix, peerAddressRewrite map[string]bool, admit func(netip.Prefix, string, string, []string) bool) []FIBRoute {
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
		if bgpstate.HasCommunity(pathCommunities(path), bgpstate.MobilityCommunityNodeLiveness) {
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
		nextHop := strings.TrimSpace(pathFIBNextHop(path, peerAddressRewrite))
		if nextHop == "" || nextHop == "0.0.0.0" || nextHop == "::" {
			continue
		}
		identityAddress := firstNonEmpty(normalizedPathNeighbor(path), nextHop)
		communities := pathCommunities(path)
		if admit != nil && !admit(parsed, identityAddress, nextHop, communities) {
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

func peerAddressFIBRewritePeers(desired map[string]desiredPeer) map[string]bool {
	out := map[string]bool{}
	for address, peer := range desired {
		if importNextHopRewrite(peer.ImportPolicy) != "peer-address" || len(importPolicyPrefixes(peer.ImportPolicy)) == 0 {
			continue
		}
		if parsed, err := netip.ParseAddr(strings.TrimSpace(address)); err == nil {
			out[parsed.String()] = true
		}
	}
	return out
}

func pathFIBNextHop(path *gobgpapi.Path, peerAddressRewrite map[string]bool) string {
	if len(peerAddressRewrite) > 0 {
		if neighbor := normalizedPathNeighbor(path); neighbor != "" && peerAddressRewrite[neighbor] {
			return neighbor
		}
	}
	return pathNextHop(path)
}

func normalizedPathNeighbor(path *gobgpapi.Path) string {
	neighbor := strings.TrimSpace(path.GetNeighborIp())
	if neighbor == "" {
		return ""
	}
	parsed, err := netip.ParseAddr(neighbor)
	if err != nil {
		return neighbor
	}
	return parsed.String()
}

func mergeFIBRoutes(routes []FIBRoute) []FIBRoute {
	type mergedRoute struct {
		nextHops        map[string]bool
		preferredSource string
		retainOnMissing bool
	}
	byPrefix := map[string]mergedRoute{}
	for _, route := range routes {
		prefix := normalizeRoutePrefix(route.Prefix)
		if prefix == "" {
			continue
		}
		merged := byPrefix[prefix]
		if merged.nextHops == nil {
			merged.nextHops = map[string]bool{}
		}
		for _, nextHop := range normalizeRouteNextHops(route.NextHops) {
			merged.nextHops[nextHop] = true
		}
		merged.retainOnMissing = merged.retainOnMissing || route.RetainOnMissing
		source := strings.TrimSpace(route.PreferredSource)
		if source != "" {
			if merged.preferredSource == "" {
				merged.preferredSource = source
			} else if merged.preferredSource != source {
				merged.preferredSource = ""
			}
		}
		byPrefix[prefix] = merged
	}
	out := make([]FIBRoute, 0, len(byPrefix))
	for prefix, merged := range byPrefix {
		var hops []string
		for hop := range merged.nextHops {
			hops = append(hops, hop)
		}
		sort.Strings(hops)
		out = append(out, FIBRoute{Prefix: prefix, NextHops: hops, PreferredSource: merged.preferredSource, RetainOnMissing: merged.retainOnMissing})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

func (c *Controller) applyMobilityPreferredSources(routes []FIBRoute) []FIBRoute {
	sources := c.mobilityPreferredSources()
	if len(sources) == 0 {
		return routes
	}
	out := make([]FIBRoute, 0, len(routes))
	for _, route := range routes {
		route.Prefix = normalizeRoutePrefix(route.Prefix)
		if route.Prefix == "" {
			continue
		}
		routePrefix, err := netip.ParsePrefix(route.Prefix)
		if err != nil {
			continue
		}
		for _, source := range sources {
			if source.prefix.Contains(routePrefix.Addr()) && route.Prefix != source.addressPrefix {
				route.PreferredSource = source.address
				break
			}
		}
		out = append(out, route)
	}
	return out
}

type mobilityPreferredSource struct {
	prefix        netip.Prefix
	address       string
	addressPrefix string
}

func (c *Controller) mobilityPreferredSources() []mobilityPreferredSource {
	if c.Router == nil {
		return nil
	}
	selfByGroup := map[string]string{}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != routerapi.FederationAPIVersion || res.Kind != "EventGroup" {
			continue
		}
		spec, err := res.EventGroupSpec()
		if err == nil {
			selfByGroup[res.Metadata.Name] = strings.TrimSpace(spec.NodeName)
		}
	}
	var out []mobilityPreferredSource
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != routerapi.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil || mobilityDeliveryMode(spec) != "bgp" {
			continue
		}
		self := selfByGroup[strings.TrimSpace(spec.GroupRef)]
		if self == "" {
			continue
		}
		spec, _, err = mobilityconfig.NormalizeMobilityPool(spec, self)
		if err != nil {
			continue
		}
		pool, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
		if err != nil {
			continue
		}
		pool = pool.Masked()
		var owned []netip.Prefix
		for _, member := range spec.Members {
			if strings.TrimSpace(member.NodeRef) != self {
				continue
			}
			for _, raw := range member.StaticOwnedAddresses {
				prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
				if err != nil {
					continue
				}
				prefix = prefix.Masked()
				if prefix.Addr().Is4() && prefix.Bits() == 32 && pool.Contains(prefix.Addr()) {
					owned = append(owned, prefix)
				}
			}
		}
		if len(owned) != 1 {
			continue
		}
		out = append(out, mobilityPreferredSource{prefix: pool, address: owned[0].Addr().String(), addressPrefix: owned[0].String()})
	}
	return out
}

func mobilityDeliveryMode(spec routerapi.MobilityPoolSpec) string {
	mode := strings.TrimSpace(spec.DeliveryPolicy.Mode)
	if mode == "" {
		return "bgp"
	}
	return mode
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

func importAllowedPrefixes(spec routerapi.BGPRouterSpec, peers map[string]desiredPeer) []allowedImportPrefix {
	out := importAllowedPrefixesFromPolicy(spec.ImportPolicy)
	for _, peer := range peers {
		out = append(out, importAllowedPrefixesFromPolicy(peer.ImportPolicy)...)
	}
	return out
}

func importAllowedPrefixesFromApplied(applied bgpdaemon.AppliedConfig) []allowedImportPrefix {
	var out []allowedImportPrefix
	out = append(out, importAllowedPrefixesFromAppliedPolicy(applied.Global.ImportPolicy)...)
	for _, peer := range applied.Peers {
		out = append(out, importAllowedPrefixesFromAppliedPolicy(peer.ImportPolicy)...)
	}
	return out
}

func importAllowedPrefixesFromAppliedAndDynamic(applied bgpdaemon.AppliedConfig, dynamicPeers map[string]desiredDynamicPeer) []allowedImportPrefix {
	out := importAllowedPrefixesFromApplied(applied)
	for _, peer := range dynamicPeers {
		out = append(out, importAllowedPrefixesFromPolicy(peer.ImportPolicy)...)
	}
	return out
}

func importAllowedPrefixesFromPolicy(spec routerapi.BGPImportPolicySpec) []allowedImportPrefix {
	var out []allowedImportPrefix
	for _, value := range cleanStrings(spec.AllowedPrefixes) {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		minLen, maxLen := importPolicyLengthBounds(spec, prefix)
		out = append(out, allowedImportPrefix{Prefix: prefix, Min: minLen, Max: maxLen})
	}
	return out
}

func importAllowedPrefixesFromAppliedPolicy(spec bgpdaemon.AppliedImportPolicy) []allowedImportPrefix {
	var out []allowedImportPrefix
	for _, value := range cleanStrings(spec.AllowedPrefixes) {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		minLen, maxLen := importPolicyLengthBounds(routerapi.BGPImportPolicySpec{
			AllowedPrefixLengthMin: spec.AllowedPrefixLengthMin,
			AllowedPrefixLengthMax: spec.AllowedPrefixLengthMax,
		}, prefix)
		out = append(out, allowedImportPrefix{Prefix: prefix, Min: minLen, Max: maxLen})
	}
	return out
}

func importPolicyLengthBounds(spec routerapi.BGPImportPolicySpec, prefix netip.Prefix) (int, int) {
	minLen := prefix.Bits()
	maxLen := int(bgpPrefixMaxLength(prefix))
	if spec.AllowedPrefixLengthMin > 0 {
		minLen = spec.AllowedPrefixLengthMin
	}
	if spec.AllowedPrefixLengthMax > 0 {
		maxLen = spec.AllowedPrefixLengthMax
	}
	return minLen, maxLen
}

func importNextHopRewrite(spec routerapi.BGPImportPolicySpec) string {
	switch strings.TrimSpace(spec.NextHopRewrite) {
	case "unchanged":
		return "unchanged"
	default:
		return "peer-address"
	}
}

func importPolicyKey(spec routerapi.BGPImportPolicySpec) string {
	normalized := routerapi.BGPImportPolicySpec{
		AllowedPrefixes:        cleanStrings(spec.AllowedPrefixes),
		AllowedPrefixLengthMin: spec.AllowedPrefixLengthMin,
		AllowedPrefixLengthMax: spec.AllowedPrefixLengthMax,
		NextHopRewrite:         importNextHopRewrite(spec),
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func bgpPoliciesKey(importSpec routerapi.BGPImportPolicySpec, peers map[string]desiredPeer, dynamicPeers map[string]desiredDynamicPeer) string {
	type peerPolicyKey struct {
		Address               string   `json:"address"`
		ImportPolicyName      string   `json:"importPolicyName,omitempty"`
		ImportAllowedPrefixes []string `json:"importAllowedPrefixes,omitempty"`
		ImportLengthMin       int      `json:"importLengthMin,omitempty"`
		ImportLengthMax       int      `json:"importLengthMax,omitempty"`
		ImportNextHopRewrite  string   `json:"importNextHopRewrite,omitempty"`
		ExportPolicyName      string   `json:"exportPolicyName,omitempty"`
		ExportAllowedPrefixes []string `json:"exportAllowedPrefixes,omitempty"`
	}
	type dynamicPeerPolicyKey struct {
		PeerGroupName         string   `json:"peerGroupName"`
		Prefixes              []string `json:"prefixes,omitempty"`
		ImportPolicyName      string   `json:"importPolicyName,omitempty"`
		ImportAllowedPrefixes []string `json:"importAllowedPrefixes,omitempty"`
		ImportLengthMin       int      `json:"importLengthMin,omitempty"`
		ImportLengthMax       int      `json:"importLengthMax,omitempty"`
		ImportNextHopRewrite  string   `json:"importNextHopRewrite,omitempty"`
		ExportPolicyName      string   `json:"exportPolicyName,omitempty"`
		ExportAllowedPrefixes []string `json:"exportAllowedPrefixes,omitempty"`
	}
	normalized := struct {
		Import       routerapi.BGPImportPolicySpec `json:"import"`
		Peers        []peerPolicyKey               `json:"peers,omitempty"`
		DynamicPeers []dynamicPeerPolicyKey        `json:"dynamicPeers,omitempty"`
	}{
		Import: routerapi.BGPImportPolicySpec{
			AllowedPrefixes:        cleanStrings(importSpec.AllowedPrefixes),
			AllowedPrefixLengthMin: importSpec.AllowedPrefixLengthMin,
			AllowedPrefixLengthMax: importSpec.AllowedPrefixLengthMax,
			NextHopRewrite:         importNextHopRewrite(importSpec),
		},
	}
	for _, peer := range sortedDesiredPeers(peers) {
		importPrefixes := cleanStrings(peer.ImportPolicy.AllowedPrefixes)
		exportPrefixes := cleanStrings(peer.ExportPolicy.AllowedPrefixes)
		if len(importPrefixes) == 0 && len(exportPrefixes) == 0 {
			continue
		}
		normalized.Peers = append(normalized.Peers, peerPolicyKey{
			Address:               strings.TrimSpace(peer.Address),
			ImportPolicyName:      strings.TrimSpace(peer.ImportPolicyName),
			ImportAllowedPrefixes: importPrefixes,
			ImportLengthMin:       peer.ImportPolicy.AllowedPrefixLengthMin,
			ImportLengthMax:       peer.ImportPolicy.AllowedPrefixLengthMax,
			ImportNextHopRewrite:  importNextHopRewrite(peer.ImportPolicy),
			ExportPolicyName:      strings.TrimSpace(peer.ExportPolicyName),
			ExportAllowedPrefixes: exportPrefixes,
		})
	}
	for _, peer := range sortedDesiredDynamicPeers(dynamicPeers) {
		importPrefixes := cleanStrings(peer.ImportPolicy.AllowedPrefixes)
		exportPrefixes := cleanStrings(peer.ExportPolicy.AllowedPrefixes)
		normalized.DynamicPeers = append(normalized.DynamicPeers, dynamicPeerPolicyKey{
			PeerGroupName:         strings.TrimSpace(peer.PeerGroupName),
			Prefixes:              append([]string(nil), peer.Prefixes...),
			ImportPolicyName:      strings.TrimSpace(peer.ImportPolicyName),
			ImportAllowedPrefixes: importPrefixes,
			ImportLengthMin:       peer.ImportPolicy.AllowedPrefixLengthMin,
			ImportLengthMax:       peer.ImportPolicy.AllowedPrefixLengthMax,
			ImportNextHopRewrite:  importNextHopRewrite(peer.ImportPolicy),
			ExportPolicyName:      strings.TrimSpace(peer.ExportPolicyName),
			ExportAllowedPrefixes: exportPrefixes,
		})
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func nextHopRewriteAction(spec routerapi.BGPImportPolicySpec) *gobgpapi.NexthopAction {
	if importNextHopRewrite(spec) == "unchanged" {
		return &gobgpapi.NexthopAction{Unchanged: true}
	}
	return &gobgpapi.NexthopAction{PeerAddress: true}
}

func globalImportPolicyAssignment(policyName string, includePolicy bool) *gobgpapi.PolicyAssignment {
	return &gobgpapi.PolicyAssignment{
		Name:          "global",
		Direction:     gobgpapi.PolicyDirection_IMPORT,
		DefaultAction: gobgpapi.RouteAction_ACCEPT,
	}
}

func peerImportPolicyAssignment(policyName string) *gobgpapi.PolicyAssignment {
	assignment := &gobgpapi.PolicyAssignment{
		Direction:     gobgpapi.PolicyDirection_IMPORT,
		DefaultAction: gobgpapi.RouteAction_REJECT,
	}
	if strings.TrimSpace(policyName) != "" {
		assignment.Policies = []*gobgpapi.Policy{{Name: strings.TrimSpace(policyName)}}
	}
	return assignment
}

func importPolicyPrefixes(spec routerapi.BGPImportPolicySpec) []*gobgpapi.Prefix {
	return bgpImportPolicyPrefixes(spec)
}

func exportPolicyPrefixes(spec routerapi.BGPExportPolicySpec) []*gobgpapi.Prefix {
	return bgpPolicyPrefixes(spec.AllowedPrefixes)
}

func bgpPolicyPrefixes(values []string) []*gobgpapi.Prefix {
	var out []*gobgpapi.Prefix
	for _, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		bits := uint32(prefix.Bits())
		out = append(out, &gobgpapi.Prefix{
			IpPrefix:      prefix.String(),
			MaskLengthMin: bits,
			MaskLengthMax: bgpPrefixMaxLength(prefix),
		})
	}
	return out
}

func bgpImportPolicyPrefixes(spec routerapi.BGPImportPolicySpec) []*gobgpapi.Prefix {
	var out []*gobgpapi.Prefix
	for _, value := range spec.AllowedPrefixes {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		minLen, maxLen := importPolicyLengthBounds(spec, prefix)
		out = append(out, &gobgpapi.Prefix{
			IpPrefix:      prefix.String(),
			MaskLengthMin: uint32(minLen),
			MaskLengthMax: uint32(maxLen),
		})
	}
	return out
}

func peerExportPolicyAssignment(policyName string) *gobgpapi.PolicyAssignment {
	assignment := &gobgpapi.PolicyAssignment{
		Direction:     gobgpapi.PolicyDirection_EXPORT,
		DefaultAction: gobgpapi.RouteAction_REJECT,
	}
	if strings.TrimSpace(policyName) != "" {
		assignment.Policies = []*gobgpapi.Policy{{Name: strings.TrimSpace(policyName)}}
	}
	return assignment
}

func bgpPrefixMaxLength(prefix netip.Prefix) uint32 {
	if prefix.Addr().Is6() {
		return 128
	}
	return 32
}

func bgpPolicyName(routerName, suffix string) string {
	return "routerd-" + sanitizeBGPPolicyName(routerName) + "-" + suffix
}

func peerExportPolicyName(routerName, address string) string {
	return bgpPolicyName(routerName, "export-"+sanitizeBGPPolicyName(address))
}

func peerImportPolicyName(routerName, address string) string {
	return bgpPolicyName(routerName, "import-"+sanitizeBGPPolicyName(address))
}

func dynamicPeerExportPolicyName(routerName, name string) string {
	return bgpPolicyName(routerName, "dynamic-export-"+sanitizeBGPPolicyName(name))
}

func dynamicPeerImportPolicyName(routerName, name string) string {
	return bgpPolicyName(routerName, "dynamic-import-"+sanitizeBGPPolicyName(name))
}

func bgpPolicyStatementName(policyName, suffix string) string {
	return strings.TrimSpace(policyName) + "-" + suffix
}

func cleanCommunityPolicyValues(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sanitizeBGPPolicyName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "default"
	}
	return out
}

func cleanStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func installedNextHops(routes []FIBRoute, result FIBSyncResult) map[string][]string {
	out := map[string][]string{}
	for _, route := range routes {
		prefix := normalizeRoutePrefix(route.Prefix)
		if prefix == "" || !result.Installed[prefix] {
			continue
		}
		out[prefix] = normalizeRouteNextHops(route.NextHops)
	}
	for prefix, hops := range result.RetainedNextHops {
		prefix = normalizeRoutePrefix(prefix)
		if prefix == "" || !result.Installed[prefix] {
			continue
		}
		out[prefix] = normalizeRouteNextHops(hops)
	}
	return out
}

func prefixAllowed(candidate netip.Prefix, allowed []allowedImportPrefix) bool {
	for _, parent := range allowed {
		if parent.Prefix.Addr().Is4() != candidate.Addr().Is4() {
			continue
		}
		if parent.Prefix.Contains(candidate.Addr()) && candidate.Bits() >= parent.Min && candidate.Bits() <= parent.Max {
			return true
		}
	}
	return false
}

type samDynamicClaimAdmission struct {
	byTunnelAddress map[string]samDynamicClaim
	claimedPrefixes map[string]samDynamicClaim
	poolPrefixes    []netip.Prefix
}

type samDynamicClaim struct {
	ClaimRef      string
	LeafID        string
	TunnelAddress string
	BGPASN        uint32
	BGPRouterID   string
	Owned         map[string]bool
}

func (c *Controller) samDynamicClaimAdmission() samDynamicClaimAdmission {
	out := samDynamicClaimAdmission{
		byTunnelAddress: map[string]samDynamicClaim{},
		claimedPrefixes: map[string]samDynamicClaim{},
	}
	if c.Router == nil {
		return out
	}
	policies := map[string]routerapi.SAMEnrollmentPolicySpec{}
	poolPrefixes := map[string]netip.Prefix{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == routerapi.MobilityAPIVersion && resource.Kind == "SAMEnrollmentPolicy" {
			spec, err := resource.SAMEnrollmentPolicySpec()
			if err == nil {
				policies[resource.Metadata.Name] = spec
			}
		}
		if resource.APIVersion == routerapi.MobilityAPIVersion && resource.Kind == "MobilityPool" {
			spec, err := resource.MobilityPoolSpec()
			if err != nil {
				continue
			}
			prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
			if err == nil {
				poolPrefixes[resource.Metadata.Name] = prefix.Masked()
			}
		}
	}
	for _, policy := range policies {
		for _, ref := range policy.MobilityPoolRefs {
			kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
			if ok && kind == "MobilityPool" {
				if prefix, exists := poolPrefixes[name]; exists {
					out.poolPrefixes = append(out.poolPrefixes, prefix)
				}
			}
		}
		for _, prefixText := range policy.MobilityPrefixes {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(prefixText))
			if err == nil {
				out.poolPrefixes = append(out.poolPrefixes, prefix.Masked())
			}
		}
	}
	out.poolPrefixes = uniquePrefixes(out.poolPrefixes)
	now := time.Now().UTC()
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" {
			continue
		}
		claim, err := resource.SAMEnrollmentClaimSpec()
		if err != nil || claim.Revoked {
			continue
		}
		_, policyName, ok := strings.Cut(strings.TrimSpace(claim.PolicyRef), "/")
		if !ok {
			continue
		}
		policy, exists := policies[policyName]
		if !exists || samEnrollmentClaimExpiredForBGP(policy, claim, now) {
			continue
		}
		tunnel, err := parseSAMEnrollmentClaimPrefix(claim.TunnelAddress)
		if err != nil || tunnel.Bits() != int(bgpPrefixMaxLength(tunnel)) {
			continue
		}
		entry := samDynamicClaim{
			ClaimRef:      "SAMEnrollmentClaim/" + strings.TrimSpace(resource.Metadata.Name),
			LeafID:        strings.TrimSpace(claim.LeafID),
			TunnelAddress: tunnel.Addr().String(),
			BGPASN:        claim.BGP.ASN,
			BGPRouterID:   strings.TrimSpace(claim.BGP.RouterID),
			Owned:         map[string]bool{},
		}
		for _, owned := range claim.Mobility.OwnedAddresses {
			prefix, err := parseSAMEnrollmentClaimPrefix(owned)
			if err != nil || prefix.Bits() != int(bgpPrefixMaxLength(prefix)) {
				continue
			}
			key := prefix.String()
			entry.Owned[key] = true
			out.claimedPrefixes[key] = entry
		}
		if len(entry.Owned) > 0 {
			out.byTunnelAddress[entry.TunnelAddress] = entry
		}
	}
	out.addSAMTransportNeighborAliases(c.Router)
	return out
}

func (a samDynamicClaimAdmission) addSAMTransportNeighborAliases(router *routerapi.Router) {
	claimsByLeafID := map[string]samDynamicClaim{}
	for _, claim := range a.byTunnelAddress {
		if strings.TrimSpace(claim.LeafID) != "" {
			claimsByLeafID[strings.TrimSpace(claim.LeafID)] = claim
		}
	}
	if len(claimsByLeafID) == 0 || router == nil {
		return
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != routerapi.HybridAPIVersion || resource.Kind != "TunnelInterface" {
			continue
		}
		peerNode := strings.TrimSpace(resource.Metadata.Annotations["mobility.routerd.net/peer-node"])
		claim, ok := claimsByLeafID[peerNode]
		if !ok {
			continue
		}
		spec, err := resource.TunnelInterfaceSpec()
		if err != nil {
			continue
		}
		neighbor, ok := samTransportNeighborAddress(spec.Address)
		if !ok {
			continue
		}
		a.byTunnelAddress[neighbor.String()] = claim
	}
}

func samTransportNeighborAddress(localPrefix string) (netip.Addr, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(localPrefix))
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 31 {
		return netip.Addr{}, false
	}
	as4 := prefix.Addr().As4()
	value := uint32(as4[0])<<24 | uint32(as4[1])<<16 | uint32(as4[2])<<8 | uint32(as4[3])
	value ^= 1
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}), true
}

func (a samDynamicClaimAdmission) Admit(nextHop string, prefix netip.Prefix) (bool, string) {
	if len(a.byTunnelAddress) == 0 && len(a.claimedPrefixes) == 0 && len(a.poolPrefixes) == 0 {
		return true, ""
	}
	nextHop = normalizeAddressString(nextHop)
	prefix = prefix.Masked()
	if !a.prefixInAdmissionPool(prefix) {
		if a.claimedPrefixes[prefix.String()].ClaimRef != "" {
			return false, "prefix-outside-authorized-pools"
		}
		return true, ""
	}
	if prefix.Bits() != int(bgpPrefixMaxLength(prefix)) {
		return false, "not-exact-host-prefix"
	}
	claim, ok := a.byTunnelAddress[nextHop]
	if !ok {
		return false, "no-accepted-claim-for-next-hop"
	}
	if !claim.Owned[prefix.String()] {
		return false, "prefix-not-owned-by-claim"
	}
	return true, ""
}

func (a samDynamicClaimAdmission) ClaimForNextHop(nextHop string) (samDynamicClaim, bool) {
	claim, ok := a.byTunnelAddress[normalizeAddressString(nextHop)]
	return claim, ok
}

func (a samDynamicClaimAdmission) prefixInAdmissionPool(prefix netip.Prefix) bool {
	for _, pool := range a.poolPrefixes {
		if pool.Addr().Is4() == prefix.Addr().Is4() && pool.Contains(prefix.Addr()) && prefix.Bits() >= pool.Bits() {
			return true
		}
	}
	return false
}

type dynamicRouteAdmissionTracker struct {
	claimAdmission samDynamicClaimAdmission
	accepted       map[string]int
	rejected       map[string]int
	reasons        map[string]int
}

type dynamicRouteAdmissionSummary struct {
	AcceptedByNextHop map[string]int
	RejectedByNextHop map[string]int
	ReasonCounts      map[string]int
}

func newDynamicRouteAdmissionTracker(claimAdmission samDynamicClaimAdmission) *dynamicRouteAdmissionTracker {
	return &dynamicRouteAdmissionTracker{
		claimAdmission: claimAdmission,
		accepted:       map[string]int{},
		rejected:       map[string]int{},
		reasons:        map[string]int{},
	}
}

func (t *dynamicRouteAdmissionTracker) Admit(nextHop string, prefix netip.Prefix) bool {
	key := normalizeAddressString(nextHop)
	ok, reason := t.claimAdmission.Admit(key, prefix)
	if ok {
		t.accepted[key]++
		return true
	}
	t.reject(key, reason)
	return false
}

func (t *dynamicRouteAdmissionTracker) Reject(nextHop string, _ netip.Prefix, reason string) {
	t.reject(normalizeAddressString(nextHop), reason)
}

func (t *dynamicRouteAdmissionTracker) reject(nextHop, reason string) {
	if reason == "" {
		reason = "rejected"
	}
	t.rejected[nextHop]++
	t.reasons[reason]++
}

func (t *dynamicRouteAdmissionTracker) Summary() dynamicRouteAdmissionSummary {
	return dynamicRouteAdmissionSummary{
		AcceptedByNextHop: copyStringIntMap(t.accepted),
		RejectedByNextHop: copyStringIntMap(t.rejected),
		ReasonCounts:      copyStringIntMap(t.reasons),
	}
}

type dynamicPeerObservation struct {
	RemoteAddress    string
	PeerGroup        string
	ASN              uint32
	State            string
	Established      bool
	ReceivedPrefixes int
	AcceptedPrefixes int
	EnrollmentClaim  string
	EnrollmentLeafID string
	EnrollmentBGPRID string
	EnrollmentTunnel string
}

func dynamicPeerObservationFromPeer(peer *gobgpapi.Peer, admission samDynamicClaimAdmission) (dynamicPeerObservation, bool) {
	group := strings.TrimSpace(peer.GetConf().GetPeerGroup())
	if !strings.HasPrefix(group, "routerd-dynamic-") {
		return dynamicPeerObservation{}, false
	}
	state := peer.GetState()
	address := normalizeAddressString(firstNonEmpty(peerAddress(peer), state.GetNeighborAddress()))
	observation := dynamicPeerObservation{
		RemoteAddress: address,
		PeerGroup:     group,
		ASN:           firstNonZero(state.GetPeerAsn(), peer.GetConf().GetPeerAsn()),
		State:         state.GetSessionState().String(),
		Established:   state.GetSessionState() == gobgpapi.PeerState_ESTABLISHED,
	}
	for _, af := range peer.GetAfiSafis() {
		observation.AcceptedPrefixes += int(af.GetState().GetAccepted())
		observation.ReceivedPrefixes += int(af.GetState().GetReceived())
	}
	if claim, ok := admission.ClaimForNextHop(address); ok {
		observation.EnrollmentClaim = claim.ClaimRef
		observation.EnrollmentLeafID = claim.LeafID
		observation.EnrollmentBGPRID = claim.BGPRouterID
		observation.EnrollmentTunnel = claim.TunnelAddress
	}
	return observation, true
}

func dynamicPeerStatusMaps(groupName string, observations []dynamicPeerObservation, summary dynamicRouteAdmissionSummary) []map[string]any {
	var out []map[string]any
	for _, peer := range observations {
		if peer.PeerGroup != groupName {
			continue
		}
		status := map[string]any{
			"remoteAddress":    peer.RemoteAddress,
			"peerGroup":        peer.PeerGroup,
			"asn":              peer.ASN,
			"state":            peer.State,
			"established":      peer.Established,
			"receivedPrefixes": peer.ReceivedPrefixes,
			"acceptedPrefixes": peer.AcceptedPrefixes,
			"acceptedRoutes":   summary.AcceptedByNextHop[peer.RemoteAddress],
			"rejectedRoutes":   summary.RejectedByNextHop[peer.RemoteAddress],
		}
		if peer.EnrollmentClaim != "" {
			status["enrollmentClaimRef"] = peer.EnrollmentClaim
			status["leafID"] = peer.EnrollmentLeafID
			status["bgpRouterID"] = peer.EnrollmentBGPRID
			status["tunnelAddress"] = peer.EnrollmentTunnel
		}
		out = append(out, status)
	}
	return out
}

func dynamicPeerAdmissionTotals(peerStatuses []map[string]any) (int, int) {
	accepted, rejected := 0, 0
	for _, status := range peerStatuses {
		accepted += statusInt(status["acceptedRoutes"])
		rejected += statusInt(status["rejectedRoutes"])
	}
	return accepted, rejected
}

func samEnrollmentClaimExpiredForBGP(policy routerapi.SAMEnrollmentPolicySpec, claim routerapi.SAMEnrollmentClaimSpec, now time.Time) bool {
	if expiresAt, ok := parseBGPEnrollmentTime(claim.ExpiresAt); ok {
		return !expiresAt.After(now)
	}
	ttlText := strings.TrimSpace(policy.TTL)
	if ttlText == "" {
		return false
	}
	ttl, err := time.ParseDuration(ttlText)
	if err != nil {
		return false
	}
	joinedAt, ok := parseBGPEnrollmentTime(claim.JoinTimestamp)
	if !ok {
		return false
	}
	return !joinedAt.Add(ttl).After(now)
}

func parseBGPEnrollmentTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func parseSAMEnrollmentClaimPrefix(value string) (netip.Prefix, error) {
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
	if addr.Is4() {
		return netip.PrefixFrom(addr, 32), nil
	}
	return netip.PrefixFrom(addr, 128), nil
}

func normalizeAddressString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return value
	}
	return addr.String()
}

func uniquePrefixes(values []netip.Prefix) []netip.Prefix {
	seen := map[string]bool{}
	var out []netip.Prefix
	for _, value := range values {
		key := value.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func copyStringIntMap(src map[string]int) map[string]int {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]int, len(src))
	for key, value := range src {
		if key == "" {
			key = "unknown"
		}
		out[key] = value
	}
	return out
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

func fibRoutesSignature(routes []FIBRoute) string {
	normalized := make([]FIBRoute, 0, len(routes))
	for _, route := range routes {
		route = normalizeFIBRouteForSignature(route)
		if route.Prefix != "" {
			normalized = append(normalized, route)
		}
	}
	sort.SliceStable(normalized, func(i, j int) bool { return normalized[i].Prefix < normalized[j].Prefix })
	var b strings.Builder
	for _, route := range normalized {
		b.WriteString(route.Prefix)
		b.WriteByte('=')
		b.WriteString(strings.Join(route.NextHops, ","))
		if route.PreferredSource != "" {
			b.WriteString("|src=")
			b.WriteString(route.PreferredSource)
		}
		if route.RetainOnMissing {
			b.WriteString("|retain")
		}
		b.WriteByte(';')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func normalizeFIBRouteForSignature(route FIBRoute) FIBRoute {
	prefix := normalizeRoutePrefix(route.Prefix)
	nextHops := normalizeRouteNextHops(route.NextHops)
	return FIBRoute{Prefix: prefix, NextHops: nextHops, PreferredSource: strings.TrimSpace(route.PreferredSource), RetainOnMissing: route.RetainOnMissing}
}

func normalizeFIBSyncResult(result FIBSyncResult) FIBSyncResult {
	out := FIBSyncResult{
		Installed:                    map[string]bool{},
		Unsupported:                  map[string]string{},
		Retained:                     map[string]bool{},
		RetainedNextHops:             map[string][]string{},
		PreferredSource:              map[string]string{},
		PreferredSourceSkipped:       map[string]bool{},
		PreferredSourceSkippedReason: map[string]string{},
	}
	for prefix, installed := range result.Installed {
		if normalized := normalizeRoutePrefix(prefix); normalized != "" {
			out.Installed[normalized] = installed
		}
	}
	for prefix, reason := range result.Unsupported {
		if normalized := normalizeRoutePrefix(prefix); normalized != "" {
			out.Unsupported[normalized] = reason
		}
	}
	for prefix, retained := range result.Retained {
		if normalized := normalizeRoutePrefix(prefix); normalized != "" {
			out.Retained[normalized] = retained
		}
	}
	for prefix, hops := range result.RetainedNextHops {
		if normalized := normalizeRoutePrefix(prefix); normalized != "" {
			out.RetainedNextHops[normalized] = normalizeRouteNextHops(hops)
		}
	}
	for prefix, source := range result.PreferredSource {
		if normalized := normalizeRoutePrefix(prefix); normalized != "" && strings.TrimSpace(source) != "" {
			out.PreferredSource[normalized] = strings.TrimSpace(source)
		}
	}
	for prefix, skipped := range result.PreferredSourceSkipped {
		if normalized := normalizeRoutePrefix(prefix); normalized != "" {
			out.PreferredSourceSkipped[normalized] = skipped
		}
	}
	for prefix, reason := range result.PreferredSourceSkippedReason {
		if normalized := normalizeRoutePrefix(prefix); normalized != "" && strings.TrimSpace(reason) != "" {
			out.PreferredSourceSkippedReason[normalized] = strings.TrimSpace(reason)
		}
	}
	return out
}

func (c *Controller) logDebug(msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.Debug(msg, args...)
	}
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
	daemonEvent := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd-bgp", Kind: "bgp", Instance: c.daemonSpec().Name}, "routerd.bgp."+strings.ReplaceAll(event.Type, " ", "."), daemonapi.SeverityInfo)
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

func mobilityLivenessMarkers(prefixes []bgpstate.Prefix) map[string]string {
	out := map[string]string{}
	for _, prefix := range prefixes {
		if !prefix.Valid || prefix.Stale || !bgpstate.HasCommunity(prefix.Communities, bgpstate.MobilityCommunityNodeLiveness) {
			continue
		}
		normalized := normalizeRoutePrefix(prefix.Prefix)
		if normalized == "" {
			continue
		}
		for _, community := range prefix.Communities {
			community = strings.TrimSpace(community)
			if !bgpstate.IsMobilityNodeIdentityCommunity(community) {
				continue
			}
			out[community] = normalized
		}
	}
	return out
}

func mobilityLivenessMarkersFromDestination(dst *gobgpapi.Destination) map[string]string {
	return mobilityLivenessMarkers(statePrefixesIncludingMobilityMarkers(dst))
}

func statePrefixesIncludingMobilityMarkers(dst *gobgpapi.Destination) []bgpstate.Prefix {
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

func mergeStringMap(dst, src map[string]string) {
	for key, value := range src {
		dst[key] = value
	}
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
