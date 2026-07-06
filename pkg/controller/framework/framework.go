// SPDX-License-Identifier: BSD-3-Clause

package framework

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/lock"
	routerotel "github.com/imksoo/routerd/pkg/otel"
)

type Controller interface {
	Name() string
	Subscriptions() []bus.Subscription
	Reconcile(context.Context, daemonapi.DaemonEvent) error
	PeriodicReconcile(context.Context) (bool, error)
}

type EventBus interface {
	Subscribe(context.Context, bus.Subscription, int) (<-chan daemonapi.DaemonEvent, func())
}

type Observer interface {
	ControllerStarted(name string, interval time.Duration)
	ControllerReconciled(name, trigger string, interval, duration time.Duration, err error)
}

// ResourceObserver optionally augments Observer with the resource that
// triggered the reconcile. Observers that implement this interface receive the
// resource kind/name in addition to the trigger label.
type ResourceObserver interface {
	ControllerReconciledResource(name, trigger, resourceKind, resourceName string, interval, duration time.Duration, err error)
}

type Runner struct {
	Bus      EventBus
	Locker   *lock.ResourceLocker
	Logger   *slog.Logger
	Interval time.Duration
	Observer Observer
}

type FuncController struct {
	ControllerName string
	Subs           []bus.Subscription
	Every          time.Duration
	ReconcileFunc  func(context.Context, daemonapi.DaemonEvent) error
	PeriodicFunc   func(context.Context) (bool, error)
}

func (c FuncController) Name() string {
	if c.ControllerName == "" {
		return "controller"
	}
	return c.ControllerName
}

func (c FuncController) Subscriptions() []bus.Subscription {
	return c.Subs
}

func (c FuncController) Reconcile(ctx context.Context, event daemonapi.DaemonEvent) error {
	if c.ReconcileFunc != nil {
		return c.ReconcileFunc(ctx, event)
	}
	if c.PeriodicFunc != nil {
		_, err := c.PeriodicFunc(ctx)
		return err
	}
	return nil
}

func (c FuncController) PeriodicReconcile(ctx context.Context) (bool, error) {
	if c.PeriodicFunc != nil {
		return c.PeriodicFunc(ctx)
	}
	if c.ReconcileFunc != nil {
		err := c.ReconcileFunc(ctx, daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "event-loop"}, "routerd.controller.periodic", daemonapi.SeverityDebug))
		return false, err
	}
	return false, nil
}

func (r Runner) Run(ctx context.Context, controllers ...Controller) error {
	if r.Bus == nil {
		return fmt.Errorf("bus is required")
	}
	interval := r.Interval
	if interval == 0 {
		interval = 5 * time.Minute
	}
	logger := r.Logger
	if logger == nil {
		logger = slog.Default()
	}
	locker := r.Locker
	if locker == nil {
		locker = lock.NewResourceLocker()
	}

	var wg sync.WaitGroup
	for _, controller := range controllers {
		controller := controller
		subs := controller.Subscriptions()
		if len(subs) == 0 {
			subs = []bus.Subscription{{Topics: []string{"routerd.resource.status.changed", "routerd.controller.bootstrap"}}}
		}
		for _, sub := range subs {
			events, cancel := r.Bus.Subscribe(ctx, sub, 64)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer cancel()
				runController(ctx, logger, locker, r.Observer, controllerInterval(controller, interval), controller, events)
			}()
		}
	}
	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}

func (r Runner) RunOnce(ctx context.Context, controllers ...Controller) error {
	interval := r.Interval
	if interval == 0 {
		interval = 5 * time.Minute
	}
	logger := r.Logger
	if logger == nil {
		logger = slog.Default()
	}
	locker := r.Locker
	if locker == nil {
		locker = lock.NewResourceLocker()
	}

	var errs []error
	for _, controller := range controllers {
		interval := controllerInterval(controller, interval)
		if r.Observer != nil {
			r.Observer.ControllerStarted(controller.Name(), interval)
		}
		err := runLocked(ctx, logger, locker, r.Observer, controller.Name()+":once", controller.Name(), "once", "", "", interval, func(runCtx context.Context) error {
			_, err := controller.PeriodicReconcile(runCtx)
			return err
		})
		if err != nil {
			errs = append(errs, err)
		}
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
	}
	return errors.Join(errs...)
}

