// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

// RuntimeSnapshot describes the confirmed controller generation, independently
// of Runner.Router, which is also used while preparing a replacement. Router
// must be treated as immutable by readers. Epoch is process-local and advances
// only when a prepared generation is handed to its event loop.
type RuntimeSnapshot struct {
	Router    *api.Router
	Epoch     uint64
	Known     bool
	Available bool
}

type RuntimeReloadOutcome struct {
	Active      RuntimeSnapshot
	OperationID uint64
	Accepted    bool
	// Restored means the previous configuration was rebuilt after the requested
	// configuration failed. During a rollback request this can be the new config.
	Restored bool
}

var ErrRuntimeMutationStopped = errors.New("runtime mutation admission is stopped")

type generationReload struct {
	router *api.Router
	ctx    context.Context
	id     uint64
	done   chan generationReloadResult
}

type generationReloadResult struct {
	outcome RuntimeReloadOutcome
	err     error
}

func (r *Runner) ReloadRuntime(ctx context.Context, router *api.Router) error {
	_, err := r.ReloadRuntimeWithOutcome(ctx, router)
	return err
}

// ReloadRuntimeWithOutcome retains the caller's transaction responsibility
// until an accepted operation is acknowledged. In particular, the caller must
// not release its exclusive mutation gate just because its context expires.
func (r *Runner) ReloadRuntimeWithOutcome(ctx context.Context, router *api.Router) (RuntimeReloadOutcome, error) {
	initial := RuntimeReloadOutcome{Active: r.ActiveRuntimeSnapshot()}
	if router == nil {
		return initial, errors.New("reload router is required")
	}
	if err := ctx.Err(); err != nil {
		return initial, err
	}
	ch := r.runtimeReloadChannel()
	r.reloadMu.Lock()
	if r.runtimeFenced {
		r.reloadMu.Unlock()
		return initial, ErrRuntimeMutationStopped
	}
	r.runtimeOperation++
	request := generationReload{router: router, ctx: ctx, id: r.runtimeOperation, done: make(chan generationReloadResult, 1)}
	stopping := r.runtimeStop
	grace := r.reloadCancelGrace
	r.reloadMu.Unlock()
	if grace <= 0 {
		grace = time.Second
	}
	initial.OperationID = request.id
	select {
	case ch <- request:
	case <-ctx.Done():
		return initial, ctx.Err()
	case <-stopping:
		return initial, ErrRuntimeMutationStopped
	}
	select {
	case result := <-request.done:
		return result.outcome, result.err
	case <-ctx.Done():
	case <-stopping:
	}
	// A builder can ignore context cancellation. Do not launch another builder
	// or unlock the transaction while it can still mutate shared state. Ask the
	// existing serve supervisor to stop once cooperative cancellation has had a
	// short grace period; the acknowledgement still owns completion.
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case result := <-request.done:
		return result.outcome, result.err
	case <-timer.C:
		r.fenceRuntime(true)
	}
	result := <-request.done
	return result.outcome, result.err
}

func (r *Runner) ActiveRuntimeSnapshot() RuntimeSnapshot {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	return r.runtimeSnapshot
}

func (r *Runner) RuntimeMutationError() error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if r.runtimeFenced {
		return ErrRuntimeMutationStopped
	}
	return nil
}

// UpdateRuntimeRouter updates a confirmed generation after a shape-preserving
// apply. The caller must hold the exclusive mutation gate and verify that the
// controller/daemon lifecycle shape is unchanged. It cannot revive a stopped
// generation or turn an in-progress preparation into a confirmed one.
func (r *Runner) UpdateRuntimeRouter(router *api.Router) error {
	if router == nil {
		return errors.New("runtime router is required")
	}
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if r.runtimeFenced || !r.runtimeSnapshot.Known || !r.runtimeSnapshot.Available {
		return ErrRuntimeMutationStopped
	}
	r.setRuntimeRouter(router)
	r.runtimeSnapshot.Router = router
	return nil
}

func (r *Runner) currentRouter() *api.Router {
	r.routerMu.RLock()
	defer r.routerMu.RUnlock()
	return r.Router
}

func (r *Runner) setRuntimeRouter(router *api.Router) {
	r.routerMu.Lock()
	defer r.routerMu.Unlock()
	r.Router = router
}

func (r *Runner) runtimeReloadChannel() chan generationReload {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if r.reloadCh == nil {
		r.reloadCh = make(chan generationReload)
	}
	if r.runtimeStop == nil {
		r.runtimeStop = make(chan struct{})
	}
	return r.reloadCh
}

func (r *Runner) runtimeReadyChannel() <-chan struct{} {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if r.runtimeReady == nil {
		r.runtimeReady = make(chan struct{})
	}
	return r.runtimeReady
}

func (r *Runner) markRuntimeReady() {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if r.runtimeReady == nil {
		r.runtimeReady = make(chan struct{})
	}
	if !r.runtimeReadyClosed {
		close(r.runtimeReady)
		r.runtimeReadyClosed = true
	}
}

func (r *Runner) publishRuntimeGeneration(generation *controllerGeneration) bool {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if r.runtimeFenced || generation.ctx.Err() != nil {
		return false
	}
	r.runtimeSnapshot = RuntimeSnapshot{Router: generation.router, Epoch: r.runtimeSnapshot.Epoch + 1, Known: true, Available: true}
	return true
}

func (r *Runner) clearRuntimeGeneration(known bool) {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	r.runtimeSnapshot = RuntimeSnapshot{Epoch: r.runtimeSnapshot.Epoch, Known: known}
}

func (r *Runner) fenceRuntime(cancelServe bool) {
	r.reloadMu.Lock()
	wasFenced := r.runtimeFenced
	r.runtimeFenced = true
	if r.runtimeSnapshot.Available {
		r.runtimeSnapshot = RuntimeSnapshot{Epoch: r.runtimeSnapshot.Epoch}
	}
	notifyServe := cancelServe && !r.runtimeCancelRequested
	if notifyServe {
		r.runtimeCancelRequested = true
	}
	if r.runtimeStop == nil {
		r.runtimeStop = make(chan struct{})
	}
	if !wasFenced {
		close(r.runtimeStop)
	}
	r.reloadMu.Unlock()
	if notifyServe && r.CancelServe != nil {
		r.CancelServe()
	}
}

func (r *Runner) acknowledgeRuntimeReload(request generationReload, restored bool, err error) {
	if request.ctx != nil {
		err = errors.Join(err, request.ctx.Err())
	}
	request.done <- generationReloadResult{outcome: RuntimeReloadOutcome{Active: r.ActiveRuntimeSnapshot(), OperationID: request.id, Accepted: true, Restored: restored}, err: err}
}

func runtimeStoppedError(err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("controller generation unavailable: %w", ErrRuntimeMutationStopped)
}
