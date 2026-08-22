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

	gobgpapi "github.com/osrg/gobgp/v4/api"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/internal/stringutil"
	routerapi "github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/mobilityfib"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/manageddaemon"
	"github.com/imksoo/routerd/pkg/samenrollment"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type dynamicConfigPartLister interface {
	ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
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
	// MutationGate fences watch-driven FIB writes against live-apply
	// transactions. Framework reconciles are gated by their worker.
	MutationGate *sync.RWMutex

	Server    GoBGPServer
	NewServer func() GoBGPServer
	Daemon    manageddaemon.Spec
	FIB       FIBSyncer

	MaxPrefixes         int
	WatchReconnectDelay time.Duration

	mu        sync.Mutex
	started   bool
	startedAt time.Time
	globalKey string
	// policyKey covers both import and export policy objects. importPolicyKey
	// deliberately excludes exports and is only used to determine whether the
	// existing RIB must be soft-reset inbound.
	policyKey                string
	desiredPeerKeys          map[string]desiredPeer
	appliedPeerKeys          map[string]desiredPeer
	appliedConfig            bgpdaemon.AppliedConfig
	importPolicyKey          string
	pendingImportPolicyReset bool
	// retiringDirectPeerAddresses is persisted before removal so routerd-bgp
	// cannot restore an obsolete high-preference direct session after a crash.
	retiringDirectPeerAddresses map[string]bool
	// pendingDirectPeerAdditions marks direct peers journaled before AddPeer.
	// It closes the corresponding crash window before final applied-state save.
	pendingDirectPeerAdditions map[string]bool
	// retiringStaticPaths is the analogous restart fence for locally originated
	// advertisements. The retained UUID is required to finish a live withdrawal.
	retiringStaticPaths  map[string]bgpdaemon.AppliedPath
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
	PassiveMode             bool
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
	// PreserveImportPrefixes prevents router-wide dynamic mobility prefixes
	// from widening a direct SAM peer's explicit admission boundary. Direct
	// peer groups are untrusted accelerators and may only receive the /32s
	// named in their profile's allowlist.
	PreserveImportPrefixes bool
	ExportPolicy           routerapi.BGPExportPolicySpec
	ExportPolicyName       string
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
	// Direct SAM peers are optional higher-preference accelerators. Withdraw an
	// obsolete direct session before removing its narrow import policy. If that
	// deletion fails, retain the old policy and stop rather than leave a live
	// high-preference path without its signed topology source.
	if _, err := c.removeObsoleteDirectPeers(ctx, routerSpec, desired); err != nil {
		return c.savePendingAll("GoBGPPeerRemoveFailed", err)
	}
	// A fallback peer can become a direct peer at the same address. Journal that
	// role upgrade before changing policies: if routerd stops after the narrow,
	// high-preference policy is installed but before UpdatePeer completes, the
	// next reconcile must still identify the live session as direct and withdraw
	// it before removing the policy. RR peers continue forwarding meanwhile.
	if err := c.journalDirectPeerTransitions(ctx, routerSpec, desired); err != nil {
		return c.savePendingAll("GoBGPPeerTransitionFenceFailed", err)
	}
	policyResult, err := c.reconcilePolicies(ctx, routerResource.Metadata.Name, effectiveImportPolicy, desired, desiredDynamic)
	if err != nil {
		return c.savePendingAll("GoBGPPolicyApplyFailed", err)
	}
	if err := c.reconcileDynamicPeers(ctx, desiredDynamic); err != nil {
		return c.savePendingAll("GoBGPDynamicPeerApplyFailed", err)
	}
	exportPolicyRefreshPeers := exportPolicyChangedPeers(c.appliedPeerKeys, desired)
	changed, err := c.reconcilePeers(ctx, routerSpec, desired)
	if err != nil {
		return c.savePendingAll("GoBGPPeerApplyFailed", err)
	}
	// Replacing a GoBGP import policy changes the filter object, but does not
	// necessarily change a peer's stable transport configuration. In
	// particular, direct-mesh peers deliberately keep their import prefix list
	// out of the peer recreation key so a narrowed allowlist cannot flap the
	// session. Ask every current peer for a soft inbound re-evaluation before
	// observing the RIB; otherwise a route admitted by the old (possibly wider)
	// direct policy could retain its higher local preference until some unrelated
	// BGP event happens.
	if policyResult.NeedsInboundReset {
		if err := c.softResetImportPolicy(ctx, desired); err != nil {
			return c.savePendingAll("GoBGPImportPolicyRefreshFailed", err)
		}
		c.pendingImportPolicyReset = false
	}
	if err := c.hardResetBFDDownPeers(ctx, bfdDownTransitions); err != nil {
		return c.savePendingAll("GoBGPBFDResetFailed", err)
	}
	if len(exportPolicyRefreshPeers) > 0 {
		if err := c.softResetExportPolicy(ctx, exportPolicyRefreshPeers); err != nil {
			return c.savePendingAll("GoBGPExportPolicyRefreshFailed", err)
		}
	}
	if err := c.withdrawPendingStaticPaths(ctx); err != nil {
		return c.savePendingAll("GoBGPPathRemoveFailed", err)
	}
	if err := c.reconcileAdvertisements(ctx, routerSpec, c.appliedConfig.Paths); err != nil {
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
	if !policyResult.AdoptedRestored {
		importDrift, err := c.importPolicyDrift(ctx, routerResource.Metadata.Name, effectiveImportPolicy, desired, desiredDynamic)
		if err != nil {
			return c.savePendingAll("GoBGPPolicyObserveFailed", err)
		}
		if importDrift.RefreshNeeded() {
			if err := c.persistPendingImportPolicyReset(ctx); err != nil {
				return c.savePendingAll("GoBGPImportPolicyFencePersistFailed", err)
			}
			if err := c.applyBGPPolicies(ctx, routerResource.Metadata.Name, effectiveImportPolicy, desired, desiredDynamic); err != nil {
				return c.savePendingAll("GoBGPPolicyApplyFailed", err)
			}
			c.policyKey = bgpPoliciesKey(effectiveImportPolicy, desired, desiredDynamic)
			c.importPolicyKey = bgpImportPoliciesKey(effectiveImportPolicy, desired, desiredDynamic)
			c.pendingImportPolicyReset = true
			if err := c.refreshPeerImportPolicyAssignments(ctx, desired, importDrift.PeerAddresses); err != nil {
				return c.savePendingAll("GoBGPPeerApplyFailed", err)
			}
			if err := c.softResetImportPolicy(ctx, desired); err != nil {
				return c.savePendingAll("GoBGPImportPolicyRefreshFailed", err)
			}
			c.pendingImportPolicyReset = false
			// The applied state was deliberately fenced before the live policy
			// repair above. Clear that durable fence now that every peer has been
			// re-evaluated, otherwise the next ordinary reconcile would perform a
			// redundant inbound reset.
			applied.PendingImportPolicyReset = false
			if err := c.Server.SaveAppliedConfig(ctx, applied); err != nil {
				return c.savePendingAll("GoBGPAppliedStatePersistFailed", err)
			}
			c.appliedConfig = applied
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
	c.policyKey = ""
	c.desiredPeerKeys = nil
	c.appliedPeerKeys = nil
	c.appliedConfig = bgpdaemon.AppliedConfig{}
	c.importPolicyKey = ""
	c.pendingImportPolicyReset = false
	c.retiringDirectPeerAddresses = nil
	c.pendingDirectPeerAdditions = nil
	c.retiringStaticPaths = nil
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
				Type: gobgpapi.WatchEventRequest_Table_Filter_TYPE_BEST,
				Init: false,
			}},
		},
		BatchSize: 1,
	}
	return server.WatchEvent(ctx, req, func(resp *gobgpapi.WatchEventResponse) error {
		if !watchEventHasBestPathChange(resp) && !watchEventHasPeerStateChange(resp) {
			return nil
		}
		if c.MutationGate != nil {
			for !c.MutationGate.TryRLock() {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Millisecond):
				}
			}
			defer c.MutationGate.RUnlock()
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
	return pe != nil && pe.GetType() == gobgpapi.WatchEventResponse_PeerEvent_TYPE_STATE
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
	c.pendingImportPolicyReset = applied.PendingImportPolicyReset
	c.retiringDirectPeerAddresses = map[string]bool{}
	c.pendingDirectPeerAdditions = map[string]bool{}
	for _, address := range applied.PendingDirectPeerAdditions {
		if address = strings.TrimSpace(address); address != "" {
			c.pendingDirectPeerAdditions[address] = true
		}
	}
	for _, address := range applied.PendingDirectPeerRemovals {
		if address = strings.TrimSpace(address); address != "" {
			c.retiringDirectPeerAddresses[address] = true
		}
	}
	c.retiringStaticPaths = staticAppliedPaths(applied.PendingStaticPathRemovals)
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
		for _, peer := range spec.Peers {
			peer = strings.TrimSpace(peer)
			out[peer] = desiredPeer{
				Address:                 peer,
				ASN:                     spec.PeerASN,
				LocalASN:                localASN,
				PassiveMode:             spec.PassiveMode,
				Password:                password,
				BFD:                     strings.TrimSpace(spec.BFD),
				EbgpMultihop:            spec.EbgpMultihop,
				RouteReflectorClient:    spec.RouteReflectorClient,
				RouteReflectorClusterID: strings.TrimSpace(spec.RouteReflectorClusterID),
				ImportPolicy:            spec.ImportPolicy,
				PreserveImportPrefixes:  strings.EqualFold(strings.TrimSpace(resource.Metadata.Annotations["mobility.routerd.net/direct-peer"]), "true"),
				ExportPolicy:            spec.ExportPolicy,
				Timers:                  spec.Timers,
			}
		}
	}
	return out, nil
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
		cleanPrefixes := stringutil.UniqueTrimmedSorted(spec.Listen.SourcePrefixes)
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
			if !peer.PreserveImportPrefixes {
				peer.ImportPolicy.AllowedPrefixes = mergeAllowedPrefixes(peer.ImportPolicy.AllowedPrefixes, dynamicExportPrefixes)
			}
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
	if len(stringutil.UniqueTrimmedSorted(spec.AllowedPrefixes)) == 0 {
		return spec
	}
	spec.AllowedPrefixes = mergeAllowedPrefixes(spec.AllowedPrefixes, dynamicPrefixes)
	return spec
}