func runController(ctx context.Context, logger *slog.Logger, locker *lock.ResourceLocker, observer Observer, interval time.Duration, controller Controller, events <-chan daemonapi.DaemonEvent) {
	intervals := adaptiveReconcileIntervalsForMax(interval)
	level := 0
	ticker := time.NewTicker(intervals[level])
	defer ticker.Stop()
	if observer != nil {
		observer.ControllerStarted(controller.Name(), intervals[level])
	}
	bootstrap := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "event-loop"}, "routerd.controller.bootstrap", daemonapi.SeverityInfo)
	runLocked(ctx, logger, locker, observer, controller.Name()+":bootstrap", controller.Name(), "bootstrap", "", "", intervals[level], func(runCtx context.Context) error {
		return controller.Reconcile(runCtx, bootstrap)
	})
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			key := eventResourceKey(event)
			kind, name := eventResourceKindName(event)
			runLocked(ctx, logger, locker, observer, key, controller.Name(), event.Type, kind, name, intervals[level], func(runCtx context.Context) error {
				return controller.Reconcile(runCtx, event)
			})
			level = 0
			ticker.Reset(intervals[level])
		case <-ticker.C:
			didWork := false
			nextLevel := level
			_ = runLockedObservedInterval(ctx, logger, locker, observer, controller.Name()+":periodic", controller.Name(), "periodic", "", "", intervals[level], func(runErr error) time.Duration {
				nextLevel = nextAdaptiveReconcileLevel(level, didWork, runErr, len(intervals)-1)
				return intervals[nextLevel]
			}, func(runCtx context.Context) error {
				worked, err := controller.PeriodicReconcile(runCtx)
				didWork = worked
				return err
			})
			level = nextLevel
			ticker.Reset(intervals[level])
		case <-ctx.Done():
			return
		}
	}
}

func adaptiveReconcileIntervalsForMax(maxInterval time.Duration) []time.Duration {
	base := []time.Duration{time.Second, 3 * time.Second, 7 * time.Second, 15 * time.Second, 31 * time.Second}
	if maxInterval <= 0 {
		return base
	}
	intervals := make([]time.Duration, 0, len(base))
	for _, interval := range base {
		if interval >= maxInterval {
			intervals = append(intervals, maxInterval)
			break
		}
		intervals = append(intervals, interval)
	}
	if maxInterval > base[len(base)-1] {
		intervals = append(intervals, maxInterval)
	}
	if len(intervals) == 0 {
		return []time.Duration{maxInterval}
	}
	return intervals
}

func nextAdaptiveReconcileLevel(current int, didWork bool, err error, maxLevel int) int {
	if didWork || err != nil {
		return 0
	}
	if current < maxLevel {
		return current + 1
	}
	return current
}

func controllerInterval(controller Controller, fallback time.Duration) time.Duration {
	if typed, ok := controller.(interface{ Interval() time.Duration }); ok {
		if interval := typed.Interval(); interval > 0 {
			return interval
		}
	}
	if typed, ok := controller.(FuncController); ok && typed.Every > 0 {
		return typed.Every
	}
	return fallback
}

func runLocked(ctx context.Context, logger *slog.Logger, locker *lock.ResourceLocker, observer Observer, key, name, trigger, resourceKind, resourceName string, interval time.Duration, fn func(context.Context) error) error {
	return runLockedObservedInterval(ctx, logger, locker, observer, key, name, trigger, resourceKind, resourceName, interval, nil, fn)
}

func runLockedObservedInterval(ctx context.Context, logger *slog.Logger, locker *lock.ResourceLocker, observer Observer, key, name, trigger, resourceKind, resourceName string, interval time.Duration, observedInterval func(error) time.Duration, fn func(context.Context) error) error {
	unlock, err := locker.Lock(ctx, key)
	if err != nil {
		logger.Warn("controller lock skipped", "controller", name, "error", err)
		return err
	}
	defer unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Error("controller panic recovered", "controller", name, "panic", recovered)
		}
	}()
	start := time.Now()
	err = routerotel.Reconcile(ctx, name, trigger, interval, fn)
	duration := time.Since(start)
	reportInterval := interval
	if observedInterval != nil {
		reportInterval = observedInterval(err)
	}
	if observer != nil {
		if resourceObserver, ok := observer.(ResourceObserver); ok {
			resourceObserver.ControllerReconciledResource(name, trigger, resourceKind, resourceName, reportInterval, duration, err)
		} else {
			observer.ControllerReconciled(name, trigger, reportInterval, duration, err)
		}
	}
	attrs := []any{"controller", name, "trigger", trigger, "duration", duration.String(), "interval", reportInterval.String()}
	if err != nil {
		logger.Warn("controller reconcile failed", append(attrs, "error", err)...)
		return err
	}
	if trigger == "bootstrap" {
		logger.Info("controller reconcile completed", attrs...)
		return nil
	}
	logger.Debug("controller reconcile completed", attrs...)
	return err
}

func eventResourceKey(event daemonapi.DaemonEvent) string {
	if event.Resource == nil {
		return event.Daemon.Kind + "/" + event.Daemon.Name
	}
	return event.Resource.APIVersion + "/" + event.Resource.Kind + "/" + event.Resource.Name
}

func eventResourceKindName(event daemonapi.DaemonEvent) (string, string) {
	if event.Resource == nil {
		return "", ""
	}
	return event.Resource.Kind, event.Resource.Name
}
