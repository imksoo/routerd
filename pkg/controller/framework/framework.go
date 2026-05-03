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
		for _, sub := range controller.Subscriptions() {
			events, cancel := r.Bus.Subscribe(ctx, sub, 64)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer cancel()
				runController(ctx, logger, locker, interval, controller, events)
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
