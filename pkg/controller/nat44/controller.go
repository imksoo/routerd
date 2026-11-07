// SPDX-License-Identifier: BSD-3-Clause

package nat44

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

const TableAppliedEvent = "routerd.nat44.table.applied"

type Controller struct {
	Router       *api.Router
	Bus          *bus.Bus
	Store        Store
	DryRun       bool
	IngressLive  bool
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
	renderRouter := c.effectiveIngressRouter()
	applyLive := !c.DryRun || c.IngressLive
	applyRouter := renderRouter
	renderRules := rules
	if applyLive {
		applyRouter = c.applyRouter(renderRouter)
		if c.DryRun {
			renderRules = nil
		}
	}
	if platform.CurrentOS() == platform.OSFreeBSD {
		if len(rules) == 0 {
			if !applyLive {
				return nil
			}
			return c.clearPF(ctx)
		}
		return c.reconcilePF(ctx, rules)
	}
	data, err := render.NftablesNAT44RulesForRouter(applyRouter, renderRules)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		if !c.DryRun && c.IngressLive {
			return c.removeUnusedNATTables(ctx, data)
		}
		return nil
	}
	path := stringutil.FirstNonEmpty(c.NftablesPath, "/run/routerd/nat44.nft")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	changed := true
	if current, err := os.ReadFile(path); err == nil && string(current) == string(data) {
		changed = false
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !applyLive {
		if changed {
			if err := os.WriteFile(path, data, 0644); err != nil {
				return err
			}
		}
		return c.saveRuleStatuses(ctx, rules, path, changed, false)
	}
	finish := func(changed, missing bool) error {
		if !c.DryRun && c.IngressLive {
			if err := c.removeUnusedNATTables(ctx, data); err != nil {
				return err
			}
		}
		return c.saveRuleStatuses(ctx, rules, path, changed, missing)
	}
	nft := stringutil.FirstNonEmpty(c.NftCommand, "nft")
	ipMissing := strings.Contains(string(data), "table ip routerd_nat {") && !nftTableExists(ctx, nft, "ip", "routerd_nat")
	ip6Missing := strings.Contains(string(data), "table ip6 routerd_nat {") && !nftTableExists(ctx, nft, "ip6", "routerd_nat")
	missing := ipMissing || ip6Missing
	if !changed && !missing {
		return finish(false, false)
	}
	applyPath := path
	applyData := data
	partial := false
	if !changed && ipMissing != ip6Missing {
		if split := strings.Index(string(data), "add table ip6 routerd_nat\n"); split > 0 {
			if ipMissing {
				applyData = data[:split]
			} else {
				applyData = data[split:]
			}
			partial = true
		}
	}
	if changed || partial {
		tmp, err := os.CreateTemp(filepath.Dir(path), ".nat44-*.nft")
		if err != nil {
			return err
		}
		applyPath = tmp.Name()
		defer os.Remove(applyPath)
		if err := tmp.Chmod(0644); err != nil {
			tmp.Close()
			return err
		}
		if _, err := tmp.Write(applyData); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
	}
	if err := checkNftablesRuleset(ctx, nft, applyPath); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, nft, "-f", applyPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft -f %s: %w: %s", applyPath, err, strings.TrimSpace(string(out)))
	}
	if changed {
		if err := os.Rename(applyPath, path); err != nil {
			return err
		}
	}
	var notifyErr error
	if c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, TableAppliedEvent, daemonapi.SeverityInfo)
		event.Attributes = map[string]string{"nftablesPath": path}
		notifyErr = c.Bus.Publish(ctx, event)
	}
	return errors.Join(finish(changed, missing), notifyErr)
}

func (c Controller) removeUnusedNATTables(ctx context.Context, data []byte) error {
	nft := stringutil.FirstNonEmpty(c.NftCommand, "nft")
	out, err := exec.CommandContext(ctx, nft, "list", "tables").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s list tables: %w: %s", nft, err, strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "table" || fields[2] != "routerd_nat" || (fields[1] != "ip" && fields[1] != "ip6") {
			continue
		}
		if strings.Contains(string(data), "table "+fields[1]+" routerd_nat {") {
			continue
		}
		if result, err := exec.CommandContext(ctx, nft, "delete", "table", fields[1], "routerd_nat").CombinedOutput(); err != nil {
			return fmt.Errorf("delete nft table %s routerd_nat: %w: %s", fields[1], err, strings.TrimSpace(string(result)))
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
	pfctl := stringutil.FirstNonEmpty(c.NftCommand, "pfctl")
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
	pfctl := stringutil.FirstNonEmpty(c.NftCommand, "pfctl")
	if c.NftCommand == "" || c.NftCommand == "nft" {
		pfctl = "pfctl"
	}
	out, err := exec.CommandContext(ctx, pfctl, "-a", "routerd_nat", "-F", "rules").CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if strings.Contains(text, "/dev/pf") || strings.Contains(text, "No such file or directory") {
			return nil
		}
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
	value := statusvalue.Address(resourcequery.Value(c.Store, spec.SNATAddressFrom))
	if value == "" {
		value = statusvalue.Address(addressFromRouterResource(c.Router, spec.SNATAddressFrom))
	}
	if value == "" {
		return "", "SNATAddressSourcePending"
	}
	return value, ""
}

