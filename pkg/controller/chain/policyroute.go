// SPDX-License-Identifier: BSD-3-Clause

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
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/healthcheck"
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
	activeTargetCandidates := c.activeTargetCandidates()

	if err := c.applyRouteTables(ctx, aliases); err != nil {
		return err
	}
	if err := c.applyPolicyNft(ctx, nft, policyPath, activeTargetCandidates); err != nil {
		return err
	}
	if err := c.applyDefaultRoutePolicies(ctx, nft, defaultRoutePath); err != nil {
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
		case "PPPoESession":
			spec, err := res.PPPoESessionSpec()
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

func (c IPv4PolicyRouteController) activeTargetCandidates() map[string]bool {
	active := map[string]bool{}
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "EgressRoutePolicy" {
			continue
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil || firstNonEmpty(spec.Mode, "") != "priority" {
			continue
		}
		healthy := c.availableDefaultRouteCandidates(spec)
		candidate, ok := selectDefaultRouteCandidate(healthy)
		if ok && len(candidate.Targets) > 0 {
			active[egressCandidateKey(res.Metadata.Name, candidate)] = true
		}
	}
	return active
}

func (c IPv4PolicyRouteController) applyRouteTables(ctx context.Context, aliases map[string]string) error {
	var failures []string
	applyTarget := func(owner string, target api.EgressRoutePolicyTarget, skipMissing bool) {
		if !c.shouldInstallPolicyRouteForHealthCheck(target.HealthCheck, target.Mark) {
			return
		}
		c.applyRouteTarget(ctx, aliases, owner, target.Name, target.EffectiveInterface(), target.EffectiveTable(), target.Priority, target.Mark, target.EffectiveMetric(), "none", "", skipMissing, &failures)
	}
	applyCandidate := func(owner string, candidate api.EgressRoutePolicyCandidate) {
		if !c.shouldInstallPolicyRouteForHealthCheck(candidate.HealthCheck, candidate.Mark) {
			return
		}
		c.applyRouteTarget(ctx, aliases, owner, firstNonEmpty(candidate.Name, candidate.EffectiveInterface()), candidate.EffectiveInterface(), candidate.EffectiveTable(), candidate.Priority, candidate.Mark, candidate.EffectiveMetric(), firstNonEmpty(candidate.GatewaySource, "none"), candidate.Gateway, false, &failures)
	}
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "EgressRoutePolicy" {
			continue
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		switch firstNonEmpty(spec.Mode, "") {
		case "priority", "mark", "hash":
		default:
			continue
		}
		for _, candidate := range spec.Candidates {
			if len(candidate.Targets) > 0 {
				for i, target := range candidate.Targets {
					if target.Name == "" {
						target.Name = fmt.Sprintf("%s-%d", firstNonEmpty(candidate.Name, res.Metadata.Name), i)
					}
					applyTarget(res.ID(), target, true)
				}
				continue
			}
			if candidate.Mark == 0 {
				continue
			}
			applyCandidate(res.ID(), candidate)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func (c IPv4PolicyRouteController) shouldInstallPolicyRouteForHealthCheck(name string, mark int) bool {
	if c.targetHealthy(name) {
		return true
	}
	return c.healthCheckUsesFwMark(name, mark)
}

func (c IPv4PolicyRouteController) healthCheckUsesFwMark(name string, mark int) bool {
	if name == "" || mark == 0 || c.Router == nil {
		return false
	}
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "HealthCheck" || res.Metadata.Name != name {
			continue
		}
		spec, err := res.HealthCheckSpec()
		if err != nil || healthCheckDisabled(spec) {
			return false
		}
		return healthcheck.ResolveSpecForResource(c.Router, name, spec).FwMark == mark
	}
	return false
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
		resolvedGateway, err := c.routeGateway(ctx, ifname, gatewaySource, gateway)
		if err != nil {
			*failures = append(*failures, fmt.Sprintf("%s route gateway: %v", owner, err))
			return
		}
		gateway = resolvedGateway
		if !c.defaultRouteMatches(ctx, ifname, table, metric, gatewaySource, gateway) {
			args := []string{"-4", "route", "replace", "default"}
			switch gatewaySource {
			case "", "none":
				args = append(args, "dev", ifname)
			case "static", "dhcpv4":
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
		}
		if err := c.ensureFwmarkRule(ctx, priority, mark, table); err != nil {
			*failures = append(*failures, fmt.Sprintf("%s fwmark rule: %v", owner, err))
			return
		}
	}
	name = firstNonEmpty(name, outboundInterface)
	_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", name, map[string]any{
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

func (c IPv4PolicyRouteController) routeGateway(ctx context.Context, ifname, gatewaySource, gateway string) (string, error) {
	switch gatewaySource {
	case "", "none":
		return "", nil
	case "static":
		if strings.TrimSpace(gateway) == "" {
			return "", fmt.Errorf("static gateway is empty for %s", ifname)
		}
		return gateway, nil
	case "dhcpv4":
		if strings.TrimSpace(gateway) != "" {
			return gateway, nil
		}
		return currentIPv4DefaultGatewayForInterface(ctx, ifname)
	default:
		return "", fmt.Errorf("unsupported gatewaySource %q", gatewaySource)
	}
}

func currentIPv4DefaultGatewayForInterface(ctx context.Context, ifname string) (string, error) {
	out, err := exec.CommandContext(ctx, "ip", "-4", "route", "show", "default", "dev", ifname).CombinedOutput()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	for i, field := range fields {
		if field == "via" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("no gateway found for %s", ifname)
}

func (c IPv4PolicyRouteController) defaultRouteMatches(ctx context.Context, ifname string, table, metric int, gatewaySource, gateway string) bool {
	out, err := exec.CommandContext(ctx, "ip", "-4", "route", "show", "default", "table", fmt.Sprintf("%d", table)).CombinedOutput()
	if err != nil {
		return false
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return false
	}
	for _, candidate := range strings.Split(line, "\n") {
		fields := strings.Fields(candidate)
		if len(fields) == 0 || fields[0] != "default" {
			continue
		}
		if !fieldValueMatches(fields, "dev", ifname) {
			continue
		}
		if !fieldValueMatches(fields, "metric", fmt.Sprintf("%d", metric)) {
			continue
		}
		switch gatewaySource {
		case "", "none":
			if hasField(fields, "via") {
				continue
			}
		case "static", "dhcpv4":
			if !fieldValueMatches(fields, "via", gateway) {
				continue
			}
		default:
			return false
		}
		return true
	}
	return false
}

func fieldValueMatches(fields []string, key, value string) bool {
	for i, field := range fields {
		if field == key && i+1 < len(fields) {
			return fields[i+1] == value
		}
	}
	return false
}

func hasField(fields []string, key string) bool {
	for _, field := range fields {
		if field == key {
			return true
		}
	}
	return false
}

func (c IPv4PolicyRouteController) applyPolicyNft(ctx context.Context, nft, path string, activeTargetCandidates map[string]bool) error {
	data, err := render.NftablesIPv4PolicyRoutes(c.effectivePolicyRouteRouter(activeTargetCandidates))
	if err != nil {
		return err
	}
	return c.applyNftTable(ctx, nft, path, "ip", "routerd_policy", data)
}

func (c IPv4PolicyRouteController) applyDefaultRoutePolicies(ctx context.Context, nft, path string) error {
	var chunks [][]byte
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "EgressRoutePolicy" {
			continue
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil || firstNonEmpty(spec.Mode, "") != "priority" {
			if err != nil {
				return err
			}
			continue
		}
		healthy := c.availableDefaultRouteCandidates(spec)
		active, ok := selectDefaultRouteCandidate(healthy)
		if !ok {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", res.Metadata.Name, map[string]any{"phase": "Pending", "reason": "NoReadyCandidates", "dryRun": c.DryRun})
			continue
		}
		data, err := render.NftablesEgressRoutePolicyDefaultMarks(res.ID(), spec, active, healthy)
		if err != nil {
			return err
		}
		chunks = append(chunks, data)
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", res.Metadata.Name, map[string]any{
			"phase":             "Applied",
			"selectedCandidate": egressCandidateName(active),
			"selectedTargets":   len(active.Targets),
			"selectedInterface": active.EffectiveInterface(),
			"dryRun":            c.DryRun,
			"updatedAt":         time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	return c.applyNftTable(ctx, nft, path, "ip", "routerd_default_route", bytes.Join(chunks, []byte("\n")))
}

func (c IPv4PolicyRouteController) availableDefaultRouteCandidates(spec api.EgressRoutePolicySpec) []api.EgressRoutePolicyCandidate {
	var out []api.EgressRoutePolicyCandidate
	aliases := c.aliases()
	for _, candidate := range spec.Candidates {
		if !c.targetHealthy(candidate.HealthCheck) {
			continue
		}
		if len(candidate.Targets) > 0 {
			for _, target := range candidate.Targets {
				if !c.targetHealthy(target.HealthCheck) {
					continue
				}
				if ifname := aliases[target.EffectiveInterface()]; ifname != "" && c.linkExists(context.Background(), ifname) {
					out = append(out, candidate)
					break
				}
			}
			continue
		}
		if ifname := aliases[candidate.EffectiveInterface()]; ifname != "" && c.linkExists(context.Background(), ifname) {
			out = append(out, candidate)
		}
	}
	return out
}

func (c IPv4PolicyRouteController) effectivePolicyRouteRouter(activeTargetCandidates map[string]bool) *api.Router {
	if c.Router == nil {
		return nil
	}
	out := *c.Router
	out.Spec.Resources = make([]api.Resource, 0, len(c.Router.Spec.Resources))
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "EgressRoutePolicy" {
			out.Spec.Resources = append(out.Spec.Resources, res)
			continue
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil {
			out.Spec.Resources = append(out.Spec.Resources, res)
			continue
		}
		mode := firstNonEmpty(spec.Mode, "")
		if mode == "priority" {
			var candidates []api.EgressRoutePolicyCandidate
			for _, candidate := range spec.Candidates {
				if len(candidate.Targets) == 0 || !activeTargetCandidates[egressCandidateKey(res.Metadata.Name, candidate)] {
					continue
				}
				targets := candidate.Targets[:0]
				for _, target := range candidate.Targets {
					if c.targetHealthy(target.HealthCheck) {
						targets = append(targets, target)
					}
				}
				if len(targets) == 0 {
					continue
				}
				candidate.Targets = targets
				candidates = append(candidates, candidate)
			}
			if len(candidates) == 0 {
				continue
			}
			spec.Candidates = candidates
			res.Spec = spec
			out.Spec.Resources = append(out.Spec.Resources, res)
			continue
		}
		var candidates []api.EgressRoutePolicyCandidate
		for _, candidate := range spec.Candidates {
			if !c.targetHealthy(candidate.HealthCheck) {
				continue
			}
			if len(candidate.Targets) > 0 {
				targets := candidate.Targets[:0]
				for _, target := range candidate.Targets {
					if c.targetHealthy(target.HealthCheck) {
						targets = append(targets, target)
					}
				}
				if len(targets) == 0 {
					continue
				}
				candidate.Targets = targets
			}
			candidates = append(candidates, candidate)
		}
		if len(candidates) == 0 {
			continue
		}
		spec.Candidates = candidates
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
	switch fmt.Sprint(status["phase"]) {
	case "Healthy":
	case "Failing":
		failed, ok := statusInt(status["consecutiveFailed"])
		if !ok || failed <= 0 || failed >= c.healthCheckUnhealthyThreshold(name) {
			return false
		}
	case PhaseDisabled, PhaseStandby, PhaseNotApplicable:
		return false
	default:
		return false
	}
	checkedAt, ok := parseStatusTimestamp(status["lastCheckedAt"])
	if !ok {
		return false
	}
	maxAge := c.healthCheckFreshness(name)
	return time.Since(checkedAt) <= maxAge
}

func (c IPv4PolicyRouteController) healthCheckFreshness(name string) time.Duration {
	freshness := 2 * time.Minute
	if c.Router == nil {
		return freshness
	}
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "HealthCheck" || res.Metadata.Name != name {
			continue
		}
		spec, err := res.HealthCheckSpec()
		if err != nil {
			return freshness
		}
		interval := parseDurationDefault(spec.Interval, 30*time.Second)
		timeout := parseDurationDefault(spec.Timeout, 3*time.Second)
		candidate := interval*3 + timeout
		if candidate > freshness {
			return candidate
		}
		return freshness
	}
	return freshness
}

func (c IPv4PolicyRouteController) healthCheckUnhealthyThreshold(name string) int {
	if c.Router == nil {
		return 3
	}
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "HealthCheck" || res.Metadata.Name != name {
			continue
		}
		spec, err := res.HealthCheckSpec()
		if err != nil {
			return 3
		}
		if spec.UnhealthyThreshold > 0 {
			return spec.UnhealthyThreshold
		}
		return 3
	}
	return 3
}

func statusInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case uint:
		return int(typed), true
	case uint8:
		return int(typed), true
	case uint16:
		return int(typed), true
	case uint32:
		return int(typed), true
	case uint64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		return n, err == nil
	default:
		return 0, false
	}
}

func parseDurationDefault(value string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func selectDefaultRouteCandidate(candidates []api.EgressRoutePolicyCandidate) (api.EgressRoutePolicyCandidate, bool) {
	if len(candidates) == 0 {
		return api.EgressRoutePolicyCandidate{}, false
	}
	ordered := append([]api.EgressRoutePolicyCandidate{}, candidates...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })
	return ordered[0], true
}

func egressCandidateKey(policy string, candidate api.EgressRoutePolicyCandidate) string {
	return policy + "/" + egressCandidateName(candidate)
}

func egressCandidateName(candidate api.EgressRoutePolicyCandidate) string {
	return firstNonEmpty(candidate.Name, candidate.EffectiveInterface(), "targets")
}

func (c IPv4PolicyRouteController) applyNftTable(ctx context.Context, nft, path, family, table string, data []byte) error {
	if len(data) == 0 {
		if c.DryRun {
			return nil
		}
		if exec.CommandContext(ctx, nft, "list", "table", family, table).Run() == nil {
			_ = exec.CommandContext(ctx, nft, "delete", "table", family, table).Run()
		}
		return nil
	}
	if c.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	changed, err := writeFileIfChanged(path, data, 0644, false)
	if err != nil {
		return err
	}
	missing := exec.CommandContext(ctx, nft, "list", "table", family, table).Run() != nil
	if !changed && !missing {
		return nil
	}
	if out, err := exec.CommandContext(ctx, nft, "-c", "-f", path).CombinedOutput(); err != nil {
		return fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.CommandContext(ctx, nft, "-f", path).CombinedOutput(); err != nil {
		return fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if (changed || missing) && c.Bus != nil {
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
