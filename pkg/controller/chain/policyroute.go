package chain

import (
	"bytes"
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

type IPv4PolicyRouteController struct {
	Router           *api.Router
	Bus              *bus.Bus
	Store            Store
	DryRun           bool
	NftCommand       string
	PolicyPath       string
	DefaultRoutePath string
	Logger           *slog.Logger
}

func (c IPv4PolicyRouteController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	nft := firstNonEmpty(c.NftCommand, "nft")
	policyPath := firstNonEmpty(c.PolicyPath, "/run/routerd/policy-route.nft")
	defaultRoutePath := firstNonEmpty(c.DefaultRoutePath, "/run/routerd/default-route.nft")
	aliases := c.aliases()
	routeSets := c.routeSets()

	if err := c.applyRouteTables(ctx, aliases); err != nil {
		return err
	}
	if err := c.applyPolicyNft(ctx, nft, policyPath); err != nil {
		return err
	}
	if err := c.applyDefaultRoutePolicies(ctx, nft, defaultRoutePath, routeSets); err != nil {
		return err
	}
	return nil
}

func (c IPv4PolicyRouteController) aliases() map[string]string {
	aliases := map[string]string{}
	for _, res := range c.Router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err == nil && spec.IfName != "" {
				aliases[res.Metadata.Name] = spec.IfName
			}
		case "PPPoEInterface":
			spec, err := res.PPPoEInterfaceSpec()
			if err == nil {
				aliases[res.Metadata.Name] = firstNonEmpty(spec.IfName, "ppp-"+res.Metadata.Name)
			}
		case "DSLiteTunnel":
			spec, err := res.DSLiteTunnelSpec()
			if err == nil {
				aliases[res.Metadata.Name] = firstNonEmpty(spec.TunnelName, res.Metadata.Name)
			}
		}
	}
	return aliases
}

func (c IPv4PolicyRouteController) routeSets() map[string]api.IPv4PolicyRouteSetSpec {
	out := map[string]api.IPv4PolicyRouteSetSpec{}
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "IPv4PolicyRouteSet" {
			continue
		}
		spec, err := res.IPv4PolicyRouteSetSpec()
		if err == nil {
			targets := spec.Targets[:0]
			for _, target := range spec.Targets {
				if c.targetHealthy(target.HealthCheck) {
					targets = append(targets, target)
				}
			}
			if len(targets) == 0 {
				continue
			}
			spec.Targets = targets
			out[res.Metadata.Name] = spec
		}
	}
	return out
}