func peerHasImportPolicy(spec routerapi.BGPImportPolicySpec) bool {
	return len(stringutil.UniqueTrimmedSorted(spec.AllowedPrefixes)) > 0 ||
		len(stringutil.UniqueTrimmedSorted(spec.RequiredCommunities)) > 0 ||
		len(stringutil.UniqueTrimmedSorted(spec.ForbiddenCommunities)) > 0 ||
		strings.TrimSpace(spec.NextHopRewrite) != "" ||
		spec.LocalPreference != 0
}

type policyReconcileResult struct {
	// AdoptedRestored is true only when a freshly constructed controller proved
	// that the policy already installed in GoBGP is semantically identical.
	// There is then no need to rewrite policy objects or reset peers.
	AdoptedRestored bool
	// NeedsInboundReset means the update replaced a policy that may already have
	// admitted routes. Peers must be soft-reset inbound before the RIB is read
	// because GoBGP does not retroactively re-evaluate those routes. The very
	// first policy install happens before peers exist and therefore does not need
	// this reset.
	NeedsInboundReset bool
}

func (c *Controller) reconcilePolicies(ctx context.Context, routerName string, spec routerapi.BGPImportPolicySpec, peers map[string]desiredPeer, dynamicPeers map[string]desiredDynamicPeer) (policyReconcileResult, error) {
	key := bgpPoliciesKey(spec, peers, dynamicPeers)
	importKey := bgpImportPoliciesKey(spec, peers, dynamicPeers)
	if c.policyKey == key {
		return policyReconcileResult{NeedsInboundReset: c.pendingImportPolicyReset}, nil
	}
	if c.policyKey == "" && len(c.appliedConfig.Peers) > 0 {
		drift, err := c.importPolicyDrift(ctx, routerName, spec, peers, dynamicPeers)
		if err != nil {
			return policyReconcileResult{}, err
		}
		if !drift.RefreshNeeded() {
			c.policyKey = key
			c.importPolicyKey = importKey
			return policyReconcileResult{AdoptedRestored: true, NeedsInboundReset: c.pendingImportPolicyReset}, nil
		}
	}
	// Write the recovery fence before altering GoBGP. A crash after the policy
	// takes effect but before ResetPeer would otherwise leave an old direct
	// route (with higher LOCAL_PREF) in the RIB even though the persisted state
	// still describes the wider, previous admission policy.
	needsInboundReset := (c.importPolicyKey != "" && c.importPolicyKey != importKey) ||
		(c.importPolicyKey == "" && len(c.appliedConfig.Peers) > 0)
	if needsInboundReset {
		if err := c.persistPendingImportPolicyReset(ctx); err != nil {
			return policyReconcileResult{}, err
		}
	}
	if err := c.applyBGPPolicies(ctx, routerName, spec, peers, dynamicPeers); err != nil {
		return policyReconcileResult{}, err
	}
	// Persisted applied state is the evidence that this policy replacement can
	// affect an already-populated RIB. Without it this is the initial install:
	// peers are created afterwards and there is nothing stale to reprocess.
	// Export-only edits require an outbound refresh but must not make already
	// accepted routes look stale. A distinct import-only key preserves the
	// direct-mesh safety reset for changed /32 admission while avoiding an
	// unrelated inbound reset for export policy changes.
	c.policyKey = key
	c.importPolicyKey = importKey
	c.pendingImportPolicyReset = needsInboundReset
	return policyReconcileResult{NeedsInboundReset: needsInboundReset}, nil
}

func (c *Controller) persistPendingImportPolicyReset(ctx context.Context) error {
	if c.pendingImportPolicyReset {
		return nil
	}
	pending := c.appliedConfig
	pending.PendingImportPolicyReset = true
	if err := c.Server.SaveAppliedConfig(ctx, pending); err != nil {
		return fmt.Errorf("persist pending import-policy reset fence: %w", err)
	}
	c.appliedConfig = pending
	c.pendingImportPolicyReset = true
	return nil
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
	assignment := globalImportPolicyAssignment(name, peerHasImportPolicy(spec))
	if peerHasImportPolicy(spec) {
		appendImportPolicy(req, name, bgpPolicyName(routerName, "import-prefixes"), spec)
	}
	importPolicies := map[string]bool{name: true}
	for _, peer := range sortedDesiredPeers(peers) {
		importPolicyName := strings.TrimSpace(peer.ImportPolicyName)
		if importPolicyName != "" && !importPolicies[importPolicyName] && peerHasImportPolicy(peer.ImportPolicy) {
			appendImportPolicy(req, importPolicyName, importPolicyName+"-prefixes", peer.ImportPolicy)
			importPolicies[importPolicyName] = true
		}
		prefixes := exportPolicyPrefixes(peer.ExportPolicy)
		if len(prefixes) == 0 || strings.TrimSpace(peer.ExportPolicyName) == "" {
			continue
		}
		prefixSetName := peer.ExportPolicyName + "-prefixes"
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_DEFINED_TYPE_PREFIX,
			Name:        prefixSetName,
			Prefixes:    prefixes,
		})
		req.Policies = append(req.Policies, &gobgpapi.Policy{
			Name: peer.ExportPolicyName,
			Statements: []*gobgpapi.Statement{{
				Name: bgpPolicyStatementName(peer.ExportPolicyName, "allow-export"),
				Conditions: &gobgpapi.Conditions{PrefixSet: &gobgpapi.MatchSet{
					Type: gobgpapi.MatchSet_TYPE_ANY,
					Name: prefixSetName,
				}},
				Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT},
			}},
		})
	}
	for _, peer := range sortedDesiredDynamicPeers(dynamicPeers) {
		importPolicyName := strings.TrimSpace(peer.ImportPolicyName)
		if importPolicyName != "" && !importPolicies[importPolicyName] && peerHasImportPolicy(peer.ImportPolicy) {
			appendImportPolicy(req, importPolicyName, importPolicyName+"-prefixes", peer.ImportPolicy)
			importPolicies[importPolicyName] = true
		}
		prefixes := exportPolicyPrefixes(peer.ExportPolicy)
		if len(prefixes) == 0 || strings.TrimSpace(peer.ExportPolicyName) == "" {
			continue
		}
		prefixSetName := peer.ExportPolicyName + "-prefixes"
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_DEFINED_TYPE_PREFIX,
			Name:        prefixSetName,
			Prefixes:    prefixes,
		})
		req.Policies = append(req.Policies, &gobgpapi.Policy{
			Name: peer.ExportPolicyName,
			Statements: []*gobgpapi.Statement{{
				Name: bgpPolicyStatementName(peer.ExportPolicyName, "allow-export"),
				Conditions: &gobgpapi.Conditions{PrefixSet: &gobgpapi.MatchSet{
					Type: gobgpapi.MatchSet_TYPE_ANY,
					Name: prefixSetName,
				}},
				Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT},
			}},
		})
	}
	return bgpPolicyPlan{SetPolicies: req, GlobalImportAssignment: assignment}
}

