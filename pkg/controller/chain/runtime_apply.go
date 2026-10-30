// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/platform"
)

type runtimeApplyAdmission struct {
	decision ha.Decision
	config   ha.Config
	borrowed bool
	newLease bool
}

func (a runtimeApplyAdmission) closeNew() {
	if a.newLease && a.decision.Lease != nil {
		_ = a.decision.Lease.Close()
	}
}

func runtimeClusterConfig(router *api.Router) (ha.Config, bool, error) {
	resource, spec, ok, err := routerdClusterResource(router)
	if err != nil || !ok {
		return ha.Config{}, false, err
	}
	ttl := 30 * time.Second
	if strings.TrimSpace(spec.LeaseTTL) != "" {
		var parseErr error
		ttl, parseErr = time.ParseDuration(spec.LeaseTTL)
		if parseErr != nil {
			return ha.Config{}, false, parseErr
		}
	}
	cfg := ha.Config{Name: resource.Metadata.Name, Identity: spec.Identity, Peers: spec.Peers, LeasePath: strings.TrimSpace(spec.LeasePath), TTL: ttl}
	return cfg, cfg.LeasePath != "" && len(cfg.Peers) > 0, nil
}

func (r *Runner) validateSelectedApplyOwner(current *controllerGeneration) error {
	if current == nil {
		return ErrRuntimeMutationStopped
	}
	err := current.ctx.Err()
	if err == nil && current.decision.Enabled {
		if current.decision.Lease == nil {
			err = errors.New("selected HA leader has no native lease owner")
		} else {
			same, observeErr := current.decision.Lease.OwnsPath(current.decision.Lease.Config.LeasePath)
			if observeErr != nil || !same {
				err = errors.Join(observeErr, errors.New("selected HA lease ownership is unavailable"))
			}
		}
	}
	if err != nil {
		r.fenceRuntime(true)
		current.cancel()
	}
	return err
}

func (r *Runner) admitRuntimeApply(ctx context.Context, current *controllerGeneration, next *api.Router, _ eventedStore) (runtimeApplyAdmission, bool, error) {
	admission := runtimeApplyAdmission{decision: ha.Decision{Leader: true}}
	if err := ctx.Err(); err != nil {
		return admission, false, err
	}
	if current == nil || current.ctx.Err() != nil {
		return admission, false, ErrRuntimeMutationStopped
	}
	if current.decision.Enabled && !current.decision.Leader {
		return admission, true, nil
	}
	if current.decision.Enabled {
		if current.decision.Lease == nil {
			return admission, false, errors.New("selected HA leader has no native lease owner")
		}
		owned, err := current.decision.Lease.OwnsPath(current.decision.Lease.Config.LeasePath)
		if err != nil || !owned {
			r.fenceRuntime(true)
			return admission, false, errors.Join(err, errors.New("selected HA lease ownership is unavailable"))
		}
	}
	cfg, enabled, err := runtimeClusterConfig(next)
	if err != nil {
		return admission, false, err
	}
	admission.config = cfg
	if !enabled {
		return admission, false, nil
	}
	if current.decision.Lease != nil {
		same, err := current.decision.Lease.OwnsPath(cfg.LeasePath)
		if err != nil {
			return admission, false, err
		}
		if same {
			admission.decision = current.decision
			admission.borrowed = true
			return admission, false, nil
		}
	}
	admission.decision, err = ha.Acquire(ctx, cfg)
	admission.newLease = admission.decision.Lease != nil
	return admission, admission.decision.Enabled && !admission.decision.Leader, err
}

// Retain the actual descriptor if a cancelled old actor has not acknowledged
// exit. Runtime fencing stops the process; GC must not release that authority.
func (r *Runner) retainRuntimeLeases(leases ...*ha.AcquiredLease) {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	for _, lease := range leases {
		if lease != nil {
			r.runtimeRetainedLeases = append(r.runtimeRetainedLeases, lease)
		}
	}
}

func decisionForLease(lease *ha.AcquiredLease) ha.Decision {
	return ha.Decision{Enabled: true, Leader: true, Identity: lease.Config.Identity, Holder: lease.Lease.Holder, LeasePath: lease.Config.LeasePath, ExpiresAt: lease.Lease.ExpiresAt, Reason: "held-by-selected-runtime", Lease: lease}
}