func (c IPv4PolicyRouteController) applyRouteTables(ctx context.Context, aliases map[string]string) error {
	var failures []string
	applyTarget := func(owner string, target api.IPv4PolicyRouteTarget, skipMissing bool) {
		if !c.targetHealthy(target.HealthCheck) {
			return
		}
		c.applyRouteTarget(ctx, aliases, owner, target.Name, target.OutboundInterface, target.Table, target.Priority, target.Mark, target.RouteMetric, "none", "", skipMissing, &failures)
	}
	applyCandidate := func(owner string, candidate api.IPv4DefaultRoutePolicyCandidate) {
		c.applyRouteTarget(ctx, aliases, owner, firstNonEmpty(candidate.Name, candidate.Interface), candidate.Interface, candidate.Table, candidate.Priority, candidate.Mark, candidate.RouteMetric, firstNonEmpty(candidate.GatewaySource, "none"), candidate.Gateway, false, &failures)
	}
	for _, res := range c.Router.Spec.Resources {
		switch res.Kind {
		case "IPv4PolicyRoute":
			spec, err := res.IPv4PolicyRouteSpec()
			if err != nil {
				failures = append(failures, err.Error())
				continue
			}
			applyTarget(res.ID(), api.IPv4PolicyRouteTarget{Name: res.Metadata.Name, OutboundInterface: spec.OutboundInterface, Table: spec.Table, Priority: spec.Priority, Mark: spec.Mark, RouteMetric: spec.RouteMetric}, false)
		case "IPv4PolicyRouteSet":
			spec, err := res.IPv4PolicyRouteSetSpec()
			if err != nil {
				failures = append(failures, err.Error())
				continue
			}
			for i, target := range spec.Targets {
				if target.Name == "" {
					target.Name = fmt.Sprintf("%s-%d", res.Metadata.Name, i)
				}
				applyTarget(res.ID(), target, true)
			}
		case "IPv4DefaultRoutePolicy":
			spec, err := res.IPv4DefaultRoutePolicySpec()
			if err != nil {
				failures = append(failures, err.Error())
				continue
			}
			for _, candidate := range spec.Candidates {
				if candidate.RouteSet != "" {
					continue
				}
				applyCandidate(res.ID(), candidate)
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func (c IPv4PolicyRouteController) applyRouteTarget(ctx context.Context, aliases map[string]string, owner, name, outboundInterface string, table, priority, mark, routeMetric int, gatewaySource, gateway string, skipMissing bool, failures *[]string) {
	ifname := aliases[outboundInterface]
	if ifname == "" {
		*failures = append(*failures, fmt.Sprintf("%s references missing outbound interface %q", owner, outboundInterface))
		return
	}
	if !c.linkExists(ctx, ifname) {
		if skipMissing {
			return
		}
		*failures = append(*failures, fmt.Sprintf("%s outbound interface %s does not exist", owner, ifname))
		return
	}
	metric := routeMetric
	if metric == 0 {
		metric = 50
	}
	if !c.DryRun {
		args := []string{"-4", "route", "replace", "default"}
		switch gatewaySource {
		case "", "none":
			args = append(args, "dev", ifname)
		case "static":
			args = append(args, "via", gateway, "dev", ifname)
		default:
			*failures = append(*failures, fmt.Sprintf("%s unsupported gatewaySource %q", owner, gatewaySource))
			return
		}
		args = append(args, "table", fmt.Sprintf("%d", table), "metric", fmt.Sprintf("%d", metric))
		if out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput(); err != nil {
			*failures = append(*failures, fmt.Sprintf("%s route table %d: %v: %s", owner, table, err, strings.TrimSpace(string(out))))
			return
		}
		if err := c.ensureFwmarkRule(ctx, priority, mark, table); err != nil {
			*failures = append(*failures, fmt.Sprintf("%s fwmark rule: %v", owner, err))
			return
		}
	}
	name = firstNonEmpty(name, outboundInterface)
	_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4PolicyRoute", name, map[string]any{
		"phase":     "Installed",
		"device":    ifname,
		"gateway":   gateway,
		"table":     table,
		"mark":      fmt.Sprintf("0x%x", mark),
		"priority":  priority,
		"metric":    metric,
		"dryRun":    c.DryRun,
		"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (c IPv4PolicyRouteController) applyPolicyNft(ctx context.Context, nft, path string) error {
	data, err := render.NftablesIPv4PolicyRoutes(c.effectivePolicyRouteRouter())
	if err != nil {
		return err
	}
	return c.applyNftTable(ctx, nft, path, "ip", "routerd_policy", data)
}

func (c IPv4PolicyRouteController) applyDefaultRoutePolicies(ctx context.Context, nft, path string, routeSets map[string]api.IPv4PolicyRouteSetSpec) error {
	var chunks [][]byte
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "IPv4DefaultRoutePolicy" {
			continue
		}
		spec, err := res.IPv4DefaultRoutePolicySpec()
		if err != nil {
			return err
		}
		healthy := c.availableDefaultRouteCandidates(spec, routeSets)
		active, ok := selectDefaultRouteCandidate(healthy)
		if !ok {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4DefaultRoutePolicy", res.Metadata.Name, map[string]any{"phase": "Pending", "reason": "NoReadyCandidates", "dryRun": c.DryRun})
			continue
		}
		data, err := render.NftablesIPv4DefaultRoutePolicy(res.ID(), spec, active, healthy, routeSets)
		if err != nil {
			return err
		}
		chunks = append(chunks, data)
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4DefaultRoutePolicy", res.Metadata.Name, map[string]any{
			"phase":             "Applied",
			"selectedCandidate": firstNonEmpty(active.Name, active.Interface, active.RouteSet),
			"selectedRouteSet":  active.RouteSet,
			"selectedInterface": active.Interface,
			"dryRun":            c.DryRun,
			"updatedAt":         time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	return c.applyNftTable(ctx, nft, path, "ip", "routerd_default_route", bytes.Join(chunks, []byte("\n")))
}

func (c IPv4PolicyRouteController) availableDefaultRouteCandidates(spec api.IPv4DefaultRoutePolicySpec, routeSets map[string]api.IPv4PolicyRouteSetSpec) []api.IPv4DefaultRoutePolicyCandidate {
	var out []api.IPv4DefaultRoutePolicyCandidate
	aliases := c.aliases()
	for _, candidate := range spec.Candidates {
		if candidate.HealthCheck != "" {
			status := c.Store.ObjectStatus(api.NetAPIVersion, "HealthCheck", candidate.HealthCheck)
			if fmt.Sprint(status["phase"]) != "Healthy" {
				continue
			}
		}
		if candidate.RouteSet != "" {
			routeSet, ok := routeSets[candidate.RouteSet]
			if !ok {
				continue
			}
			for _, target := range routeSet.Targets {
				if !c.targetHealthy(target.HealthCheck) {
					continue
				}
				if ifname := aliases[target.OutboundInterface]; ifname != "" && c.linkExists(context.Background(), ifname) {
					out = append(out, candidate)
					break
				}
			}
			continue
		}
		if ifname := aliases[candidate.Interface]; ifname != "" && c.linkExists(context.Background(), ifname) {
			out = append(out, candidate)
		}
	}
	return out
}

func (c IPv4PolicyRouteController) effectivePolicyRouteRouter() *api.Router {
	if c.Router == nil {
		return nil
	}
	out := *c.Router
	out.Spec.Resources = make([]api.Resource, 0, len(c.Router.Spec.Resources))
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "IPv4PolicyRouteSet" {
			out.Spec.Resources = append(out.Spec.Resources, res)
			continue
		}
		spec, err := res.IPv4PolicyRouteSetSpec()
		if err != nil {
			out.Spec.Resources = append(out.Spec.Resources, res)
			continue
		}
		targets := spec.Targets[:0]
		for _, target := range spec.Targets {
			if c.targetHealthy(target.HealthCheck) {
				targets = append(targets, target)
			}
		}
		if len(targets) == 0 {
			continue
		}
		spec.Targets = targets
		res.Spec = spec
		out.Spec.Resources = append(out.Spec.Resources, res)
	}
	return &out
}

func (c IPv4PolicyRouteController) targetHealthy(name string) bool {
	if name == "" {
		return true
	}
	status := c.Store.ObjectStatus(api.NetAPIVersion, "HealthCheck", name)
	return fmt.Sprint(status["phase"]) == "Healthy"
}

func selectDefaultRouteCandidate(candidates []api.IPv4DefaultRoutePolicyCandidate) (api.IPv4DefaultRoutePolicyCandidate, bool) {
	if len(candidates) == 0 {
		return api.IPv4DefaultRoutePolicyCandidate{}, false
	}
	ordered := append([]api.IPv4DefaultRoutePolicyCandidate{}, candidates...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })
	return ordered[0], true
}

func (c IPv4PolicyRouteController) applyNftTable(ctx context.Context, nft, path, family, table string, data []byte) error {
	if len(data) == 0 {
		if c.DryRun {
			return nil
		}
		_ = exec.CommandContext(ctx, nft, "delete", "table", family, table).Run()
		return nil
	}
	if c.DryRun {
		return nil
	}
	changed, err := writeFileIfChanged(path, data, 0644, false)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, nft, "-c", "-f", path).CombinedOutput(); err != nil {
		return fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if !changed && exec.CommandContext(ctx, nft, "list", "table", family, table).Run() == nil {
		return nil
	}
	_ = exec.CommandContext(ctx, nft, "delete", "table", family, table).Run()
	if out, err := exec.CommandContext(ctx, nft, "-f", path).CombinedOutput(); err != nil {
		return fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ipv4.policy_route.applied", daemonapi.SeverityInfo)
		event.Attributes = map[string]string{"table": table, "path": path}
		_ = c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c IPv4PolicyRouteController) ensureFwmarkRule(ctx context.Context, priority, mark, table int) error {
	priorityText := fmt.Sprintf("%d", priority)
	markText := fmt.Sprintf("0x%x", mark)
	tableText := fmt.Sprintf("%d", table)
	if out, err := exec.CommandContext(ctx, "ip", "-4", "rule", "show", "priority", priorityText).CombinedOutput(); err == nil {
		line := string(out)
		if strings.Contains(line, "fwmark "+markText) && strings.Contains(line, "lookup "+tableText) {
			return nil
		}
	}
	for {
		out, err := exec.CommandContext(ctx, "ip", "-4", "rule", "show", "priority", priorityText).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			break
		}
		if err := exec.CommandContext(ctx, "ip", "-4", "rule", "del", "priority", priorityText).Run(); err != nil {
			break
		}
	}
	if out, err := exec.CommandContext(ctx, "ip", "-4", "rule", "add", "priority", priorityText, "fwmark", markText, "table", tableText).CombinedOutput(); err != nil {
		return fmt.Errorf("ip -4 rule add priority %s fwmark %s table %s: %w: %s", priorityText, markText, tableText, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c IPv4PolicyRouteController) linkExists(ctx context.Context, ifname string) bool {
	return exec.CommandContext(ctx, "ip", "link", "show", "dev", ifname).Run() == nil
}
