// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	mobilitycontroller "github.com/imksoo/routerd/pkg/controller/mobility"
	provideractioncontroller "github.com/imksoo/routerd/pkg/controller/provideraction"
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type gracefulStopTarget struct {
	PoolName string
	SelfNode string
	Source   string
	Prefixes []string
}

type gracefulStopOptions struct {
	Timeout          time.Duration
	PollInterval     time.Duration
	BGPPaths         mobilitycontroller.BGPPathClient
	ProviderAction   provideractioncontroller.Controller
	Logger           *eventlog.Logger
	ControllerLogger *slog.Logger
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
	if !hasGracefulStopDrainablePool(router) {
		return nil
	}
	prepare := mobilitycontroller.Controller{
		Router:                      router,
		Store:                       store,
		BGPPaths:                    opts.BGPPaths,
		StartedAt:                   time.Unix(0, 0).UTC(),
		SuppressProviderDeprovision: true,
		ForceSelfDrain:              true,
	}
	if err := prepare.Reconcile(ctx); err != nil {
		return fmt.Errorf("prepare graceful mobility stop: %w", err)
	}
	logGracefulStop(opts.Logger, eventlog.LevelInfo, "graceful stop notified mobility peers", map[string]string{"targets": fmt.Sprint(gracefulStopTargetCount(targets))})
	waitCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	if err := waitForGracefulStopTakeover(waitCtx, opts.BGPPaths, targets, defaultGracefulStopPoll(opts.PollInterval)); err != nil {
		return err
	}
	final := mobilitycontroller.Controller{
		Router:         router,
		Store:          store,
		BGPPaths:       opts.BGPPaths,
		StartedAt:      time.Unix(0, 0).UTC(),
		ForceSelfDrain: true,
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
		targets = append(targets, gracefulStopTarget{PoolName: res.Metadata.Name, SelfNode: selfNode, Source: source, Prefixes: prefixes})
	}
	return targets, nil
}

func hasGracefulStopDrainablePool(router *api.Router) bool {
	if router == nil {
		return false
	}
	for i := range router.Spec.Resources {
		res := &router.Spec.Resources[i]
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil {
			continue
		}
		selfNode, err := api.EventGroupSelfNode(router, spec.GroupRef)
		if err != nil {
			continue
		}
		resolved, err := mobilityconfig.ResolveMobilityPoolMembers(router, spec)
		if err != nil {
			continue
		}
		for _, member := range resolved.Members {
			if strings.TrimSpace(member.NodeRef) != strings.TrimSpace(selfNode) {
				continue
			}
			if member.Placement.Group == "" {
				continue
			}
			return true
		}
	}
	return false
}

func waitForGracefulStopTakeover(ctx context.Context, bgp mobilitycontroller.BGPPathClient, targets []gracefulStopTarget, poll time.Duration) error {
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

func gracefulStopTakeoverComplete(ctx context.Context, bgp mobilitycontroller.BGPPathClient, targets []gracefulStopTarget) (bool, error) {
	paths, err := bgp.ListPaths(ctx, "")
	if err != nil {
		return false, fmt.Errorf("list BGP paths for graceful stop takeover: %w", err)
	}
	peerActive := map[string]bool{}
	for _, path := range paths {
		path = bgpdaemon.NormalizeAppliedPath(path)
		if path.Source == "" || path.Prefix == "" || path.Attrs.LocalPref <= 200 {
			continue
		}
		peerActive[path.Source+"\x00"+path.Prefix] = true
	}
	for _, target := range targets {
		for _, prefix := range target.Prefixes {
			found := false
			for key := range peerActive {
				if strings.HasSuffix(key, "\x00"+prefix) && !strings.HasPrefix(key, target.Source+"\x00") {
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
