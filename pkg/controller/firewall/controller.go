// SPDX-License-Identifier: BSD-3-Clause

package firewall

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/firewallbackend"
	"github.com/imksoo/routerd/pkg/platform"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Controller struct {
	Router       *api.Router
	Bus          *bus.Bus
	Store        Store
	DryRun       bool
	NftablesPath string
	NftCommand   string
	Interval     time.Duration
	Logger       *slog.Logger
}

func (c Controller) Start(ctx context.Context) {
	if c.Router == nil || c.Bus == nil || c.Store == nil {
		return
	}
	interval := c.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.*"}}, 32)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		_ = c.reconcileLogged(ctx)
		for {
			select {
			case event, ok := <-ch:
				if !ok {
					return
				}
				if strings.HasPrefix(event.Type, "routerd.firewall.") {
					continue
				}
				_ = c.reconcileLogged(ctx)
			case <-ticker.C:
				_ = c.reconcileLogged(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c Controller) reconcileLogged(ctx context.Context) error {
	if err := c.Reconcile(ctx); err != nil {
		if c.Logger != nil {
			c.Logger.Warn("firewall reconcile failed", "error", err)
		}
		return err
	}
	return nil
}

func (c Controller) Reconcile(ctx context.Context) error {
	if !hasFirewall(c.Router) {
		return nil
	}
	backend := firewallbackend.ForPlatform(platform.CurrentOS(), c.NftCommand)
	ruleset, err := backend.Render(c.Router, c.NftablesPath)
	if err != nil {
		_ = c.saveErrorStatuses(ctx, backend.Name(), c.NftablesPath, "RenderFailed", err)
		return err
	}
	changed, err := backend.Apply(ctx, ruleset, c.DryRun)
	if err != nil {
		_ = c.saveErrorStatuses(ctx, ruleset.Backend, ruleset.Path, "ApplyFailed", err)
		return err
	}
	return c.savePolicyStatuses(ctx, ruleset.Backend, ruleset.Path, changed, ruleset.InternalHoles)
}

func (c Controller) saveErrorStatuses(ctx context.Context, backend, path, reason string, applyErr error) error {
	status := map[string]any{
		"phase":        "Error",
		"backend":      backend,
		"dryRun":       c.DryRun,
		"reason":       reason,
		"error":        applyErr.Error(),
		"rulesetPath":  path,
		"nftablesPath": path,
		"conditions":   []map[string]any{{"type": "Applied", "status": "False", "reason": reason, "message": applyErr.Error()}},
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.FirewallAPIVersion, resource.Kind, resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	if c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.firewall.rules.error", daemonapi.SeverityError)
		event.Attributes = map[string]string{"backend": backend, "nftablesPath": path, "dryRun": fmt.Sprintf("%t", c.DryRun), "reason": reason, "error": applyErr.Error()}
		_ = c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c Controller) savePolicyStatuses(ctx context.Context, backend, path string, changed bool, internalHoles int) error {
	status := map[string]any{
		"phase":         "Applied",
		"backend":       backend,
		"dryRun":        c.DryRun,
		"changed":       changed,
		"rules":         firewallRuleCount(c.Router),
		"internalHoles": internalHoles,
		"rulesetPath":   path,
		"nftablesPath":  path,
		"conditions":    []map[string]any{{"type": "Applied", "status": "True", "reason": "Rendered"}},
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.FirewallAPIVersion, resource.Kind, resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	if changed && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.firewall.rules.applied", daemonapi.SeverityInfo)
		event.Attributes = map[string]string{"backend": backend, "nftablesPath": path, "dryRun": fmt.Sprintf("%t", c.DryRun), "internalHoles": fmt.Sprintf("%d", internalHoles)}
		_ = c.Bus.Publish(ctx, event)
	}
	return nil
}

func hasFirewall(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.FirewallAPIVersion && (resource.Kind == "FirewallZone" || resource.Kind == "FirewallPolicy" || resource.Kind == "FirewallRule" || resource.Kind == "ClientPolicy" || resource.Kind == "LocalServiceRedirect") {
			return true
		}
	}
	return false
}

func firewallRuleCount(router *api.Router) int {
	n := 0
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.FirewallAPIVersion && resource.Kind == "FirewallRule" {
			n++
		}
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
