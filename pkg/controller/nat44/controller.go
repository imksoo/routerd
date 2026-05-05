package nat44

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.lan.route.changed"}}, 16)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		_ = c.reconcileLogged(ctx)
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					return
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
			c.Logger.Warn("nat44 reconcile failed", "error", err)
		}
		return err
	}
	return nil
}

func (c Controller) Reconcile(ctx context.Context) error {
	var rules []render.NAT44RenderRule
	aliases := interfaceAliases(c.Router)
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "NAT44Rule" {
			continue
		}
		spec, err := resource.NAT44RuleSpec()
		if err != nil {
			return err
		}
		ifname, reason := c.resolveEgressInterface(spec, aliases)
		if ifname == "" {
			status := map[string]any{
				"phase":      "Pending",
				"reason":     reason,
				"conditions": []map[string]any{{"type": "EgressResolved", "status": "False", "reason": reason}},
				"dryRun":     c.DryRun,
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "NAT44Rule", resource.Metadata.Name, status); err != nil {
				return err
			}
			continue
		}
		rules = append(rules, render.NAT44RenderRule{
			Name:                    resource.Metadata.Name,
			Type:                    spec.Type,
			EgressInterface:         ifname,
			SourceRanges:            spec.SourceRanges,
			DestinationCIDRs:        spec.DestinationCIDRs,
			ExcludeDestinationCIDRs: spec.ExcludeDestinationCIDRs,
			SNATAddress:             spec.SNATAddress,
		})
	}
	if len(rules) == 0 {
		return nil
	}
	data, err := render.NftablesNAT44Rules(rules)
	if err != nil {
		return err
	}
	path := firstNonEmpty(c.NftablesPath, "/run/routerd/nat44.nft")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	changed := true
	if current, err := os.ReadFile(path); err == nil && string(current) == string(data) {
		changed = false
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
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
		_ = exec.CommandContext(ctx, nft, "delete", "table", "ip", "routerd_nat").Run()
		cmd := exec.CommandContext(ctx, nft, "-f", path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("nft -f %s: %w: %s", path, err, strings.TrimSpace(string(out)))
		}
	}
	if !changed && !c.DryRun && !nat44TableExists(ctx, nft) {
		if err := checkNftablesRuleset(ctx, nft, path); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, nft, "-f", path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("nft -f %s: %w: %s", path, err, strings.TrimSpace(string(out)))
		}
	}
	for _, rule := range rules {
		status := map[string]any{
			"phase":                   "Active",
			"activeEgressInterface":   rule.EgressInterface,
			"sourceRanges":            rule.SourceRanges,
			"destinationCIDRs":        rule.DestinationCIDRs,
			"excludeDestinationCIDRs": rule.ExcludeDestinationCIDRs,
			"nftablesPath":            path,
			"dryRun":                  c.DryRun,
			"conditions":              []map[string]any{{"type": "Applied", "status": "True", "reason": "Rendered"}},
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "NAT44Rule", rule.Name, status); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.nat44.rule.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule", Name: rule.Name}
			event.Attributes = map[string]string{"egressInterface": rule.EgressInterface, "nftablesPath": path, "dryRun": fmt.Sprintf("%t", c.DryRun)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func nat44TableExists(ctx context.Context, nft string) bool {
	return exec.CommandContext(ctx, nft, "list", "table", "ip", "routerd_nat").Run() == nil
}

func checkNftablesRuleset(ctx context.Context, nft, path string) error {
	out, err := exec.CommandContext(ctx, nft, "-c", "-f", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c Controller) resolveEgressInterface(spec api.NAT44RuleSpec, aliases map[string]string) (string, string) {
	if spec.EgressInterface != "" {
		if ifname := aliases[spec.EgressInterface]; ifname != "" {
			return ifname, ""
		}
		return spec.EgressInterface, ""
	}
	policyName := spec.EgressPolicyRef
	if policyName == "" {
		policies := policyNames(c.Router)
		if len(policies) == 1 {
			policyName = policies[0]
		}
	}
	if policyName == "" {
		return "", "EgressPolicyMissing"
	}
	status := c.Store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", policyName)
	if fmt.Sprint(status["phase"]) != "Applied" {
		return "", "EgressPolicyNotApplied"
	}
	if selected, _ := status["selectedDevice"].(string); selected != "" {
		return selected, ""
	}
	return "", "SelectedDeviceMissing"
}

func interfaceAliases(router *api.Router) map[string]string {
	aliases := map[string]string{}
	if router == nil {
		return aliases
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "Interface":
			spec, err := resource.InterfaceSpec()
			if err == nil && spec.IfName != "" {
				aliases[resource.Metadata.Name] = spec.IfName
			}
		case "DSLiteTunnel":
			spec, err := resource.DSLiteTunnelSpec()
			if err == nil {
				aliases[resource.Metadata.Name] = firstNonEmpty(spec.TunnelName, resource.Metadata.Name)
			}
		case "WireGuardInterface", "VRF", "VXLANTunnel", "Bridge":
			aliases[resource.Metadata.Name] = resource.Metadata.Name
		}
	}
	return aliases
}

func policyNames(router *api.Router) []string {
	if router == nil {
		return nil
	}
	var out []string
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "EgressRoutePolicy" {
			out = append(out, resource.Metadata.Name)
		}
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
