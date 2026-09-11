// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/config"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/eventlog"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type canonicalWriteState string

const (
	canonicalNotRequested canonicalWriteState = "not-requested"
	canonicalNotReplaced  canonicalWriteState = "not-replaced"
	canonicalReplaced     canonicalWriteState = "replaced"
	canonicalUnknown      canonicalWriteState = "unknown"
)

// applyAttemptOutcome records independent facts, including partial progress.
// Phase remains a dataplane observation, never a persistence acknowledgement.
type applyAttemptOutcome struct {
	Result              *apply.Result
	Stage               string
	Generation          int64
	GenerationCreated   bool
	Canonical           canonicalWriteState
	DurabilityConfirmed bool
	TerminalAttempted   bool
	TerminalSucceeded   bool
	Runtime             controllerchain.RuntimeReloadOutcome
}

// Hooks are per attempt and deliberately narrow: tests still exercise the
// actual generation store and orchestration without touching host controllers.
type applyAttemptHooks struct {
	finishGeneration     func(*routerstate.SQLiteStore, int64, string, []string) error
	writeCanonical       func(string, []byte) (config.AtomicWriteOutcome, error)
	configureControllers func(*controllerchain.Options)
}

type applyAttemptError struct {
	Outcome applyAttemptOutcome
	Cause   error
}

func (e *applyAttemptError) Error() string {
	return fmt.Sprintf("apply failed: stage=%s generation=%d canonical=%s durabilityConfirmed=%t terminalAttempted=%t terminalSucceeded=%t runtimeKnown=%t runtimeAvailable=%t activeEpoch=%d operation=%d", e.Outcome.Stage, e.Outcome.Generation, e.Outcome.Canonical, e.Outcome.DurabilityConfirmed, e.Outcome.TerminalAttempted, e.Outcome.TerminalSucceeded, e.Outcome.Runtime.Active.Known, e.Outcome.Runtime.Active.Available, e.Outcome.Runtime.Active.Epoch, e.Outcome.Runtime.OperationID)
}

func (e *applyAttemptError) Unwrap() error { return e.Cause }

func (o *applyAttemptOutcome) finish(store *routerstate.SQLiteStore, opts applyOptions, phase string, warnings []string) error {
	if !o.GenerationCreated || o.TerminalAttempted {
		return nil
	}
	o.TerminalAttempted = true
	finish := (*routerstate.SQLiteStore).FinishGeneration
	if opts.attemptHooks != nil && opts.attemptHooks.finishGeneration != nil {
		finish = opts.attemptHooks.finishGeneration
	}
	err := finish(store, o.Generation, phase, warnings)
	o.TerminalSucceeded = err == nil
	return err
}

func (o *applyAttemptOutcome) finishEarlyFailure(store *routerstate.SQLiteStore, opts applyOptions, err *error) {
	if *err != nil && o.GenerationCreated && !o.TerminalAttempted {
		if finishErr := o.finish(store, opts, "Errored", nil); finishErr != nil {
			*err = errors.Join(*err, fmt.Errorf("record failed generation: %w", finishErr))
		}
	}
}

func (o *applyAttemptOutcome) wrapError(err *error, logger *eventlog.Logger) {
	if *err == nil {
		return
	}
	*err = &applyAttemptError{Outcome: *o, Cause: *err}
	if logger != nil {
		logger.Emit(eventlog.LevelError, "apply", (*err).Error(), nil)
	}
}

func (o *applyAttemptOutcome) commit(opts applyOptions, configYAML string) error {
	if opts.DryRun || opts.SkipConfigCommit || strings.TrimSpace(opts.ConfigPath) == "" {
		return nil
	}
	o.Stage = "canonical-write"
	o.Canonical = canonicalNotReplaced
	write := config.AtomicWriteFileWithOutcome
	if opts.attemptHooks != nil && opts.attemptHooks.writeCanonical != nil {
		write = opts.attemptHooks.writeCanonical
	}
	written, err := write(opts.ConfigPath, []byte(configYAML))
	if written.Replaced {
		o.Canonical = canonicalReplaced
	}
	o.DurabilityConfirmed = written.DurabilityConfirmed
	return err
}
