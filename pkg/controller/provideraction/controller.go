// SPDX-License-Identifier: BSD-3-Clause

package provideractioncontroller

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	enginepkg "github.com/imksoo/routerd/pkg/provideraction"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// Store is the action journal + dynamic part surface required by the
// controller. *state.SQLiteStore satisfies it.
type Store interface {
	enginepkg.Store
}

// Controller imports provider ActionPlans and, when ProviderActionPolicy permits
// policy auto-approval, executes pending provider actions through the same
// Engine path used by routerctl action execute.
type Controller struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	Runner enginepkg.ExecutorRunner
	Now    func() time.Time
	DryRun bool
	Logger *slog.Logger
}

func (c Controller) Reconcile(ctx context.Context) error {
	if c.Store == nil {
		return nil
	}
	now := c.now()
	policy, plugins, err := enginepkg.PolicyAndPlugins(c.Router)
	if err != nil {
		return err
	}
	runner := c.Runner
	if runner == nil {
		runner = enginepkg.RunExecutor
	}
	engine, err := enginepkg.NewEngine(enginepkg.Config{
		Store:   c.Store,
		Runner:  runner,
		Now:     func() time.Time { return now },
		Log:     controllerLogger{logger: c.Logger},
		Plugins: plugins,
	})
	if err != nil {
		return err
	}
	if _, err := engine.ImportFromDynamicParts(); err != nil {
		return err
	}
	if c.DryRun {
		c.log("provideraction: auto execution dry-run disabled")
		return nil
	}
	enabled, reason := enginepkg.AutoExecutionEnabled(policy)
	if !enabled {
		c.log("provideraction: auto execution disabled: " + reason)
		return nil
	}
	if _, err := engine.RecoverStaleRunningActions(); err != nil {
		return err
	}
	rows, err := c.Store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return fmt.Errorf("list action journal: %w", err)
	}
	candidates := autoExecutionCandidates(rows, policy.MaxActionsPerRun)
	for _, row := range candidates {
		if err := engine.Execute(ctx, row.ID, enginepkg.ModeExecute, policy); err != nil {
			c.log("provideraction: auto execute action failed", "id", row.ID, "key", row.IdempotencyKey, "error", err)
			continue
		}
		updated, found, err := c.Store.GetActionByID(row.ID)
		if err != nil {
			return fmt.Errorf("load executed action %d: %w", row.ID, err)
		}
		if found {
			_ = c.publishProviderCaptureChanged(ctx, updated, now)
		}
	}
	return nil
}

func (c Controller) publishProviderCaptureChanged(ctx context.Context, row routerstate.ActionExecutionRecord, now time.Time) error {
	if c.Bus == nil || row.Status != routerstate.ActionSucceeded || !providerCaptureAction(row.Action) {
		return nil
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "provider-action-execution", Kind: "provider-action-execution"}, enginepkg.ProviderCaptureChangedEvent, daemonapi.SeverityInfo)
	event.Time = now
	event.Attributes = map[string]string{
		"actionID":       fmt.Sprint(row.ID),
		"action":         row.Action,
		"provider":       row.Provider,
		"providerRef":    row.ProviderRef,
		"idempotencyKey": row.IdempotencyKey,
	}
	return c.Bus.Publish(ctx, event)
}

func providerCaptureAction(action string) bool {
	switch strings.TrimSpace(action) {
	case "assign-secondary-ip", "unassign-secondary-ip":
		return true
	default:
		return false
	}
}

func (c Controller) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c Controller) log(msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.Debug(msg, args...)
	}
}

func autoExecutionCandidates(rows []routerstate.ActionExecutionRecord, limit int) []routerstate.ActionExecutionRecord {
	if limit <= 0 {
		return nil
	}
	out := make([]routerstate.ActionExecutionRecord, 0, limit)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	for _, row := range rows {
		switch row.Status {
		case routerstate.ActionPending, routerstate.ActionApproved:
			out = append(out, row)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

type controllerLogger struct {
	logger *slog.Logger
}

func (l controllerLogger) Printf(format string, args ...any) {
	if l.logger != nil {
		l.logger.Debug(fmt.Sprintf(format, args...))
	}
}