func (r *Runner) applyAdmittedRuntime(ctx context.Context, logger *slog.Logger, store eventedStore, current *controllerGeneration, request generationReload, admission runtimeApplyAdmission) (*controllerGeneration, bool, error) {
	if err := request.ctx.Err(); err != nil {
		admission.closeNew()
		return current, false, err
	}
	if err := current.ctx.Err(); err != nil {
		admission.closeNew()
		r.fenceRuntime(true)
		current.cancel()
		return current, false, err
	}
	// Canonical persistence and stopping the loop can wait. Re-observe both
	// actual lease descriptors before candidate preparation or native effects.
	for _, lease := range []*ha.AcquiredLease{current.decision.Lease, admission.decision.Lease} {
		if lease == nil {
			continue
		}
		same, authorityErr := lease.OwnsPath(lease.Config.LeasePath)
		if authorityErr != nil || !same {
			admission.closeNew()
			if lease == current.decision.Lease {
				r.fenceRuntime(true)
				current.cancel()
			}
			return current, false, errors.Join(authorityErr, errors.New("HA lease authority changed while canonical commit waited"))
		}
	}
	if !request.reload {
		defer admission.closeNew()
		if admission.newLease {
			return current, false, errors.New("HA lifecycle changed without a generation reload")
		}
		if err := r.UpdateRuntimeRouter(request.router); err != nil {
			return current, false, err
		}
		current.router = request.router
		required := framework.Runner{Logger: logger, Interval: 30 * time.Second, Observer: r.Opts.ControllerObserver}
		return current, false, required.RunOnce(current.ctx, current.controllers...)
	}
	previous, err := r.currentPreparedRouter()
	if err != nil {
		admission.closeNew()
		return current, false, err
	}
	oldDecision := current.decision
	var heldOld *ha.AcquiredLease
	if oldDecision.Lease != nil {
		heldOld, err = oldDecision.Lease.Transfer(oldDecision.Lease.Config)
		if err != nil {
			admission.closeNew()
			r.fenceRuntime(true)
			current.cancel()
			return current, false, err
		}
	}
	current.cancel()
	r.clearRuntimeGeneration(false)
	if stopErr := r.closeControllerGeneration(current); stopErr != nil {
		r.retainRuntimeLeases(heldOld)
		admission.closeNew()
		return nil, false, stopErr
	}
	if admission.borrowed {
		admission.decision.Lease = heldOld
		var moved *ha.AcquiredLease
		moved, err = heldOld.Transfer(admission.config)
		if err == nil {
			heldOld = moved
			admission.decision.Lease = moved
			err = moved.Refresh()
		}
		if err == nil {
			admission.decision = decisionForLease(moved)
		}
	}
	if err == nil {
		var prepared *preparedRouterConfig
		prepared, err = prepareRouterConfig(request.router, platform.CurrentOS())
		if err == nil {
			r.publishPreparedRouter(prepared)
			var next *controllerGeneration
			next, err = r.prepareControllerGenerationOwned(ctx, request.ctx, logger, store.withRouter(request.router), nil, &admission.decision, true)
			if next != nil {
				if heldOld != nil && heldOld != next.decision.Lease {
					_ = heldOld.Close()
				}
				return next, false, err // A strict effect failure remains visible, not Healthy.
			}
		}
	}
	if r.RuntimeMutationError() != nil || ctx.Err() != nil {
		r.retainRuntimeLeases(heldOld, admission.decision.Lease)
		return nil, false, errors.Join(err, ctx.Err(), r.RuntimeMutationError())
	}
	// A structural failure can restore the acknowledged old generation. Preserve
	// its exact held descriptor across restoration; never reacquire our own lock.
	if admission.borrowed && heldOld != nil {
		restoredLease, restoreErr := heldOld.Transfer(oldDecision.Lease.Config)
		if restoreErr == nil {
			heldOld = restoredLease
			restoreErr = heldOld.Refresh()
		}
		if restoreErr != nil {
			r.retainRuntimeLeases(heldOld)
			r.fenceRuntime(true)
			return nil, false, errors.Join(err, restoreErr)
		}
	}
	if heldOld != nil {
		oldDecision = decisionForLease(heldOld)
	}
	if admission.newLease {
		admission.closeNew()
	}
	r.publishPreparedRouter(previous)
	restored, restoreErr := r.prepareControllerGenerationOwned(ctx, nil, logger, store.withRouter(previous.declared), nil, &oldDecision, true)
	if restored == nil {
		r.retainRuntimeLeases(heldOld)
		return nil, false, errors.Join(err, fmt.Errorf("restore prior runtime: %w", restoreErr))
	}
	return restored, true, errors.Join(err, restoreErr)
}
