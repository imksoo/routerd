// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"

	"github.com/imksoo/routerd/pkg/api"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
)

// mutateRuntime retains the existing exclusive mutation owner through canonical
// persistence and activation. Candidate workers, sockets and native effects are
// authorized only by the durably acknowledged desired configuration. A later
// apply failure records partial progress; it does not silently restore old intent.
func (m serveConfigMutator) mutateRuntime(next *api.Router, configYAML string) (outcome applyAttemptOutcome, err error) {
	previous := m.getRouter()
	shapeChanged, _ := runtimeShapeChanged(previous, next)
	initial := controllerchain.RuntimeReloadOutcome{}
	if m.activeRuntime != nil {
		initial.Active = m.activeRuntime()
	}
	defer func() {
		if err != nil {
			outcome.wrapError(&err, m.logger)
		}
		m.cache.StoreAttempt(outcome, err)
	}()
	requested := false
	operation := m
	operation.baseOpts.applyRuntime = func(ctx context.Context, commit func() error) (controllerchain.RuntimeReloadOutcome, error) {
		requested = true
		return m.applyRuntimeOutcome(ctx, next, shapeChanged, commit)
	}
	outcome, err = operation.reconcileWithOutcome(next, configYAML)
	if !requested {
		outcome.Runtime = initial
	}
	if m.activeRuntime != nil {
		outcome.Runtime.Active = m.activeRuntime()
	}
	m.publishConfirmedRuntime(outcome.Runtime.Active)
	return outcome, err
}

func (m serveConfigMutator) publishConfirmedRuntime(active controllerchain.RuntimeSnapshot) {
	if active.Known && active.Available && active.Router != nil && m.setRouter != nil && (m.getRouter == nil || m.getRouter() != active.Router) {
		m.setRouter(active.Router)
	}
}
