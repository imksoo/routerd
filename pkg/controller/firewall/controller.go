package firewall

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/render"
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
	holes := render.InternalFirewallHoles(c.Router)
	data, err := render.NftablesFirewall(c.Router, holes)
	if err != nil {
		return err
	}
	path := firstNonEmpty(c.NftablesPath, "/run/routerd/firewall.nft")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	previous, readErr := os.ReadFile(path)
	changed := readErr != nil || !bytes.Equal(previous, data)
	if changed {
		if err := os.WriteFile(path, data, 0644); err != nil {
			return err
		}
	}
	nft := firstNonEmpty(c.NftCommand, "nft")
	if changed {
		if err := checkNftablesRuleset(ctx, nft, path); err != nil {
			return err
		}
	}
	if changed && !c.DryRun {
		_ = exec.CommandContext(ctx, nft, "delete", "table", "inet", "routerd_filter").Run()
		out, err := exec.CommandContext(ctx, nft, "-f", path).CombinedOutput()
		if err != nil {
			return fmt.Errorf("nft -f %s: %w: %s", path, err, strings.TrimSpace(string(out)))
		}
	}
	status := map[string]any{
		"phase":         "Applied",
		"dryRun":        c.DryRun,
		"changed":       changed,
		"rules":         firewallRuleCount(c.Router),
		"internalHoles": len(holes),
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
		event.Attributes = map[string]string{"nftablesPath": path, "dryRun": fmt.Sprintf("%t", c.DryRun), "internalHoles": fmt.Sprintf("%d", len(holes))}
		_ = c.Bus.Publish(ctx, event)
	}
	return nil
}

func checkNftablesRuleset(ctx context.Context, nft, path string) error {
	out, err := exec.CommandContext(ctx, nft, "-c", "-f", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func hasFirewall(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.FirewallAPIVersion && (resource.Kind == "FirewallZone" || resource.Kind == "FirewallPolicy" || resource.Kind == "FirewallRule") {
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
