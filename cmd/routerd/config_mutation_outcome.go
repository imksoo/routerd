// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"

	"github.com/imksoo/routerd/pkg/api"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
)

// mutateRuntime runs under the caller's exclusive mutation gate through reload,
// apply, persistence and any acknowledged restoration. Post-rename failures keep
// the requested runtime; a restoration error never proves that the old one runs.
func (m serveConfigMutator) mutateRuntime(next *api.Router, configYAML string) (outcome applyAttemptOutcome, err error) {
	previous := m.getRouter()
	shapeChanged, _ := runtimeShapeChanged(previous, next)
	outcome = applyAttemptOutcome{Stage: "runtime-reload", Canonical: canonicalNotRequested}
	if m.activeRuntime != nil {
		outcome.Runtime.Active = m.activeRuntime()
	}
	defer func() {
		if err != nil {
			// The outer attempt includes restoration, which can change the active
			// snapshot after the one-shot apply has already returned its error.
			outcome.wrapError(&err, m.logger)
			m.cache.StoreAttempt(outcome, err)
		} else {
			m.cache.StoreAttempt(outcome, nil)
		}
	}()
	if shapeChanged {
		outcome.Runtime, err = m.reloadRuntimeOutcome(next)
		if err != nil {
			m.publishConfirmedRuntime(outcome.Runtime.Active)
			return outcome, err
		}
		if !outcome.Runtime.Active.Known || !outcome.Runtime.Active.Available {
			return outcome, errors.New("reload did not confirm an active runtime")
		}
	}
	runtimeOutcome := outcome.Runtime
	outcome, err = m.reconcileWithOutcome(next, configYAML)
	outcome.Runtime = runtimeOutcome
	// Standby is an intentional successful runtime update without a canonical
	// commit; preserve the existing HA contract on this normal path as well.
	standby := err == nil && outcome.Result != nil && outcome.Result.Phase == "Standby"
	if err == nil && !standby && (outcome.Canonical != canonicalReplaced || !outcome.DurabilityConfirmed) {
		outcome.Stage = "canonical-confirmation"
		err = errors.New("apply did not acknowledge durable canonical configuration replacement")
	}
	if outcome.Canonical == canonicalReplaced || standby {
		if shapeChanged {
			m.publishConfirmedRuntime(outcome.Runtime.Active)
		} else {
			if updateErr := m.updateConfirmedRuntime(next); updateErr != nil {
				if err == nil {
					outcome.Stage = "runtime-publish"
				}
				err = errors.Join(err, updateErr)
			}
		}
	} else if shapeChanged && outcome.Canonical != canonicalUnknown {
		restored, restoreErr := m.reloadRuntimeOutcome(previous)
		outcome.Runtime = restored
		m.publishConfirmedRuntime(restored.Active)
		if restoreErr != nil {
			if err == nil {
				outcome.Stage = "runtime-restore"
			}
			err = errors.Join(err, restoreErr)
		}
	}
	if m.activeRuntime != nil {
		outcome.Runtime.Active = m.activeRuntime()
	}
	if err != nil {
		return outcome, err
	}
	return outcome, nil
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
