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
	observation, err := observeDynamicRouteSAM(lister, now, targetOS)
	if err != nil {
		return dynamicRouteSAMView{}, err
	}
	base, err := prepared.routerForDynamicObservation(observation, now)
	if err != nil {
		return dynamicRouteSAMView{}, err
	}
	selected, err := resourcequery.FilterRouterByWhen(base, store)
	if err != nil {
		return dynamicRouteSAMView{}, err
	}
	return prepared.buildDynamicRouteSAMView(selected, observation, now)
}
