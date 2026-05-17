// SPDX-License-Identifier: BSD-3-Clause

package nat44

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/platform"
	"routerd/pkg/render"
	"routerd/pkg/resourcequery"
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
		snatAddress, snatReason := c.resolveSNATAddress(spec)
		if spec.Type == "snat" && snatAddress == "" {
			status := map[string]any{
				"phase":      "Pending",
				"reason":     snatReason,
				"conditions": []map[string]any{{"type": "SNATAddressResolved", "status": "False", "reason": snatReason}},
				"dryRun":     c.DryRun,
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "NAT44Rule", resource.Metadata.Name, status); err != nil {
				return err
			}
			continue
		}
		rules = append(rules, render.NAT44RenderRule{
			Name:                      resource.Metadata.Name,
			Type:                      spec.Type,
			EgressInterface:           ifname,
			SourceRanges:              spec.SourceRanges,
			DestinationCIDRs:          spec.DestinationCIDRs,
			DestinationSetRefs:        spec.DestinationSetRefs,
			ExcludeDestinationCIDRs:   spec.ExcludeDestinationCIDRs,
			ExcludeDestinationSetRefs: spec.ExcludeDestinationSetRefs,
			SNATAddress:               snatAddress,
		})
	}
	if platform.CurrentOS() == platform.OSFreeBSD {
		if len(rules) == 0 {
			if c.DryRun {
				return nil
			}
			return c.clearPF(ctx)
		}
		return c.reconcilePF(ctx, rules)
	}
	data, err := render.NftablesNAT44RulesForRouter(c.Router, rules)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		if c.DryRun {
			return nil
		}
		return c.clearNftables(ctx)
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
	if c.DryRun {
		return c.saveRuleStatuses(ctx, rules, path, changed, false)
	}
	nft := firstNonEmpty(c.NftCommand, "nft")
	missing := (strings.Contains(string(data), "table ip routerd_nat") && !nftTableExists(ctx, nft, "ip", "routerd_nat")) ||
		(strings.Contains(string(data), "table ip6 routerd_nat") && !nftTableExists(ctx, nft, "ip6", "routerd_nat"))
	if !changed && !missing {
		return c.saveRuleStatuses(ctx, rules, path, false, false)
	}
	if err := checkNftablesRuleset(ctx, nft, path); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, nft, "-f", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft -f %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return c.saveRuleStatuses(ctx, rules, path, changed, missing)
}

func (c Controller) clearNftables(ctx context.Context) error {
	nft := firstNonEmpty(c.NftCommand, "nft")
	for _, family := range []string{"ip", "ip6"} {
		if !nftTableExists(ctx, nft, family, "routerd_nat") {
			continue
		}
		out, err := exec.CommandContext(ctx, nft, "delete", "table", family, "routerd_nat").CombinedOutput()
		if err != nil {
			return fmt.Errorf("nft delete table %s routerd_nat: %w: %s", family, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func (c Controller) reconcilePF(ctx context.Context, rules []render.NAT44RenderRule) error {
	data, err := render.PFNAT44Rules(rules)
	if err != nil {
		return err
	}
	defaults, _ := platform.Current()
	path := filepath.Join(defaults.RuntimeDir, "nat44.pf")
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
	if c.DryRun {
		return c.saveRuleStatuses(ctx, rules, path, changed, false)
	}
	pfctl := firstNonEmpty(c.NftCommand, "pfctl")
	if c.NftCommand == "" || c.NftCommand == "nft" {
		pfctl = "pfctl"
	}
	if out, err := exec.CommandContext(ctx, pfctl, "-n", "-a", "routerd_nat", "-f", path).CombinedOutput(); err != nil {
		return fmt.Errorf("%s -n -a routerd_nat -f %s: %w: %s", pfctl, path, err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.CommandContext(ctx, pfctl, "-a", "routerd_nat", "-f", path).CombinedOutput(); err != nil {
		return fmt.Errorf("%s -a routerd_nat -f %s: %w: %s", pfctl, path, err, strings.TrimSpace(string(out)))
	}
	return c.saveRuleStatuses(ctx, rules, path, changed, false)
}

func (c Controller) clearPF(ctx context.Context) error {
	pfctl := firstNonEmpty(c.NftCommand, "pfctl")
	if c.NftCommand == "" || c.NftCommand == "nft" {
		pfctl = "pfctl"
	}
	out, err := exec.CommandContext(ctx, pfctl, "-a", "routerd_nat", "-F", "rules").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s -a routerd_nat -F rules: %w: %s", pfctl, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c Controller) saveRuleStatuses(ctx context.Context, rules []render.NAT44RenderRule, path string, changed, missing bool) error {
	for _, rule := range rules {
		status := map[string]any{
			"phase":                   "Active",
			"activeEgressInterface":   rule.EgressInterface,
			"snatAddress":             rule.SNATAddress,
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
		if (changed || missing) && c.Bus != nil {
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
	return nftTableExists(ctx, nft, "ip", "routerd_nat")
}

func nftTableExists(ctx context.Context, nft, family, name string) bool {
	return exec.CommandContext(ctx, nft, "list", "table", family, name).Run() == nil
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

func (c Controller) resolveSNATAddress(spec api.NAT44RuleSpec) (string, string) {
	if spec.Type != "snat" {
		return "", ""
	}
	if strings.TrimSpace(spec.SNATAddress) != "" {
		return strings.TrimSpace(spec.SNATAddress), ""
	}
	if strings.TrimSpace(spec.SNATAddressFrom.Resource) == "" {
		return "", "SNATAddressMissing"
	}
	value := statusAddressValue(resourcequery.Value(c.Store, spec.SNATAddressFrom))
	if value == "" {
		value = statusAddressValue(addressFromRouterResource(c.Router, spec.SNATAddressFrom))
	}
	if value == "" {
		return "", "SNATAddressSourcePending"
	}
	return value, ""
}

func statusAddressValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	return value
}

func addressFromRouterResource(router *api.Router, source api.StatusValueSourceSpec) string {
	if router == nil || strings.TrimSpace(source.Resource) == "" {
		return ""
	}
	kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
	if !ok || kind == "" || name == "" {
		return ""
	}
	field := strings.TrimSpace(source.Field)
	if field == "" {
		field = "address"
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != kind || res.Metadata.Name != name {
			continue
		}
		switch kind {
		case "IPv4StaticAddress":
			if field != "address" {
				return ""
			}
			spec, err := res.IPv4StaticAddressSpec()
			if err != nil {
				return ""
			}
			return spec.Address
		default:
			return ""
		}
	}
	return ""
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
		case "PPPoEInterface":
			spec, err := resource.PPPoEInterfaceSpec()
			if err == nil {
				aliases[resource.Metadata.Name] = firstNonEmpty(spec.IfName, "ppp-"+resource.Metadata.Name)
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
