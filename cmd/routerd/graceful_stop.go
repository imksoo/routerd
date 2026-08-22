// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	mobilitycontroller "github.com/imksoo/routerd/pkg/controller/mobility"
	provideractioncontroller "github.com/imksoo/routerd/pkg/controller/provideraction"
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type gracefulStopTarget struct {
	PoolName             string
	Source               string
	Prefixes             []string
	SuccessorCommunities map[string]bool
}

type gracefulStopOptions struct {
	Timeout          time.Duration
	PollInterval     time.Duration
	BGPPaths         mobilitycontroller.BGPPathClient
	ProviderAction   provideractioncontroller.Controller
	Logger           *eventlog.Logger
	ControllerLogger *slog.Logger
}

// gracefulStopObservedPathClient is deliberately separate from
// mobilitycontroller.BGPPathClient.  ListPaths is the local applied-path
// journal, whereas graceful handoff must prove that a peer route is present
// in this router's live RIB.
type gracefulStopObservedPathClient interface {
	ListObservedPaths(context.Context) ([]bgpdaemon.ObservedPath, error)
}

func runGracefulStopHandoff(ctx context.Context, router *api.Router, store *routerstate.SQLiteStore, opts gracefulStopOptions) error {
	if router == nil || store == nil || opts.BGPPaths == nil || opts.Timeout <= 0 {
		return nil
	}
	targets, err := gracefulStopTargets(ctx, router, opts.BGPPaths)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		logGracefulStop(opts.Logger, eventlog.LevelInfo, "graceful stop found no mobility /32 paths to hand off", nil)
		return nil
	}
	targetPools := gracefulStopTargetPools(targets)
	// Probe before mutating the local advertisement. During an in-place
	// package upgrade routerd can briefly be newer than the already-running
	// routerd-bgp helper. A missing read-only live-RIB endpoint must make this
	// best-effort handoff a no-op, never leave a partial ForceSelfDrain behind.
	observed, err := gracefulStopLiveRIB(ctx, opts.BGPPaths)
	if err != nil {
		return err
	}
	prepare := mobilitycontroller.Controller{
		Router:                      router,
		Store:                       store,
		BGPPaths:                    opts.BGPPaths,
		StartedAt:                   time.Unix(0, 0).UTC(),
		SuppressProviderDeprovision: true,
		ForceSelfDrainPools:         targetPools,
		ReconcilePools:              targetPools,
	}
	if err := prepare.Reconcile(ctx); err != nil {
		return fmt.Errorf("prepare graceful mobility stop: %w", err)
	}
	logGracefulStop(opts.Logger, eventlog.LevelInfo, "graceful stop notified mobility peers", map[string]string{"targets": fmt.Sprint(gracefulStopTargetCount(targets))})
	waitCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	if err := waitForGracefulStopTakeover(waitCtx, observed, targets, defaultGracefulStopPoll(opts.PollInterval)); err != nil {
		return err
	}
	final := mobilitycontroller.Controller{
		Router:              router,
		Store:               store,
		BGPPaths:            opts.BGPPaths,
		StartedAt:           time.Unix(0, 0).UTC(),
		ForceSelfDrainPools: targetPools,
		ReconcilePools:      targetPools,
	}
	if err := final.Reconcile(ctx); err != nil {
		return fmt.Errorf("finalize graceful mobility stop: %w", err)
	}
	opts.ProviderAction.Router = router
	opts.ProviderAction.Store = store
	opts.ProviderAction.Logger = opts.ControllerLogger
	if err := opts.ProviderAction.Reconcile(ctx); err != nil {
		logGracefulStop(opts.Logger, eventlog.LevelWarning, "graceful stop provider action finalize failed", map[string]string{"error": err.Error()})
	}
	if err := withdrawGracefulStopSelfPaths(ctx, opts.BGPPaths, targets); err != nil {
		return err
	}
	logGracefulStop(opts.Logger, eventlog.LevelInfo, "graceful stop completed mobility handoff", map[string]string{"targets": fmt.Sprint(gracefulStopTargetCount(targets))})
	return nil
}

