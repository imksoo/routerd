// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"

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
	initialRuntime := controllerchain.RuntimeReloadOutcome{}
	if m.activeRuntime != nil {
		initialRuntime.Active = m.activeRuntime()
	}
	defer func() {
		if err != nil {
			outcome.wrapError(&err, m.logger)
		}
		m.cache.StoreAttempt(outcome, err)
	}()
	var preparedServers *preparedServeHTTPServers
	defer func() { preparedServers.close() }()
	activationAttempted, activationSucceeded := false, false
	operation := m
	operation.baseOpts.activateCommittedRuntime = func(ctx context.Context) (controllerchain.RuntimeReloadOutcome, error) {
		activationAttempted = true
		active := initialRuntime
		if err := ctx.Err(); err != nil {
			return active, err
		}
		if m.servers != nil {
			var err error
			preparedServers, err = m.servers.prepareCandidate(next)
			if err != nil {
				return active, err
			}
		}
		if shapeChanged {
			var err error
			active, err = m.reloadRuntimeOutcome(next)
			m.publishConfirmedRuntime(active.Active)
			if err != nil {
				return active, err
			}
			if !active.Active.Known || !active.Active.Available {
				return active, errors.New("reload did not confirm an active runtime")
			}
		} else {
			if err := m.updateConfirmedRuntime(next); err != nil {
				if m.activeRuntime != nil {
					active.Active = m.activeRuntime()
				}
				return active, err
			}
			if m.activeRuntime != nil {
				active.Active = m.activeRuntime()
			} else {
				active.Active = controllerchain.RuntimeSnapshot{Router: next, Known: true, Available: true}
			}
		}
		activationSucceeded = true
		return active, nil
	}
	outcome, err = operation.reconcileWithOutcome(next, configYAML)
	if !activationAttempted {
		outcome.Runtime = initialRuntime
	}
	if activationSucceeded && preparedServers != nil {
		if publishErr := preparedServers.publish(); publishErr != nil {
			outcome.Stage = "server-publish"
			err = errors.Join(err, publishErr)
		}
	}
	if m.activeRuntime != nil {
		outcome.Runtime.Active = m.activeRuntime()
	}
	return outcome, err
}

func (m serveConfigMutator) publishConfirmedRuntime(active controllerchain.RuntimeSnapshot) {
	if active.Known && active.Available && active.Router != nil && m.setRouter != nil {
		m.setRouter(active.Router)
	}
}

func (m serveConfigMutator) updateConfirmedRuntime(next *api.Router) error {
	if m.updateRuntime != nil {
		if err := m.updateRuntime(next); err != nil {
			return err
		}
	}
	if m.setRouter != nil {
		m.setRouter(next)
	}
	return nil
}