func (c Controller) effectiveIngressRouter() *api.Router {
	if c.Router == nil || c.Store == nil {
		return c.Router
	}
	next := *c.Router
	next.Spec.Resources = append([]api.Resource(nil), c.Router.Spec.Resources...)
	for i, resource := range next.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "IngressService" {
			continue
		}
		spec, err := resource.IngressServiceSpec()
		if err != nil {
			continue
		}
		status := c.Store.ObjectStatus(api.FirewallAPIVersion, "IngressService", resource.Metadata.Name)
		if backends, selection := activeBackends(status); len(backends) > 0 {
			spec.Backends = backends
			spec.Policy.Selection = selection
			next.Spec.Resources[i].Spec = spec
			continue
		}
		name, address, port := activeBackend(status["activeBackend"])
		if address == "" || port == 0 {
			continue
		}
		spec.Backends = []api.IngressBackendSpec{{Name: name, Address: address, Port: port}}
		spec.Policy.Selection = "failover"
		next.Spec.Resources[i].Spec = spec
	}
	return &next
}

func (c Controller) applyRouter(router *api.Router) *api.Router {
	if router == nil {
		return nil
	}
	includeNAT := !c.DryRun
	includeIngress := c.IngressLive
	if includeNAT && includeIngress {
		return router
	}
	next := *router
	next.Spec.Resources = make([]api.Resource, 0, len(router.Spec.Resources))
	for _, resource := range router.Spec.Resources {
		if nat44ControllerResource(resource) && !includeNAT {
			continue
		}
		if resource.APIVersion == api.FirewallAPIVersion && resource.Kind == "IngressService" && !includeIngress {
			continue
		}
		next.Spec.Resources = append(next.Spec.Resources, resource)
	}
	return &next
}

func nat44ControllerResource(resource api.Resource) bool {
	if resource.Kind == "NAT44Rule" || resource.Kind == "NAT44FlowDNATPinhole" {
		return true
	}
	return resource.APIVersion == api.FirewallAPIVersion && resource.Kind == "LocalServiceRedirect"
}

func activeBackends(status map[string]any) ([]api.IngressBackendSpec, string) {
	selection := strings.TrimSpace(fmt.Sprint(status["effectiveSelection"]))
	if selection != "sourceHash" && selection != "random" {
		return nil, ""
	}
	var values []any
	switch typed := status["activeBackends"].(type) {
	case []any:
		values = typed
	case []map[string]any:
		values = make([]any, 0, len(typed))
		for _, item := range typed {
			values = append(values, item)
		}
	default:
		return nil, ""
	}
	var out []api.IngressBackendSpec
	for _, value := range values {
		name, address, port := activeBackend(value)
		if address == "" || port == 0 {
			continue
		}
		out = append(out, api.IngressBackendSpec{Name: name, Address: address, Port: port})
	}
	if len(out) < 2 {
		return nil, ""
	}
	return out, selection
}

func activeBackend(value any) (string, string, int) {
	item, ok := value.(map[string]any)
	if !ok {
		return "", "", 0
	}
	name := strings.TrimSpace(fmt.Sprint(item["name"]))
	address := strings.TrimSpace(fmt.Sprint(item["address"]))
	port := 0
	switch typed := item["port"].(type) {
	case int:
		port = typed
	case int64:
		port = int(typed)
	case float64:
		port = int(typed)
	}
	return name, address, port
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
				aliases[resource.Metadata.Name] = stringutil.FirstNonEmpty(spec.TunnelName, resource.Metadata.Name)
			}
		case "PPPoESession":
			spec, err := resource.PPPoESessionSpec()
			if err == nil {
				aliases[resource.Metadata.Name] = stringutil.FirstNonEmpty(spec.IfName, "ppp-"+resource.Metadata.Name)
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