func gracefulStopLiveRIB(ctx context.Context, bgp mobilitycontroller.BGPPathClient) (gracefulStopObservedPathClient, error) {
	observed, ok := bgp.(gracefulStopObservedPathClient)
	if !ok {
		return nil, fmt.Errorf("graceful mobility stop requires live BGP RIB observation")
	}
	if _, err := observed.ListObservedPaths(ctx); err != nil {
		return nil, fmt.Errorf("probe live BGP RIB before graceful mobility stop: %w", err)
	}
	return observed, nil
}

func gracefulStopTargets(ctx context.Context, router *api.Router, bgp mobilitycontroller.BGPPathClient) ([]gracefulStopTarget, error) {
	var targets []gracefulStopTarget
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil || strings.TrimSpace(spec.Prefix) == "" {
			continue
		}
		poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
		if err != nil {
			continue
		}
		selfNode, err := api.EventGroupSelfNode(router, spec.GroupRef)
		if err != nil {
			continue
		}
		source := mobilitycontroller.DynamicSource(res.Metadata.Name, selfNode)
		paths, err := bgp.ListPaths(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("list graceful stop BGP paths for %s: %w", source, err)
		}
		prefixes := gracefulStopPoolPathPrefixes(paths, poolPrefix.Masked())
		if len(prefixes) == 0 {
			continue
		}
		successors, err := gracefulStopSuccessorCommunities(router, spec, selfNode)
		if err != nil {
			return nil, fmt.Errorf("resolve graceful stop successors for %s: %w", res.Metadata.Name, err)
		}
		if len(successors) == 0 {
			// A target-scoped ForceSelfDrain leaves this singleton/drained pool
			// untouched. Other pools with a real successor may still hand off.
			continue
		}
		targets = append(targets, gracefulStopTarget{
			PoolName:             res.Metadata.Name,
			Source:               source,
			Prefixes:             prefixes,
			SuccessorCommunities: successors,
		})
	}
	return targets, nil
}

func gracefulStopTargetPools(targets []gracefulStopTarget) map[string]bool {
	pools := make(map[string]bool, len(targets))
	for _, target := range targets {
		if name := strings.TrimSpace(target.PoolName); name != "" {
			pools[name] = true
		}
	}
	return pools
}

// gracefulStopSuccessorCommunities limits handoff proof to members that can
// actually replace self in this pool's placement group. The global RIB can
// contain unrelated MobilityPool routes with a syntactically valid identity
// community; those routes are never evidence that this pool has a successor.
func gracefulStopSuccessorCommunities(router *api.Router, spec api.MobilityPoolSpec, selfNode string) (map[string]bool, error) {
	resolved, err := mobilityconfig.ResolveMobilityPoolMembers(router, spec)
	if err != nil {
		return nil, err
	}
	identityNodes := make([]string, 0, len(resolved.Members))
	for _, member := range resolved.Members {
		identityNodes = append(identityNodes, strings.TrimSpace(member.NodeRef))
	}
	if collisions := bgpstate.MobilityNodeIdentityCollisions(identityNodes); len(collisions) > 0 {
		return nil, fmt.Errorf("MobilityPool has ambiguous mobility identity community %s", collisions[0].Community)
	}
	selfNode = strings.TrimSpace(selfNode)
	placementGroup := ""
	for _, member := range resolved.Members {
		if strings.TrimSpace(member.NodeRef) == selfNode {
			placementGroup = strings.TrimSpace(member.Placement.Group)
			break
		}
	}
	if placementGroup == "" {
		return nil, fmt.Errorf("self node %q has no placement group", selfNode)
	}
	successors := map[string]bool{}
	for _, member := range resolved.Members {
		if strings.TrimSpace(member.NodeRef) == selfNode || strings.TrimSpace(member.Placement.Group) != placementGroup {
			continue
		}
		if member.Maintenance.Drain {
			continue
		}
		if community := bgpstate.MobilityNodeIdentityCommunity(member.NodeRef); community != "" {
			successors[community] = true
		}
	}
	return successors, nil
}