func appendImportPolicy(req *gobgpapi.SetPoliciesRequest, policyName, prefixSetName string, spec routerapi.BGPImportPolicySpec) {
	prefixes := importPolicyPrefixes(spec)
	if strings.TrimSpace(policyName) == "" {
		return
	}
	policyName = strings.TrimSpace(policyName)
	prefixSetName = strings.TrimSpace(prefixSetName)
	if len(prefixes) > 0 {
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_DEFINED_TYPE_PREFIX,
			Name:        prefixSetName,
			Prefixes:    prefixes,
		})
	}
	requiredSetName := policyName + "-required-communities"
	requiredCommunities := cleanCommunityPolicyValues(spec.RequiredCommunities)
	if len(requiredCommunities) > 0 {
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_DEFINED_TYPE_COMMUNITY,
			Name:        requiredSetName,
			List:        requiredCommunities,
		})
	}
	forbiddenSetName := policyName + "-forbidden-communities"
	forbiddenCommunities := cleanCommunityPolicyValues(spec.ForbiddenCommunities)
	if len(forbiddenCommunities) > 0 {
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_DEFINED_TYPE_COMMUNITY,
			Name:        forbiddenSetName,
			List:        forbiddenCommunities,
		})
	}
	statements := []*gobgpapi.Statement{}
	if len(forbiddenCommunities) > 0 {
		statements = append(statements, &gobgpapi.Statement{
			Name: bgpPolicyStatementName(policyName, "reject-forbidden-community"),
			Conditions: &gobgpapi.Conditions{CommunitySet: &gobgpapi.MatchSet{
				Type: gobgpapi.MatchSet_TYPE_ANY,
				Name: forbiddenSetName,
			}},
			Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_REJECT},
		})
	}
	acceptConditions := &gobgpapi.Conditions{}
	if len(prefixes) > 0 {
		acceptConditions.PrefixSet = &gobgpapi.MatchSet{
			Type: gobgpapi.MatchSet_TYPE_ANY,
			Name: prefixSetName,
		}
	}
	if len(requiredCommunities) > 0 {
		acceptConditions.CommunitySet = &gobgpapi.MatchSet{
			Type: gobgpapi.MatchSet_TYPE_ALL,
			Name: requiredSetName,
		}
	}
	statements = append(statements, &gobgpapi.Statement{
		Name:       bgpPolicyStatementName(policyName, "allow-import"),
		Conditions: acceptConditions,
		Actions: &gobgpapi.Actions{
			RouteAction: gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT,
			Nexthop:     nextHopRewriteAction(spec),
			LocalPref:   importLocalPreferenceAction(spec),
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
	List     []string
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
	Name             string
	PrefixSetName    string
	PrefixSetType    int32
	CommunitySetName string
	CommunitySetType int32
	RouteAction      int32
	NextHop          string
	LocalPreference  *uint32
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
			if setName := strings.TrimSpace(statement.GetConditions().GetCommunitySet().GetName()); setName != "" {
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
		if statement.GetActions().GetNexthop() != nil || statement.GetActions().GetLocalPref() != nil {
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
	for name, desiredSet := range desired.DefinedSets {
		set, err := c.definedSetByName(ctx, gobgpapi.DefinedType(desiredSet.Type), name)
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
		assignment, err := c.policyAssignment(ctx, desired.GlobalAssignment.Name, gobgpapi.PolicyDirection_POLICY_DIRECTION_IMPORT)
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

func (c *Controller) definedSetByName(ctx context.Context, definedType gobgpapi.DefinedType, name string) (*gobgpapi.DefinedSet, error) {
	var out *gobgpapi.DefinedSet
	err := c.Server.ListDefinedSet(ctx, &gobgpapi.ListDefinedSetRequest{DefinedType: definedType, Name: name}, func(set *gobgpapi.DefinedSet) {
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
	out.List = append(out.List, set.GetList()...)
	sort.Strings(out.List)
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
		communitySet := statement.GetConditions().GetCommunitySet()
		out.Statements = append(out.Statements, canonicalStatement{
			Name:             strings.TrimSpace(statement.GetName()),
			PrefixSetName:    strings.TrimSpace(prefixSet.GetName()),
			PrefixSetType:    int32(prefixSet.GetType()),
			CommunitySetName: strings.TrimSpace(communitySet.GetName()),
			CommunitySetType: int32(communitySet.GetType()),
			RouteAction:      int32(statement.GetActions().GetRouteAction()),
			NextHop:          canonicalNextHopAction(statement.GetActions().GetNexthop()),
			LocalPreference:  canonicalLocalPreferenceAction(statement.GetActions().GetLocalPref()),
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

func canonicalLocalPreferenceAction(action *gobgpapi.LocalPrefAction) *uint32 {
	if action == nil {
		return nil
	}
	value := action.GetValue()
	return &value
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
			Direction: gobgpapi.ResetPeerRequest_DIRECTION_IN,
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
		sameStringSet(stringutil.UniqueTrimmedSorted(a.ExportPolicy.AllowedPrefixes), stringutil.UniqueTrimmedSorted(b.ExportPolicy.AllowedPrefixes))
}

func (c *Controller) softResetExportPolicy(ctx context.Context, addresses []string) error {
	addresses = append([]string(nil), addresses...)
	sort.Strings(addresses)
	for _, address := range addresses {
		if err := c.Server.ResetPeer(ctx, &gobgpapi.ResetPeerRequest{
			Address:   address,
			Soft:      true,
			Direction: gobgpapi.ResetPeerRequest_DIRECTION_OUT,
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
			PassiveMode:             peer.PassiveMode,
			Password:                peer.Password,
			BFD:                     peer.BFD,
			EbgpMultihop:            peer.EbgpMultihop,
			RouteReflectorClient:    peer.RouteReflectorClient,
			RouteReflectorClusterID: peer.RouteReflectorClusterID,
			Timers:                  routerapi.BGPTimersSpec{Profile: peer.TimersProfile},
			GracefulRestart:         gr,
			ConvergenceProfile:      peer.ConvergenceProfile,
			ImportPolicy: routerapi.BGPImportPolicySpec{
				AllowedPrefixes:        peer.ImportPolicy.AllowedPrefixes,
				AllowedPrefixLengthMin: peer.ImportPolicy.AllowedPrefixLengthMin,
				AllowedPrefixLengthMax: peer.ImportPolicy.AllowedPrefixLengthMax,
				RequiredCommunities:    peer.ImportPolicy.RequiredCommunities,
				ForbiddenCommunities:   peer.ImportPolicy.ForbiddenCommunities,
				NextHopRewrite:         peer.ImportPolicy.NextHopRewrite,
				LocalPreference:        peer.ImportPolicy.LocalPreference,
			},
			ImportPolicyName:       peer.ImportPolicyName,
			PreserveImportPrefixes: peer.PreserveImportPrefixes,
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
		Version:                    bgpdaemon.AppliedVersion,
		PendingImportPolicyReset:   c.pendingImportPolicyReset,
		PendingDirectPeerAdditions: mapKeys(c.pendingDirectPeerAdditions),
		PendingDirectPeerRemovals:  mapKeys(c.retiringDirectPeerAddresses),
		PendingStaticPathRemovals:  staticAppliedPathValues(c.retiringStaticPaths),
		Global:                     appliedGlobalFromSpec(spec, c.Router),
		Peers:                      map[string]bgpdaemon.AppliedPeer{},
		Advertisements:             mapKeys(advertisements),
		Paths:                      bgpdaemon.NonStaticPaths(existingPaths),
	}
	for prefix := range advertisements {
		out.Paths = append(out.Paths, staticAppliedPath(prefix, c.pathUUIDs[prefix], staticAdvertisementAttrs(spec)))
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
			AllowedPrefixes:        stringutil.UniqueTrimmedSorted(spec.ImportPolicy.AllowedPrefixes),
			AllowedPrefixLengthMin: spec.ImportPolicy.AllowedPrefixLengthMin,
			AllowedPrefixLengthMax: spec.ImportPolicy.AllowedPrefixLengthMax,
			RequiredCommunities:    stringutil.UniqueTrimmedSorted(spec.ImportPolicy.RequiredCommunities),
			ForbiddenCommunities:   stringutil.UniqueTrimmedSorted(spec.ImportPolicy.ForbiddenCommunities),
			NextHopRewrite:         importNextHopRewrite(spec.ImportPolicy),
			LocalPreference:        spec.ImportPolicy.LocalPreference,
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
		PassiveMode:             peer.PassiveMode,
		Password:                peer.Password,
		BFD:                     peer.BFD,
		EbgpMultihop:            peer.EbgpMultihop,
		RouteReflectorClient:    peer.RouteReflectorClient,
		RouteReflectorClusterID: peer.RouteReflectorClusterID,
		TimersProfile:           strings.TrimSpace(peer.Timers.Profile),
		ConvergenceProfile:      peer.ConvergenceProfile,
		ImportPolicyName:        peer.ImportPolicyName,
		ImportPolicy: bgpdaemon.AppliedImportPolicy{
			AllowedPrefixes:        stringutil.UniqueTrimmedSorted(peer.ImportPolicy.AllowedPrefixes),
			AllowedPrefixLengthMin: peer.ImportPolicy.AllowedPrefixLengthMin,
			AllowedPrefixLengthMax: peer.ImportPolicy.AllowedPrefixLengthMax,
			RequiredCommunities:    stringutil.UniqueTrimmedSorted(peer.ImportPolicy.RequiredCommunities),
			ForbiddenCommunities:   stringutil.UniqueTrimmedSorted(peer.ImportPolicy.ForbiddenCommunities),
			NextHopRewrite:         importNextHopRewrite(peer.ImportPolicy),
			LocalPreference:        peer.ImportPolicy.LocalPreference,
		},
		PreserveImportPrefixes: peer.PreserveImportPrefixes,
		ExportPolicyName:       peer.ExportPolicyName,
		ExportPolicy: bgpdaemon.AppliedExportPolicy{
			AllowedPrefixes: stringutil.UniqueTrimmedSorted(peer.ExportPolicy.AllowedPrefixes),
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
		for _, prefix := range stringutil.UniqueTrimmedSorted(group) {
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
		out[address] = peer.GetState().GetSessionState() == gobgpapi.PeerState_SESSION_STATE_ESTABLISHED
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
			Direction:     gobgpapi.ResetPeerRequest_DIRECTION_BOTH,
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

func (c *Controller) reconcilePeers(ctx context.Context, routerSpec routerapi.BGPRouterSpec, desired map[string]desiredPeer) (bool, error) {
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
			delete(c.pendingDirectPeerAdditions, address)
			continue
		}
		if _, err := c.Server.UpdatePeer(ctx, &gobgpapi.UpdatePeerRequest{Peer: goBGPPeer(peer), DoSoftResetIn: true}); err != nil {
			return changed, err
		}
		c.desiredPeerKeys[address] = peer
		delete(c.pendingDirectPeerAdditions, address)
		changed = true
	}
	for address, peer := range desired {
		if current, ok := live[address]; ok {
			if c.desiredPeerMatches(address, current, peer) {
				c.desiredPeerKeys[address] = peer
				delete(c.pendingDirectPeerAdditions, address)
				continue
			}
		}
		if peer.PreserveImportPrefixes && !c.pendingDirectPeerAdditions[address] {
			if err := c.persistDirectPeerAddition(ctx, routerSpec, peer); err != nil {
				return changed, err
			}
		}
		if err := c.Server.AddPeer(ctx, &gobgpapi.AddPeerRequest{Peer: goBGPPeer(peer)}); err != nil {
			return changed, err
		}
		c.desiredPeerKeys[address] = peer
		delete(c.pendingDirectPeerAdditions, address)
		changed = true
	}
	return changed, nil
}

// journalDirectPeerTransitions records fallback-to-direct role changes before
// their direct import policy can be installed. The journal intentionally also
// updates appliedPeerKeys: if the process keeps running after a later peer
// update failure, a following reconcile must classify that live session as
// direct and withdraw it before relaxing its policy.
func (c *Controller) journalDirectPeerTransitions(ctx context.Context, routerSpec routerapi.BGPRouterSpec, desired map[string]desiredPeer) error {
	addresses := make([]string, 0, len(desired))
	for address, peer := range desired {
		if peer.PreserveImportPrefixes {
			addresses = append(addresses, address)
		}
	}
	sort.Strings(addresses)
	unknownLivePredecessors := map[string]bool{}
	for _, address := range addresses {
		persisted, found := c.appliedConfig.Peers[address]
		knownFallback := found && !persisted.PreserveImportPrefixes
		if !found {
			// A prior fallback AddPeer/UpdatePeer can have succeeded just before
			// its ordinary final applied-state save failed. The live-peer cache
			// is the only evidence in that short window; retain it as a
			// pre-policy promotion fence as well.
			previous, known := c.desiredPeerKeys[address]
			if !known {
				previous, known = c.appliedPeerKeys[address]
			}
			knownFallback = known && !previous.PreserveImportPrefixes
			if !knownFallback {
				unknownLivePredecessors[address] = true
			}
		}
		// A brand-new direct peer has no live, persisted predecessor. Its
		// existing AddPeer-side journal is sufficient and avoids making the
		// initial policy install look like a restored populated RIB. The
		// pre-policy fence is specifically for replacing a known fallback peer
		// at the same address; an unrecorded live peer is checked below.
		if !knownFallback {
			continue
		}
		if err := c.persistDirectPeerAddition(ctx, routerSpec, desired[address]); err != nil {
			return err
		}
	}
	if len(unknownLivePredecessors) == 0 {
		return nil
	}
	// An old routerd process can leave a static fallback peer alive after its
	// state write was lost or removed. Treat a live same-address peer as a
	// fallback predecessor too: writing the direct fence before policy mutation
	// is safer than assuming an unrecorded session is harmless. Dynamic peers
	// are unrelated listener-created sessions and are not candidates here.
	liveStaticPeers := map[string]bool{}
	if err := c.Server.ListPeer(ctx, &gobgpapi.ListPeerRequest{}, func(peer *gobgpapi.Peer) {
		if isRouterdDynamicPeer(peer) {
			return
		}
		if address := peerAddress(peer); address != "" {
			liveStaticPeers[address] = true
		}
	}); err != nil {
		return fmt.Errorf("list peers before direct promotion fence: %w", err)
	}
	for _, address := range mapKeys(unknownLivePredecessors) {
		if !liveStaticPeers[address] {
			continue
		}
		if err := c.persistDirectPeerAddition(ctx, routerSpec, desired[address]); err != nil {
			return err
		}
	}
	return nil
}

// removeObsoleteDirectPeers withdraws an earlier direct SAM session before
// reconcilePolicies can remove its import filter. The normal peer reconciler
// handles ordinary peer deletion after policy installation; that ordering is
// unsafe only for the optional direct path because it has higher LOCAL_PREF.
func (c *Controller) removeObsoleteDirectPeers(ctx context.Context, routerSpec routerapi.BGPRouterSpec, desired map[string]desiredPeer) (bool, error) {
	previousPeers := make(map[string]desiredPeer, len(c.appliedPeerKeys)+len(c.desiredPeerKeys))
	for address, peer := range c.appliedPeerKeys {
		previousPeers[address] = peer
	}
	// desiredPeerKeys records a peer as soon as it has been added to GoBGP. It
	// therefore closes the small window where a live direct peer was created
	// successfully but persisting applied.json failed before the next reconcile.
	for address, peer := range c.desiredPeerKeys {
		previousPeers[address] = peer
	}
	addressSet := map[string]bool{}
	for address, previous := range previousPeers {
		if !previous.PreserveImportPrefixes {
			continue
		}
		if next, stillDesired := desired[address]; stillDesired && next.PreserveImportPrefixes {
			continue
		}
		addressSet[address] = true
	}
	for address := range c.retiringDirectPeerAddresses {
		if next, stillDesired := desired[address]; stillDesired && next.PreserveImportPrefixes {
			// The same peer is desired again (for example after an interrupted
			// profile update), so it is no longer retirement work. It may have
			// been replaced by a fallback peer after the earlier DeletePeer,
			// though, so journal an addition and force UpdatePeer before the
			// final desired-state save clears the durable marker.
			delete(c.retiringDirectPeerAddresses, address)
			if err := c.persistDirectPeerAddition(ctx, routerSpec, next); err != nil {
				return false, err
			}
			continue
		}
		addressSet[address] = true
	}
	if len(addressSet) == 0 {
		return false, nil
	}
	addresses := mapKeys(addressSet)
	if err := c.persistDirectPeerRetirements(ctx, addresses); err != nil {
		return false, err
	}
	live := map[string]bool{}
	if err := c.Server.ListPeer(ctx, &gobgpapi.ListPeerRequest{}, func(peer *gobgpapi.Peer) {
		if address := peerAddress(peer); address != "" {
			live[address] = true
		}
	}); err != nil {
		return false, fmt.Errorf("list peers before direct peer removal: %w", err)
	}
	changed := false
	for _, address := range addresses {
		if !live[address] {
			delete(c.appliedPeerKeys, address)
			delete(c.desiredPeerKeys, address)
			delete(c.retiringDirectPeerAddresses, address)
			delete(c.pendingDirectPeerAdditions, address)
			changed = true
			continue
		}
		if err := c.Server.DeletePeer(ctx, &gobgpapi.DeletePeerRequest{Address: address}); err != nil && !isMissingGoBGPPeer(err) {
			return changed, fmt.Errorf("delete obsolete direct peer %s: %w", address, err)
		}
		delete(c.appliedPeerKeys, address)
		delete(c.desiredPeerKeys, address)
		delete(c.retiringDirectPeerAddresses, address)
		delete(c.pendingDirectPeerAdditions, address)
		changed = true
	}
	return changed, nil
}

// persistDirectPeerRetirements marks obsolete direct peers as withheld from
// daemon restore before touching their live sessions. This is deliberately a
// separate, durable step: the retained peer record keeps its old narrow policy
// available to a live routerd-bgp, while the marker prevents a restart from
// reviving that peer before routerd finishes the withdrawal.
func (c *Controller) persistDirectPeerRetirements(ctx context.Context, addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}
	pending := c.appliedConfig
	if pending.Peers == nil {
		pending.Peers = map[string]bgpdaemon.AppliedPeer{}
	}
	retiring := map[string]bool{}
	for _, address := range pending.PendingDirectPeerRemovals {
		if address = strings.TrimSpace(address); address != "" {
			retiring[address] = true
		}
	}
	for _, address := range addresses {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		existing, found := pending.Peers[address]
		if !found || !existing.PreserveImportPrefixes {
			// A direct peer can have been successfully updated in GoBGP just
			// before the normal final applied-state save. In that crash window
			// pending.Peers still describes the preceding fallback peer. Replace
			// that stale record with the in-memory direct record so the durable
			// retirement marker remains valid and restart-safe.
			peer, found := c.desiredPeerKeys[address]
			if !found || !peer.PreserveImportPrefixes {
				peer, found = c.appliedPeerKeys[address]
			}
			if !found || !peer.PreserveImportPrefixes {
				return fmt.Errorf("cannot persist direct peer retirement for %s without its direct peer record", address)
			}
			pending.Peers[address] = appliedPeer(peer)
		}
		retiring[address] = true
	}
	pending.PendingDirectPeerRemovals = mapKeys(retiring)
	pending = bgpdaemon.Normalize(pending)
	if err := c.Server.SaveAppliedConfig(ctx, pending); err != nil {
		return fmt.Errorf("persist direct peer retirement fence: %w", err)
	}
	c.appliedConfig = pending
	if c.retiringDirectPeerAddresses == nil {
		c.retiringDirectPeerAddresses = map[string]bool{}
	}
	for address := range retiring {
		c.retiringDirectPeerAddresses[address] = true
	}
	return nil
}

// persistDirectPeerAddition journals a direct peer before its policy or live
// peer state can change. The peer definition remains available for policy
// refresh, but routerd-bgp skips the pending peer during restore until routerd
// has observed a complete reconcile and written the final state.
func (c *Controller) persistDirectPeerAddition(ctx context.Context, routerSpec routerapi.BGPRouterSpec, peer desiredPeer) error {
	if !peer.PreserveImportPrefixes || strings.TrimSpace(peer.Address) == "" {
		return nil
	}
	pending := c.appliedConfig
	if pending.Global.ASN == 0 {
		pending.Version = bgpdaemon.AppliedVersion
		pending.Global = appliedGlobalFromSpec(routerSpec, c.Router)
	}
	if pending.Peers == nil {
		pending.Peers = map[string]bgpdaemon.AppliedPeer{}
	}
	pending.Peers[peer.Address] = appliedPeer(peer)
	additions := map[string]bool{}
	for _, address := range pending.PendingDirectPeerAdditions {
		if address = strings.TrimSpace(address); address != "" {
			additions[address] = true
		}
	}
	additions[peer.Address] = true
	pending.PendingDirectPeerAdditions = mapKeys(additions)
	pending = bgpdaemon.Normalize(pending)
	if err := c.Server.SaveAppliedConfig(ctx, pending); err != nil {
		return fmt.Errorf("persist direct peer addition fence: %w", err)
	}
	c.appliedConfig = pending
	if c.appliedPeerKeys == nil {
		c.appliedPeerKeys = map[string]desiredPeer{}
	}
	c.appliedPeerKeys[peer.Address] = peer
	if c.pendingDirectPeerAdditions == nil {
		c.pendingDirectPeerAdditions = map[string]bool{}
	}
	c.pendingDirectPeerAdditions[peer.Address] = true
	return nil
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
	desiredAttrs := staticAdvertisementAttrs(spec)
	existing := staticAppliedPaths(appliedPaths)
	c.pathUUIDs = staticPathUUIDs(appliedPaths)
	retirements := map[string]bgpdaemon.AppliedPath{}
	for prefix, current := range existing {
		if !desired[prefix] || !staticPathAttrsEqual(current.Attrs, desiredAttrs) {
			retirements[prefix] = current
		}
	}
	if err := c.persistStaticPathRetirements(ctx, staticAppliedPathValues(retirements)); err != nil {
		return err
	}
	for prefix := range c.pathUUIDs {
		if !desired[prefix] {
			if len(c.pathUUIDs[prefix]) > 0 {
				if err := c.Server.DeletePath(ctx, &gobgpapi.DeletePathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Uuid: c.pathUUIDs[prefix]}); err != nil && !isMissingGoBGPPath(err) {
					return err
				}
			}
			delete(c.pathUUIDs, prefix)
			delete(c.retiringStaticPaths, prefix)
		}
	}
	for prefix := range desired {
		if current, found := existing[prefix]; found && staticPathAttrsEqual(current.Attrs, desiredAttrs) {
			continue
		}
		if uuid := c.pathUUIDs[prefix]; len(uuid) > 0 {
			if err := c.Server.DeletePath(ctx, &gobgpapi.DeletePathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Uuid: uuid}); err != nil && !isMissingGoBGPPath(err) {
				return err
			}
			delete(c.pathUUIDs, prefix)
			delete(c.retiringStaticPaths, prefix)
		}
		path, err := appliedPathToGoBGPPath(staticAppliedPath(prefix, nil, desiredAttrs))
		if err != nil {
			return err
		}
		resp, err := c.Server.AddPath(ctx, &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: path})
		if err != nil {
			return err
		}
		c.pathUUIDs[prefix] = resp.GetUuid()
	}
	return nil
}

// withdrawPendingStaticPaths finishes a path withdrawal that was fenced in
// applied.json before a prior reconciliation could complete. routerd-bgp does
// not restore these paths, so retrying a missing UUID is safe and convergent.
func (c *Controller) withdrawPendingStaticPaths(ctx context.Context) error {
	for _, path := range staticAppliedPathValues(c.retiringStaticPaths) {
		uuid, err := bgpdaemon.DecodeUUID(path.UUID)
		if err != nil {
			return fmt.Errorf("decode retiring static BGP path %s UUID: %w", path.Prefix, err)
		}
		if len(uuid) == 0 {
			return fmt.Errorf("retiring static BGP path %s has no UUID; restart routerd-bgp before retrying removal", path.Prefix)
		}
		if err := c.Server.DeletePath(ctx, &gobgpapi.DeletePathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Uuid: uuid}); err != nil && !isMissingGoBGPPath(err) {
			return fmt.Errorf("withdraw retiring static BGP path %s: %w", path.Prefix, err)
		}
		delete(c.retiringStaticPaths, path.Prefix)
	}
	return nil
}

// persistStaticPathRetirements removes paths from routerd-bgp's restart state
// before their live DeletePath calls. In contrast to an ordinary final save,
// this write is an intentional precondition: after it succeeds a daemon
// restart cannot re-advertise a path which the current desired config removed.
func (c *Controller) persistStaticPathRetirements(ctx context.Context, paths []bgpdaemon.AppliedPath) error {
	if len(paths) == 0 {
		return nil
	}
	pending := c.appliedConfig
	retiring := staticAppliedPaths(pending.PendingStaticPathRemovals)
	for _, path := range paths {
		path = bgpdaemon.NormalizeAppliedPath(path)
		if path.Source != bgpdaemon.AppliedPathSourceStatic {
			return fmt.Errorf("cannot retire non-static BGP path %s from static advertisement reconciliation", path.Prefix)
		}
		uuid, err := bgpdaemon.DecodeUUID(path.UUID)
		if err != nil {
			return fmt.Errorf("decode static BGP path %s UUID before retirement: %w", path.Prefix, err)
		}
		if len(uuid) == 0 {
			return fmt.Errorf("static BGP path %s has no UUID; restart routerd-bgp before changing this advertisement", path.Prefix)
		}
		retiring[path.Prefix] = path
	}
	keptPaths := make([]bgpdaemon.AppliedPath, 0, len(pending.Paths))
	for _, path := range pending.Paths {
		path = bgpdaemon.NormalizeAppliedPath(path)
		if path.Source == bgpdaemon.AppliedPathSourceStatic && retiring[path.Prefix].Prefix != "" {
			continue
		}
		keptPaths = append(keptPaths, path)
	}
	pending.Paths = keptPaths
	// Paths are the authoritative representation once present. Clear the
	// legacy projection so Normalize does not put a retiring static route back.
	pending.Advertisements = nil
	pending.PendingStaticPathRemovals = staticAppliedPathValues(retiring)
	pending = bgpdaemon.Normalize(pending)
	if err := c.Server.SaveAppliedConfig(ctx, pending); err != nil {
		return fmt.Errorf("persist static path retirement fence: %w", err)
	}
	c.appliedConfig = pending
	c.retiringStaticPaths = retiring
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

// staticAdvertisementAttrs is the concrete meaning of BGPRouter's outbound
// community set for routerd-created local advertisements. In particular, a
// direct SAM leaf signs its owned /32 with its node identity here; import-side
// direct policy can then authenticate the peer rather than merely its tunnel.
func staticAdvertisementAttrs(spec routerapi.BGPRouterSpec) bgpdaemon.AppliedPathAttrs {
	return bgpdaemon.AppliedPathAttrs{Communities: stringutil.UniqueTrimmedSorted(spec.Communities.Set.Out)}
}

func staticAppliedPath(prefix string, uuid []byte, attrs bgpdaemon.AppliedPathAttrs) bgpdaemon.AppliedPath {
	path := bgpdaemon.StaticAppliedPath(prefix, uuid)
	path.Attrs = attrs
	return bgpdaemon.NormalizeAppliedPath(path)
}

func staticPathAttrsEqual(left, right bgpdaemon.AppliedPathAttrs) bool {
	left.Communities = stringutil.UniqueTrimmedSorted(left.Communities)
	right.Communities = stringutil.UniqueTrimmedSorted(right.Communities)
	return reflect.DeepEqual(left, right)
}

func staticAppliedPaths(paths []bgpdaemon.AppliedPath) map[string]bgpdaemon.AppliedPath {
	out := map[string]bgpdaemon.AppliedPath{}
	for _, path := range bgpdaemon.Normalize(bgpdaemon.AppliedConfig{Paths: paths}).Paths {
		if path.Source == bgpdaemon.AppliedPathSourceStatic {
			out[path.Prefix] = path
		}
	}
	return out
}

func staticAppliedPathValues(paths map[string]bgpdaemon.AppliedPath) []bgpdaemon.AppliedPath {
	prefixes := make([]string, 0, len(paths))
	for prefix := range paths {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	out := make([]bgpdaemon.AppliedPath, 0, len(prefixes))
	for _, prefix := range prefixes {
		out = append(out, bgpdaemon.NormalizeAppliedPath(paths[prefix]))
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
	return stringutil.UniqueTrimmedSorted(prefixes)
}

func isMissingGoBGPPath(err error) bool {
	return err != nil && strings.Contains(err.Error(), "can't find a specified path")
}

func isMissingGoBGPPeer(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "can't delete a peer configuration for") ||
		strings.Contains(message, "not found peer")
}

func appliedPathToGoBGPPath(appliedPath bgpdaemon.AppliedPath) (*gobgpapi.Path, error) {
	appliedPath = bgpdaemon.NormalizeAppliedPath(appliedPath)
	parsed, err := netip.ParsePrefix(appliedPath.Prefix)
	if err != nil {
		return nil, err
	}
	parsed = parsed.Masked()
	nlri := ipAddressNLRI(parsed)
	attrs := []*gobgpapi.Attribute{originAttribute()}
	nextHop := "0.0.0.0"
	if parsed.Addr().Is6() {
		nextHop = "::"
	}
	if appliedPath.Attrs.NextHop != "" {
		nextHop = appliedPath.Attrs.NextHop
	}
	attrs = append(attrs, nextHopAttribute(nextHop))
	if appliedPath.Attrs.LocalPref > 0 {
		attrs = append(attrs, localPrefAttribute(appliedPath.Attrs.LocalPref))
	}
	if appliedPath.Attrs.MED > 0 {
		attrs = append(attrs, medAttribute(appliedPath.Attrs.MED))
	}
	communities, err := standardCommunities(appliedPath.Attrs.Communities)
	if err != nil {
		return nil, err
	}
	if len(communities) > 0 {
		attrs = append(attrs, communitiesAttribute(communities))
	}
	return &gobgpapi.Path{Family: familyForPrefix(parsed), Nlri: nlri, Pattrs: attrs}, nil
}

func ipAddressNLRI(prefix netip.Prefix) *gobgpapi.NLRI {
	return &gobgpapi.NLRI{Nlri: &gobgpapi.NLRI_Prefix{Prefix: &gobgpapi.IPAddressPrefix{
		Prefix: prefix.Addr().String(), PrefixLen: uint32(prefix.Bits()),
	}}}
}

func originAttribute() *gobgpapi.Attribute {
	return &gobgpapi.Attribute{Attr: &gobgpapi.Attribute_Origin{Origin: &gobgpapi.OriginAttribute{Origin: 0}}}
}

func nextHopAttribute(nextHop string) *gobgpapi.Attribute {
	return &gobgpapi.Attribute{Attr: &gobgpapi.Attribute_NextHop{NextHop: &gobgpapi.NextHopAttribute{NextHop: nextHop}}}
}

func localPrefAttribute(localPref uint32) *gobgpapi.Attribute {
	return &gobgpapi.Attribute{Attr: &gobgpapi.Attribute_LocalPref{LocalPref: &gobgpapi.LocalPrefAttribute{LocalPref: localPref}}}
}

func medAttribute(med uint32) *gobgpapi.Attribute {
	return &gobgpapi.Attribute{Attr: &gobgpapi.Attribute_MultiExitDisc{MultiExitDisc: &gobgpapi.MultiExitDiscAttribute{Med: med}}}
}

func communitiesAttribute(communities []uint32) *gobgpapi.Attribute {
	return &gobgpapi.Attribute{Attr: &gobgpapi.Attribute_Communities{Communities: &gobgpapi.CommunitiesAttribute{Communities: communities}}}
}

func standardCommunities(values []string) ([]uint32, error) {
	var out []uint32
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch value {
		case "internet":
			out = append(out, 0)
			continue
		case "no-export":
			out = append(out, uint32(0xffff)<<16|0xff01)
			continue
		case "no-advertise":
			out = append(out, uint32(0xffff)<<16|0xff02)
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

func (c *Controller) mobilityFIBVerdicts() []dynamicconfig.FIBVerdict {
	lister, ok := c.Store.(dynamicConfigPartLister)
	if !ok {
		return nil
	}
	records, err := lister.ListDynamicConfigParts()
	if err != nil {
		return nil
	}
	var out []dynamicconfig.FIBVerdict
	invalidPools := map[string]bool{}
	now := time.Now().UTC()
	activeRecords, invalidTypedPools := codec.ActiveMobilityPoolPlanRecords(records, now)
	for pool := range invalidTypedPools {
		invalidPools[pool] = true
	}
	for _, active := range activeRecords {
		record, source := active.Record, active.Source
		if source.ARPObserver || invalidPools[source.PoolRef] {
			continue
		}
		if strings.TrimSpace(record.FIBVerdictsJSON) == "" {
			invalidPools[source.PoolRef] = true
			continue
		}
		verdicts, err := codec.DecodeMobilityFIBVerdicts(record.FIBVerdictsJSON)
		if err != nil || dynamicconfig.ValidateMobilityFIBVerdicts(verdicts, source.PoolRef) != nil {
			invalidPools[source.PoolRef] = true
			continue
		}
		if strings.TrimSpace(record.MobilityDataplaneJSON) != "" {
			plan, err := codec.DecodeMobilityDataplanePlan(record.MobilityDataplaneJSON)
			if err != nil || dynamicconfig.ValidateMobilityDataplanePlanScope(plan, source.PoolRef) != nil || plan.PoolPrefix != mobilityFIBScopePrefix(verdicts) {
				invalidPools[source.PoolRef] = true
				continue
			}
		}
		for _, verdict := range verdicts {
			out = append(out, verdict)
		}
	}
	if len(invalidPools) == 0 {
		return out
	}
	filtered := out[:0]
	for _, verdict := range out {
		if !invalidPools[strings.TrimSpace(verdict.PoolRef)] {
			filtered = append(filtered, verdict)
		}
	}
	return filtered
}

// samTransportTransitScopes derives the RR forwarding authority from the raw
// transport-generated BGPPeer resource. It deliberately does not use the
// effective peer policy: router defaults can widen that policy with unrelated
// dynamic export prefixes. A malformed or manually authored peer contributes
// no authority, leaving mobility-tagged paths fail-closed.
func (c *Controller) samTransportTransitScopes() []mobilityfib.TransitScope {
	if c.Router == nil {
		return nil
	}
	profiles := map[string]routerapi.SAMTransportProfileSpec{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.MobilityAPIVersion || resource.Kind != "SAMTransportProfile" {
			continue
		}
		spec, err := resource.SAMTransportProfileSpec()
		if err == nil {
			profiles[resource.Metadata.Name] = spec
		}
	}
	routers := c.bgpRouters()
	if len(routers) != 1 {
		return nil
	}
	routerName := strings.TrimSpace(routers[0].Metadata.Name)
	var out []mobilityfib.TransitScope
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		profileName, selfNode, peerNode, ok := samTransportPeerProvenance(resource)
		if !ok {
			continue
		}
		profile, ok := profiles[profileName]
		if !ok || strings.TrimSpace(profile.SelfNodeRef) != selfNode {
			continue
		}
		spec, err := resource.BGPPeerSpec()
		if err != nil || !spec.RouteReflectorClient || strings.TrimSpace(profile.BGP.RouterRef) != strings.TrimSpace(spec.RouterRef) {
			continue
		}
		_, targetRouter, routerRefOK := strings.Cut(strings.TrimSpace(spec.RouterRef), "/")
		if !routerRefOK || targetRouter != routerName {
			continue
		}
		expectedIdentity := bgpstate.MobilityNodeIdentityCommunity(peerNode)
		if expectedIdentity == "" || !bgpstate.HasCommunity(spec.ImportPolicy.RequiredCommunities, expectedIdentity) {
			continue
		}
		prefixes, ok := exactIPv4TransitPrefixes(spec.ImportPolicy)
		if !ok {
			continue
		}
		peers := make([]netip.Addr, 0, len(spec.Peers))
		for _, rawPeer := range spec.Peers {
			peer, err := netip.ParseAddr(strings.TrimSpace(rawPeer))
			if err != nil || !peer.Is4() {
				peers = nil
				break
			}
			peers = append(peers, peer.Unmap())
		}
		if len(peers) == 0 {
			continue
		}
		for _, peer := range peers {
			for _, prefix := range prefixes {
				out = append(out, mobilityfib.TransitScope{
					Prefix:               prefix,
					Neighbor:             peer,
					RequiredCommunities:  append([]string(nil), spec.ImportPolicy.RequiredCommunities...),
					ForbiddenCommunities: append([]string(nil), spec.ImportPolicy.ForbiddenCommunities...),
				})
			}
		}
	}
	return out
}

func samTransportPeerProvenance(resource routerapi.Resource) (profileName, selfNode, peerNode string, ok bool) {
	profileName = strings.TrimSpace(resource.Metadata.Annotations["mobility.routerd.net/transport-profile"])
	selfNode = strings.TrimSpace(resource.Metadata.Annotations["mobility.routerd.net/self-node"])
	peerNode = strings.TrimSpace(resource.Metadata.Annotations["mobility.routerd.net/peer-node"])
	if profileName == "" || selfNode == "" || peerNode == "" || selfNode == peerNode {
		return "", "", "", false
	}
	for _, owner := range resource.Metadata.OwnerRefs {
		if owner.APIVersion == routerapi.MobilityAPIVersion && owner.Kind == "SAMTransportProfile" && strings.TrimSpace(owner.Name) == profileName {
			return profileName, selfNode, peerNode, true
		}
	}
	return "", "", "", false
}

func exactIPv4TransitPrefixes(policy routerapi.BGPImportPolicySpec) ([]netip.Prefix, bool) {
	if policy.AllowedPrefixLengthMin != 32 || policy.AllowedPrefixLengthMax != 32 {
		return nil, false
	}
	values := stringutil.UniqueTrimmedSorted(policy.AllowedPrefixes)
	if len(values) == 0 {
		return nil, false
	}
	out := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is4() {
			return nil, false
		}
		out = append(out, prefix.Masked())
	}
	return out, true
}

func mobilityFIBScopePrefix(verdicts []dynamicconfig.FIBVerdict) string {
	for _, verdict := range verdicts {
		if verdict.Scope != nil {
			return verdict.Scope.Prefix
		}
	}
	return ""
}

func (c *Controller) observeState(ctx context.Context, allowedImportPrefixes []allowedImportPrefix, desired map[string]desiredPeer) (bgpstate.State, []FIBRoute, map[string]string, error) {
	var state bgpstate.State
	var routes []FIBRoute
	livenessMarkers := map[string]string{}
	fibNextHopRewritePeers := peerAddressFIBRewritePeers(desired)
	mobilityVerdicts := c.mobilityFIBVerdicts()
	claimAdmission := c.samDynamicClaimAdmission()
	if !routerHasBGPDynamicPeer(c.Router) {
		claimAdmission = samDynamicClaimAdmission{}
	}
	transitScopes := c.samTransportTransitScopes()
	fibPolicy := mobilityfib.NewSnapshotFromVerdictsAndTransit(mobilityVerdicts, transitScopes)
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
		err := c.Server.ListPath(ctx, &gobgpapi.ListPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Family: family}, func(dst *gobgpapi.Destination) {
			state.Prefixes = append(state.Prefixes, statePrefixes(dst)...)
			mergeStringMap(livenessMarkers, mobilityLivenessMarkersFromDestination(dst))
			routes = append(routes, fibRoutesFromDestination(dst, allowedImportPrefixes, fibNextHopRewritePeers, func(prefix netip.Prefix, identityAddress, _ string, communities []string) bool {
				neighbor, _ := netip.ParseAddr(strings.TrimSpace(identityAddress))
				if !fibPolicy.AdmitBGPPathFrom(prefix, neighbor, communities) {
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
	// SAM transport inner prefixes are point-to-point tunnel addressing space.
	// They are already owned by the tunnel interfaces, so a reflected aggregate
	// or /31 must never be installed as a BGP kernel route.  Apart from being
	// redundant, an aggregate can make its own next hops unreachable while the
	// transport topology is reconciling.
	routes = c.excludeSAMTransportInnerFIBRoutes(routes)
	routes = applyMobilityPreferredSources(routes, fibPolicy.PreferredSources())
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

func (c *Controller) excludeSAMTransportInnerFIBRoutes(routes []FIBRoute) []FIBRoute {
	if len(routes) == 0 || c.Router == nil {
		return routes
	}
	inner := map[netip.Prefix]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != routerapi.MobilityAPIVersion || resource.Kind != "SAMTransportProfile" {
			continue
		}
		spec, err := resource.SAMTransportProfileSpec()
		if err != nil {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.InnerPrefix))
		if err == nil {
			inner[prefix.Masked()] = true
		}
	}
	if len(inner) == 0 {
		return routes
	}
	out := make([]FIBRoute, 0, len(routes))
	for _, route := range routes {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(route.Prefix))
		if err != nil {
			out = append(out, route)
			continue
		}
		transportRoute := false
		for transportPrefix := range inner {
			if prefix.Masked().Overlaps(transportPrefix) {
				transportRoute = true
				break
			}
		}
		if !transportRoute {
			out = append(out, route)
		}
	}
	return out
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
			sourcePrefixes := stringutil.UniqueTrimmedSorted(spec.Listen.SourcePrefixes)
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
	peerType := gobgpapi.PeerType_PEER_TYPE_EXTERNAL
	if peer.LocalASN != 0 && peer.ASN == peer.LocalASN {
		peerType = gobgpapi.PeerType_PEER_TYPE_INTERNAL
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
		Transport: goBGPPeerTransport(peer.PassiveMode),
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
	if peerHasImportPolicy(peer.ImportPolicy) && strings.TrimSpace(peer.ImportPolicyName) != "" {
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

func goBGPPeerTransport(passiveMode bool) *gobgpapi.Transport {
	if !passiveMode {
		return nil
	}
	return &gobgpapi.Transport{PassiveMode: true}
}

func goBGPDynamicPeerGroup(peer desiredDynamicPeer) *gobgpapi.PeerGroup {
	peerType := gobgpapi.PeerType_PEER_TYPE_EXTERNAL
	if peer.LocalASN != 0 && peer.ASN == peer.LocalASN {
		peerType = gobgpapi.PeerType_PEER_TYPE_INTERNAL
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
	if peerHasImportPolicy(peer.ImportPolicy) && strings.TrimSpace(peer.ImportPolicyName) != "" {
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
	// A pre-policy direct-peer journal can deliberately describe a direct peer
	// while the live daemon still has the preceding fallback peer. Do not let
	// the cache suppress the required UpdatePeer; the marker is cleared only
	// after that live update (or AddPeer) succeeds.
	if c.pendingDirectPeerAdditions[address] {
		return false
	}
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
		Address:          stringutil.FirstNonBlank(peerAddress(peer), state.GetNeighborAddress()),
		ASN:              firstNonZero(state.GetPeerAsn(), peer.GetConf().GetPeerAsn()),
		State:            session,
		Established:      state.GetSessionState() == gobgpapi.PeerState_SESSION_STATE_ESTABLISHED,
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
		prefix := stringutil.FirstNonBlank(dst.GetPrefix(), pathPrefix(path))
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
	type candidate struct {
		nextHop string
		rank    bgpPathRank
		best    bool
		stale   bool
	}
	var candidates []candidate
	localBest := false
	for _, path := range dst.GetPaths() {
		if path.GetIsWithdraw() || path.GetIsNexthopInvalid() {
			continue
		}
		if bgpstate.HasCommunity(pathCommunities(path), bgpstate.MobilityCommunityNodeLiveness) {
			continue
		}
		pathPrefix := stringutil.FirstNonBlank(prefix, normalizeRoutePrefix(pathPrefix(path)))
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
			// A GoBGP-local best path has no remote gateway. Do not synthesize
			// a kernel route through a lower-ranked remote alternate: traffic for
			// this prefix is local to the router that selected it.
			if path.GetBest() {
				localBest = true
			}
			continue
		}
		identityAddress := stringutil.FirstNonBlank(normalizedPathNeighbor(path), nextHop)
		communities := pathCommunities(path)
		if admit != nil && !admit(parsed, identityAddress, nextHop, communities) {
			continue
		}
		candidates = append(candidates, candidate{
			nextHop: nextHop,
			rank:    pathRank(path),
			best:    path.GetBest(),
			stale:   path.GetStale(),
		})
		prefix = parsed.String()
	}
	if len(candidates) == 0 || prefix == "" {
		return nil
	}
	if localBest {
		return nil
	}
	// A stale direct-mesh path can remain GoBGP's best path during graceful
	// restart even after a lower-preference reflected path is live. Prefer the
	// live set for FIB selection, while retaining stale-path forwarding when it
	// is the only available set during the graceful-restart window.
	liveCandidates := make([]candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.stale {
			liveCandidates = append(liveCandidates, candidate)
		}
	}
	if len(liveCandidates) > 0 {
		candidates = liveCandidates
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
		if importNextHopRewrite(peer.ImportPolicy) != "peer-address" {
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

// applyMobilityPreferredSources projects the preferred-source field already
// decided in the typed mobility FIB scope. It must not reopen or normalize
// MobilityPool configuration.
func applyMobilityPreferredSources(routes []FIBRoute, sources []mobilityfib.PreferredSource) []FIBRoute {
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
			if source.Prefix.Contains(routePrefix.Addr()) && route.Prefix != source.AddressPrefix {
				route.PreferredSource = source.Address
				break
			}
		}
		out = append(out, route)
	}
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
		switch {
		case attr.GetLocalPref() != nil:
			rank.LocalPref = attr.GetLocalPref().GetLocalPref()
		case attr.GetAsPath() != nil:
			rank.ASPathLen += asPathLength(attr.GetAsPath().GetSegments())
		case attr.GetAs4Path() != nil:
			rank.ASPathLen += asPathLength(attr.GetAs4Path().GetSegments())
		case attr.GetOrigin() != nil:
			rank.Origin = uint8(attr.GetOrigin().GetOrigin())
		case attr.GetMultiExitDisc() != nil:
			rank.MED = attr.GetMultiExitDisc().GetMed()
		}
	}
	return rank
}

func asPathLength(segments []*gobgpapi.AsSegment) int {
	length := 0
	for _, segment := range segments {
		if segment.GetType() == gobgpapi.AsSegment_TYPE_AS_SET && len(segment.GetNumbers()) > 0 {
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
	for _, value := range stringutil.UniqueTrimmedSorted(spec.AllowedPrefixes) {
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
	for _, value := range stringutil.UniqueTrimmedSorted(spec.AllowedPrefixes) {
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
		AllowedPrefixes:        stringutil.UniqueTrimmedSorted(spec.AllowedPrefixes),
		AllowedPrefixLengthMin: spec.AllowedPrefixLengthMin,
		AllowedPrefixLengthMax: spec.AllowedPrefixLengthMax,
		RequiredCommunities:    stringutil.UniqueTrimmedSorted(spec.RequiredCommunities),
		ForbiddenCommunities:   stringutil.UniqueTrimmedSorted(spec.ForbiddenCommunities),
		NextHopRewrite:         importNextHopRewrite(spec),
		LocalPreference:        spec.LocalPreference,
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func bgpPoliciesKey(importSpec routerapi.BGPImportPolicySpec, peers map[string]desiredPeer, dynamicPeers map[string]desiredDynamicPeer) string {
	return bgpPoliciesKeyWithExports(importSpec, peers, dynamicPeers, true)
}

// bgpImportPoliciesKey deliberately excludes exports. GoBGP must re-evaluate
// learned routes when an import policy changes, but an export-only policy edit
// has no bearing on the current RIB.
func bgpImportPoliciesKey(importSpec routerapi.BGPImportPolicySpec, peers map[string]desiredPeer, dynamicPeers map[string]desiredDynamicPeer) string {
	return bgpPoliciesKeyWithExports(importSpec, peers, dynamicPeers, false)
}

func bgpPoliciesKeyWithExports(importSpec routerapi.BGPImportPolicySpec, peers map[string]desiredPeer, dynamicPeers map[string]desiredDynamicPeer, includeExports bool) string {
	type peerPolicyKey struct {
		Address                    string   `json:"address"`
		ImportPolicyName           string   `json:"importPolicyName,omitempty"`
		ImportAllowedPrefixes      []string `json:"importAllowedPrefixes,omitempty"`
		ImportLengthMin            int      `json:"importLengthMin,omitempty"`
		ImportLengthMax            int      `json:"importLengthMax,omitempty"`
		ImportNextHopRewrite       string   `json:"importNextHopRewrite,omitempty"`
		ImportLocalPreference      uint32   `json:"importLocalPreference,omitempty"`
		ImportRequiredCommunities  []string `json:"importRequiredCommunities,omitempty"`
		ImportForbiddenCommunities []string `json:"importForbiddenCommunities,omitempty"`
		ExportPolicyName           string   `json:"exportPolicyName,omitempty"`
		ExportAllowedPrefixes      []string `json:"exportAllowedPrefixes,omitempty"`
	}
	type dynamicPeerPolicyKey struct {
		PeerGroupName              string   `json:"peerGroupName"`
		Prefixes                   []string `json:"prefixes,omitempty"`
		ImportPolicyName           string   `json:"importPolicyName,omitempty"`
		ImportAllowedPrefixes      []string `json:"importAllowedPrefixes,omitempty"`
		ImportLengthMin            int      `json:"importLengthMin,omitempty"`
		ImportLengthMax            int      `json:"importLengthMax,omitempty"`
		ImportNextHopRewrite       string   `json:"importNextHopRewrite,omitempty"`
		ImportLocalPreference      uint32   `json:"importLocalPreference,omitempty"`
		ImportRequiredCommunities  []string `json:"importRequiredCommunities,omitempty"`
		ImportForbiddenCommunities []string `json:"importForbiddenCommunities,omitempty"`
		ExportPolicyName           string   `json:"exportPolicyName,omitempty"`
		ExportAllowedPrefixes      []string `json:"exportAllowedPrefixes,omitempty"`
	}
	normalized := struct {
		Import       routerapi.BGPImportPolicySpec `json:"import"`
		Peers        []peerPolicyKey               `json:"peers,omitempty"`
		DynamicPeers []dynamicPeerPolicyKey        `json:"dynamicPeers,omitempty"`
	}{
		Import: routerapi.BGPImportPolicySpec{
			AllowedPrefixes:        stringutil.UniqueTrimmedSorted(importSpec.AllowedPrefixes),
			AllowedPrefixLengthMin: importSpec.AllowedPrefixLengthMin,
			AllowedPrefixLengthMax: importSpec.AllowedPrefixLengthMax,
			RequiredCommunities:    stringutil.UniqueTrimmedSorted(importSpec.RequiredCommunities),
			ForbiddenCommunities:   stringutil.UniqueTrimmedSorted(importSpec.ForbiddenCommunities),
			NextHopRewrite:         importNextHopRewrite(importSpec),
			LocalPreference:        importSpec.LocalPreference,
		},
	}
	for _, peer := range sortedDesiredPeers(peers) {
		importPrefixes := stringutil.UniqueTrimmedSorted(peer.ImportPolicy.AllowedPrefixes)
		requiredCommunities := stringutil.UniqueTrimmedSorted(peer.ImportPolicy.RequiredCommunities)
		forbiddenCommunities := stringutil.UniqueTrimmedSorted(peer.ImportPolicy.ForbiddenCommunities)
		exportPrefixes := []string(nil)
		exportPolicyName := ""
		if includeExports {
			exportPrefixes = stringutil.UniqueTrimmedSorted(peer.ExportPolicy.AllowedPrefixes)
			exportPolicyName = strings.TrimSpace(peer.ExportPolicyName)
		}
		if len(importPrefixes) == 0 && len(requiredCommunities) == 0 && len(forbiddenCommunities) == 0 && len(exportPrefixes) == 0 {
			continue
		}
		normalized.Peers = append(normalized.Peers, peerPolicyKey{
			Address:                    strings.TrimSpace(peer.Address),
			ImportPolicyName:           strings.TrimSpace(peer.ImportPolicyName),
			ImportAllowedPrefixes:      importPrefixes,
			ImportLengthMin:            peer.ImportPolicy.AllowedPrefixLengthMin,
			ImportLengthMax:            peer.ImportPolicy.AllowedPrefixLengthMax,
			ImportNextHopRewrite:       importNextHopRewrite(peer.ImportPolicy),
			ImportLocalPreference:      peer.ImportPolicy.LocalPreference,
			ImportRequiredCommunities:  requiredCommunities,
			ImportForbiddenCommunities: forbiddenCommunities,
			ExportPolicyName:           exportPolicyName,
			ExportAllowedPrefixes:      exportPrefixes,
		})
	}
	for _, peer := range sortedDesiredDynamicPeers(dynamicPeers) {
		importPrefixes := stringutil.UniqueTrimmedSorted(peer.ImportPolicy.AllowedPrefixes)
		requiredCommunities := stringutil.UniqueTrimmedSorted(peer.ImportPolicy.RequiredCommunities)
		forbiddenCommunities := stringutil.UniqueTrimmedSorted(peer.ImportPolicy.ForbiddenCommunities)
		exportPrefixes := []string(nil)
		exportPolicyName := ""
		if includeExports {
			exportPrefixes = stringutil.UniqueTrimmedSorted(peer.ExportPolicy.AllowedPrefixes)
			exportPolicyName = strings.TrimSpace(peer.ExportPolicyName)
		}
		normalized.DynamicPeers = append(normalized.DynamicPeers, dynamicPeerPolicyKey{
			PeerGroupName:              strings.TrimSpace(peer.PeerGroupName),
			Prefixes:                   append([]string(nil), peer.Prefixes...),
			ImportPolicyName:           strings.TrimSpace(peer.ImportPolicyName),
			ImportAllowedPrefixes:      importPrefixes,
			ImportLengthMin:            peer.ImportPolicy.AllowedPrefixLengthMin,
			ImportLengthMax:            peer.ImportPolicy.AllowedPrefixLengthMax,
			ImportNextHopRewrite:       importNextHopRewrite(peer.ImportPolicy),
			ImportLocalPreference:      peer.ImportPolicy.LocalPreference,
			ImportRequiredCommunities:  requiredCommunities,
			ImportForbiddenCommunities: forbiddenCommunities,
			ExportPolicyName:           exportPolicyName,
			ExportAllowedPrefixes:      exportPrefixes,
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

func importLocalPreferenceAction(spec routerapi.BGPImportPolicySpec) *gobgpapi.LocalPrefAction {
	if spec.LocalPreference == 0 {
		return nil
	}
	return &gobgpapi.LocalPrefAction{Value: spec.LocalPreference}
}

func globalImportPolicyAssignment(policyName string, includePolicy bool) *gobgpapi.PolicyAssignment {
	return &gobgpapi.PolicyAssignment{
		Name:          "global",
		Direction:     gobgpapi.PolicyDirection_POLICY_DIRECTION_IMPORT,
		DefaultAction: gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT,
	}
}

func peerImportPolicyAssignment(policyName string) *gobgpapi.PolicyAssignment {
	assignment := &gobgpapi.PolicyAssignment{
		Direction:     gobgpapi.PolicyDirection_POLICY_DIRECTION_IMPORT,
		DefaultAction: gobgpapi.RouteAction_ROUTE_ACTION_REJECT,
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
		Direction:     gobgpapi.PolicyDirection_POLICY_DIRECTION_EXPORT,
		DefaultAction: gobgpapi.RouteAction_ROUTE_ACTION_REJECT,
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
	return stringutil.ConservativeName(value, "default")
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
		if !exists || samenrollment.ClaimExpired(policy, claim, now) {
			continue
		}
		tunnel, err := samenrollment.ParsePrefixOrAddress(claim.TunnelAddress)
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
			prefix, err := samenrollment.ParsePrefixOrAddress(owned)
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
	address := normalizeAddressString(stringutil.FirstNonBlank(peerAddress(peer), state.GetNeighborAddress()))
	observation := dynamicPeerObservation{
		RemoteAddress: address,
		PeerGroup:     group,
		ASN:           firstNonZero(state.GetPeerAsn(), peer.GetConf().GetPeerAsn()),
		State:         state.GetSessionState().String(),
		Established:   state.GetSessionState() == gobgpapi.PeerState_SESSION_STATE_ESTABLISHED,
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
	nlri := path.GetNlri().GetPrefix()
	if nlri == nil {
		return ""
	}
	addr, err := netip.ParseAddr(nlri.GetPrefix())
	if err != nil {
		return ""
	}
	return netip.PrefixFrom(addr, int(nlri.GetPrefixLen())).Masked().String()
}

func pathCommunities(path *gobgpapi.Path) []string {
	var out []string
	for _, attr := range path.GetPattrs() {
		if communities := attr.GetCommunities(); communities != nil {
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
		switch {
		case attr.GetNextHop() != nil:
			return strings.TrimSpace(attr.GetNextHop().GetNextHop())
		case attr.GetMpReach() != nil:
			for _, hop := range attr.GetMpReach().GetNextHops() {
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
	nextHop := "0.0.0.0"
	if parsed.Addr().Is6() {
		nextHop = "::"
	}
	return &gobgpapi.Path{
		Family: familyForPrefix(parsed),
		Nlri:   ipAddressNLRI(parsed),
		Pattrs: []*gobgpapi.Attribute{originAttribute(), nextHopAttribute(nextHop)},
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
			reason := stringutil.FirstNonBlank(peer.LastErrorReason, peer.State, "NotEstablished")
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
				Address:           statusvalue.Text(item["address"]),
				ASN:               uint32(statusInt(item["asn"])),
				State:             statusvalue.Text(item["state"]),
				Established:       statusvalue.BoolOrFalse(item["established"]),
				PrefixesReceived:  statusInt(item["prefixesReceived"]),
				LastEstablishedAt: statusvalue.Text(item["lastEstablishedAt"]),
				LastErrorAt:       statusvalue.Text(item["lastErrorAt"]),
				LastErrorReason:   statusvalue.Text(item["lastErrorReason"]),
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
		prefix := stringutil.FirstNonBlank(dst.GetPrefix(), pathPrefix(path))
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

func (c *Controller) Close() {
	if c.Server != nil {
		c.Server.Stop()
	}
}
