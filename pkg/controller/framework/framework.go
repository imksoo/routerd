package framework

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/lock"
	routerotel "routerd/pkg/otel"
)

type Controller interface {
	Name() string
	Subscriptions() []bus.Subscription
	Reconcile(context.Context, daemonapi.DaemonEvent) error
	PeriodicReconcile(context.Context) error
}

type EventBus interface {
	Subscribe(context.Context, bus.Subscription, int) (<-chan daemonapi.DaemonEvent, func())
}

type Runner struct {
	Bus      EventBus
	Locker   *lock.ResourceLocker
	Logger   *slog.Logger
	Interval time.Duration
}

type FuncController struct {
	ControllerName string
	Subs           []bus.Subscription
	Every          time.Duration
	ReconcileFunc  func(context.Context, daemonapi.DaemonEvent) error
	PeriodicFunc   func(context.Context) error
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
		return c.PeriodicFunc(ctx)
	}
	return nil
}

func (c FuncController) PeriodicReconcile(ctx context.Context) error {
	if c.PeriodicFunc != nil {
		return c.PeriodicFunc(ctx)
	}
	if c.ReconcileFunc != nil {
		return c.ReconcileFunc(ctx, daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "event-loop"}, "routerd.controller.periodic", daemonapi.SeverityDebug))
	}
	return nil
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
				runController(ctx, logger, locker, controllerInterval(controller, interval), controller, events)
			}()
		}
	}
	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}

func runController(ctx context.Context, logger *slog.Logger, locker *lock.ResourceLocker, interval time.Duration, controller Controller, events <-chan daemonapi.DaemonEvent) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	bootstrap := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "event-loop"}, "routerd.controller.bootstrap", daemonapi.SeverityInfo)
	runLocked(ctx, logger, locker, controller.Name()+":bootstrap", controller.Name(), func(runCtx context.Context) error {
		return controller.Reconcile(runCtx, bootstrap)
	})
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			key := eventResourceKey(event)
			runLocked(ctx, logger, locker, key, controller.Name(), func(runCtx context.Context) error {
				return controller.Reconcile(runCtx, event)
			})
		case <-ticker.C:
			runLocked(ctx, logger, locker, controller.Name()+":periodic", controller.Name(), controller.PeriodicReconcile)
		case <-ctx.Done():
			return
		}
	}
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

func runLocked(ctx context.Context, logger *slog.Logger, locker *lock.ResourceLocker, key, name string, fn func(context.Context) error) {
	unlock, err := locker.Lock(ctx, key)
	if err != nil {
		logger.Warn("controller lock skipped", "controller", name, "error", err)
		return
	}
	defer unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Error("controller panic recovered", "controller", name, "panic", recovered)
		}
	}()
	if err := routerotel.Reconcile(ctx, name, fn); err != nil {
		logger.Warn("controller reconcile failed", "controller", name, "error", err)
	}
}

func eventResourceKey(event daemonapi.DaemonEvent) string {
	if event.Resource == nil {
		return event.Daemon.Kind + "/" + event.Daemon.Name
	}
	return event.Resource.APIVersion + "/" + event.Resource.Kind + "/" + event.Resource.Name
}