func waitForGracefulStopTakeover(ctx context.Context, bgp gracefulStopObservedPathClient, targets []gracefulStopTarget, poll time.Duration) error {
	for {
		complete, err := gracefulStopTakeoverComplete(ctx, bgp, targets)
		if err != nil {
			return err
		}
		if complete {
			return nil
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("graceful mobility stop timed out waiting for peer takeover: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func gracefulStopTakeoverComplete(ctx context.Context, bgp gracefulStopObservedPathClient, targets []gracefulStopTarget) (bool, error) {
	paths, err := bgp.ListObservedPaths(ctx)
	if err != nil {
		return false, fmt.Errorf("list live BGP RIB paths for graceful stop takeover: %w", err)
	}
	peerActive := map[string]map[string]bool{}
	for _, path := range paths {
		prefix := normalizeGracefulStopPrefix(path.Prefix)
		if prefix == "" || !path.Best || !path.Valid || path.Stale || strings.TrimSpace(path.PeerAddress) == "" ||
			!bgpstate.HasCommunity(path.Communities, bgpstate.MobilityCommunityOwner) ||
			!bgpstate.HasCommunity(path.Communities, bgpstate.MobilityCommunityActiveHolder) {
			continue
		}
		for _, community := range path.Communities {
			community = strings.TrimSpace(community)
			if bgpstate.IsMobilityNodeIdentityCommunity(community) {
				if peerActive[prefix] == nil {
					peerActive[prefix] = map[string]bool{}
				}
				peerActive[prefix][community] = true
			}
		}
	}
	for _, target := range targets {
		for _, prefix := range target.Prefixes {
			active := peerActive[normalizeGracefulStopPrefix(prefix)]
			found := false
			for community := range active {
				if target.SuccessorCommunities[community] {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		}
	}
	return true, nil
}

func normalizeGracefulStopPrefix(value string) string {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return prefix.Masked().String()
}

// runGracefulStopHandoffExclusive keeps the ephemeral ForceSelfDrain plan
// from racing the normal controller generation while shutdown is in progress.
func runGracefulStopHandoffExclusive(ctx context.Context, gate *sync.RWMutex, handoff func(context.Context) error) error {
	if handoff == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("graceful mobility stop context ended before mutation fence: %w", err)
	}
	if gate == nil {
		return handoff(ctx)
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !gate.TryLock() {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for controller mutation fence: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	defer gate.Unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("graceful mobility stop context ended after mutation fence: %w", err)
	}
	return handoff(ctx)
}

func withdrawGracefulStopSelfPaths(ctx context.Context, bgp mobilitycontroller.BGPPathClient, targets []gracefulStopTarget) error {
	for _, target := range targets {
		paths, err := bgp.ListPaths(ctx, target.Source)
		if err != nil {
			return fmt.Errorf("list final graceful stop paths for %s: %w", target.Source, err)
		}
		for _, path := range paths {
			if err := bgp.DeletePath(ctx, path); err != nil {
				return fmt.Errorf("withdraw graceful stop BGP path %s/%s: %w", path.Source, path.Prefix, err)
			}
		}
	}
	return nil
}

func gracefulStopPoolPathPrefixes(paths []bgpdaemon.AppliedPath, poolPrefix netip.Prefix) []string {
	seen := map[string]bool{}
	for _, path := range paths {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(path.Prefix))
		if err != nil || prefix.Bits() != 32 || !poolPrefix.Contains(prefix.Addr()) {
			continue
		}
		seen[prefix.Masked().String()] = true
	}
	out := make([]string, 0, len(seen))
	for prefix := range seen {
		out = append(out, prefix)
	}
	sort.Strings(out)
	return out
}

func gracefulStopTargetCount(targets []gracefulStopTarget) int {
	count := 0
	for _, target := range targets {
		count += len(target.Prefixes)
	}
	return count
}

func defaultGracefulStopPoll(poll time.Duration) time.Duration {
	if poll <= 0 {
		return time.Second
	}
	return poll
}

func logGracefulStop(logger *eventlog.Logger, level eventlog.Level, message string, attrs map[string]string) {
	if logger != nil {
		logger.Emit(level, "serve", message, attrs)
	}
}
