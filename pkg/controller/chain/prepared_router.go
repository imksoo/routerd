// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

// preparedRouterConfig is the immutable static preparation owned by the same
// configuration publication as declared. It has no observation-dependent state.
type preparedRouterConfig struct {
	declared  *api.Router
	startup   *dynamicconfig.PreparedStartup
	staticSAM *api.Router
	timedSAM  bool
	targetOS  platform.OS
}

type routerObservation struct {
	DeclaredRouter  *api.Router
	EffectiveRouter *api.Router
}

// RuntimeRouterObservation captures the declared configuration and the current
// resource selections from one immutable preparation and one dynamic read.
// Callers must treat every returned router as read-only.
type RuntimeRouterObservation struct {
	DeclaredRouter  *api.Router
	EffectiveRouter *api.Router
	RouteRouter     *api.Router
	NextChangeAt    time.Time
}

type RuntimeObservationStore interface {
	resourcequery.StateStore
	resourcequery.StatusReader
}

// ObserveRouterConfiguration admits one standalone plan without requiring a
// writable controller store. Runtime observations reuse Runner's preparation.
func ObserveRouterConfiguration(router *api.Router, store RuntimeObservationStore, now time.Time, targetOS platform.OS) (RuntimeRouterObservation, error) {
	prepared, err := prepareRouterConfig(router, targetOS)
	if err != nil {
		return RuntimeRouterObservation{}, err
	}
	lister, _ := store.(dynamicConfigPartLister)
	view, err := prepared.observeRuntimeRouter(store, lister, now, targetOS)
	if err != nil {
		return RuntimeRouterObservation{}, err
	}
	return runtimeRouterObservation(view), nil
}

func runtimeRouterObservation(view dynamicRouteSAMView) RuntimeRouterObservation {
	return RuntimeRouterObservation{DeclaredRouter: view.DeclaredRouter, EffectiveRouter: view.EffectiveRouter, RouteRouter: view.RouteRouter, NextChangeAt: view.NextChangeAt}
}

// Runtime callers have already admitted the operator graph at load/apply. The
// dynamic public entry points retain their own external validation boundary.
func prepareRouterConfig(router *api.Router, targetOS platform.OS) (*preparedRouterConfig, error) {
	if router == nil {
		return nil, fmt.Errorf("startup router is required")
	}
	startup, err := dynamicconfig.PrepareValidatedStartupForOS(*router, targetOS)
	if err != nil {
		return nil, err
	}
	prepared := &preparedRouterConfig{declared: router, startup: startup, staticSAM: startup.Router(), targetOS: targetOS}
	for _, resource := range startup.Router().Spec.Resources {
		if resource.Kind != "WireGuardInterface" {
			continue
		}
		spec, err := resource.WireGuardInterfaceSpec()
		if err != nil {
			return nil, err
		}
		for _, source := range spec.PeersFrom {
			if strings.HasPrefix(strings.TrimSpace(source.Resource), "SAMEnrollmentPolicy/") {
				// Claim signature and expiry decisions are live observations.
				prepared.timedSAM = true
			}
		}
	}
	if !prepared.timedSAM {
		prepared.staticSAM, err = resolveWireGuardSAMResources(startup.Router(), time.Time{})
		if err != nil {
			return nil, err
		}
	}
	return prepared, nil
}

// currentPreparedRouter captures declared intent and its preparation together.
// Initial struct literals are admitted once here; later runtime changes publish
// a fully prepared replacement under the same routerMu.
func (r *Runner) currentPreparedRouter() (*preparedRouterConfig, error) {
	r.routerMu.Lock()
	defer r.routerMu.Unlock()
	if r.preparedRouter != nil {
		return r.preparedRouter, nil
	}
	prepared, err := prepareRouterConfig(r.Router, platform.CurrentOS())
	if err != nil {
		return nil, err
	}
	r.preparedRouter = prepared
	return prepared, nil
}

func (r *Runner) publishPreparedRouter(prepared *preparedRouterConfig) {
	r.routerMu.Lock()
	defer r.routerMu.Unlock()
	r.Router = prepared.declared
	r.preparedRouter = prepared
}

func (r *Runner) effectiveDynamicRouteSAMViewForReconcile(store eventedStore, now time.Time, targetOS platform.OS) (dynamicRouteSAMView, error) {
	prepared, err := r.currentPreparedRouter()
	if err != nil {
		return dynamicRouteSAMView{}, err
	}
	if prepared.targetOS != targetOS {
		return dynamicRouteSAMView{}, fmt.Errorf("prepared router OS %q differs from observation OS %q", prepared.targetOS, targetOS)
	}
	store = store.withRouter(prepared.declared)
	if err := r.saveWhenFalseStatuses(store); err != nil {
		return dynamicRouteSAMView{}, err
	}
	lister, _ := r.Store.(dynamicConfigPartLister)
	return prepared.observeRuntimeRouter(store, lister, now, targetOS)
}

// ObserveRuntimeRouter uses the same live selection as reconciliation without
// publishing statuses, running controllers, or changing native state.
func (r *Runner) ObserveRuntimeRouter(now time.Time, targetOS platform.OS) (RuntimeRouterObservation, error) {
	prepared, err := r.currentPreparedRouter()
	if err != nil {
		return RuntimeRouterObservation{}, err
	}
	store := eventedStore{Store: r.Store, Bus: r.Bus}.withRouter(prepared.declared)
	lister, _ := r.Store.(dynamicConfigPartLister)
	view, err := prepared.observeRuntimeRouter(store, lister, now, targetOS)
	if err != nil {
		return RuntimeRouterObservation{}, err
	}
	return runtimeRouterObservation(view), nil
}

func (p *preparedRouterConfig) observeRuntimeRouter(store resourcequery.StateStore, lister dynamicConfigPartLister, now time.Time, targetOS platform.OS) (dynamicRouteSAMView, error) {
	if p.targetOS != targetOS {
		return dynamicRouteSAMView{}, fmt.Errorf("prepared router OS %q differs from observation OS %q", p.targetOS, targetOS)
	}
	observation, err := observeDynamicRouteSAM(lister, now, targetOS)
	if err != nil {
		return dynamicRouteSAMView{}, err
	}
	base, err := p.routerForDynamicObservation(observation, now)
	if err != nil {
		return dynamicRouteSAMView{}, err
	}
	selected, err := resourcequery.FilterRouterByWhen(base, store)
	if err != nil {
		return dynamicRouteSAMView{}, err
	}
	view, err := p.buildDynamicRouteSAMView(selected, observation, now)
	if err != nil {
		return dynamicRouteSAMView{}, err
	}
	// Native parent authority must retain the current unselected graph: a when
	// mask cannot turn a declared virtual parent into an unowned physical device.
	view.ParentRouter = base
	return view, nil
}
