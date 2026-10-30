// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/egressroute"
	"github.com/imksoo/routerd/pkg/healthcheck"
	"github.com/imksoo/routerd/pkg/nftstate"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

type IPv4PolicyRouteController struct {
	Router              *api.Router
	DeclaredRouter      *api.Router
	Bus                 *bus.Bus
	Store               Store
	DryRun              bool
	NftCommand          string
	PolicyPath          string
	DefaultRoutePath    string
	LedgerPath          string
	HostPolicyStatePath string
	CommandOutput       func(context.Context, string, ...string) ([]byte, error)
	Logger              *slog.Logger
	OperatingSystem     platform.OS
	Now                 func() time.Time
	NativeScope         func(context.Context) (string, error)
}

func (c IPv4PolicyRouteController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	if !c.hasIPRoute2() {
		return nil
	}
	nft := firstNonEmpty(c.NftCommand, "nft")
	policyPath := firstNonEmpty(c.PolicyPath, "/run/routerd/policy-route.nft")
	defaultRoutePath := firstNonEmpty(c.DefaultRoutePath, "/run/routerd/default-route.nft")
	aliases := c.aliases()
	if err := c.applyRouteTables(ctx, aliases); err != nil {
		return err
	}
	// Route journal persistence and native acknowledgements can wait while
	// Health changes. Select the current candidate only after those effects.
	activeTargetCandidates, err := c.activeTargetCandidates(ctx)
	if err != nil {
		return err
	}
	if err := c.applyPolicyNft(ctx, nft, policyPath, activeTargetCandidates); err != nil {
		return err
	}
	if err := c.applyDefaultRoutePolicies(ctx, nft, defaultRoutePath); err != nil {
		return err
	}
	// Host-only IPv6 policy is deliberately last and isolated: an RA address
	// gap must not roll back the already-complete IPv4 DS-Lite policy path.
	return c.reconcileIPv6HostPolicies(ctx)
}

func (c IPv4PolicyRouteController) hasIPRoute2() bool {
	if c.OperatingSystem != "" {
		return c.OperatingSystem == platform.OSLinux
	}
	_, features := platform.Current()
	return features.HasIproute2
}

type ipv6HostPolicy struct {
	Name      string `json:"name"`
	Owner     string `json:"owner,omitempty"`
	Priority  int    `json:"priority"`
	Table     int    `json:"table,omitempty"`
	Lookup    string `json:"lookup,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
	Interface string `json:"interface,omitempty"`
	Source    string `json:"source,omitempty"`
	Metric    int    `json:"metric,omitempty"`
	RuleFrom  bool   `json:"ruleFrom,omitempty"`
}

type ipv6HostPolicyState struct {
	Policies []ipv6HostPolicy `json:"policies"`
}

// isSourceRule also recognizes the short-lived on-disk form written while
// this feature was being introduced. That form has no route fields and is
// therefore unambiguously a source-only rule, even if ruleFrom is absent.
func (p ipv6HostPolicy) isSourceRule() bool {
	return p.RuleFrom || (p.Lookup != "" && p.Table == 0 && p.Gateway == "" && p.Interface == "" && p.Source != "")
}

func (c IPv4PolicyRouteController) reconcileIPv6HostPolicies(ctx context.Context) error {
	path := firstNonEmpty(c.HostPolicyStatePath, "/run/routerd/ipv6-host-policy.json")
	previous, err := loadIPv6HostPolicyState(path)
	if err != nil {
		c.logHostPolicyError("load IPv6 host policy state", err)
		return err
	}
	desired, unavailable, err := c.desiredIPv6HostPolicies(ctx)
	if err != nil {
		c.logHostPolicyError("derive IPv6 host policy", err)
		return err
	}
	previousByName := make(map[string]ipv6HostPolicy, len(previous.Policies))
	for _, policy := range previous.Policies {
		previousByName[policy.Name] = policy
	}
	for name := range unavailable {
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", name, map[string]any{
			"phase": "Pending", "reason": "PreferredSourceUnavailable", "dryRun": c.DryRun,
		}); err != nil {
			return err
		}
		if old, ok := previousByName[name]; ok {
			desired = append(desired, old) // retain a known-good rule until RA returns.
		}
	}
	if len(unavailable) > 0 && len(desired) == 0 && len(previous.Policies) == 0 {
		return nil // first acquisition is intentionally unapplied.
	}
	applied, transient, err := c.applyIPv6HostPolicyState(ctx, previous, ipv6HostPolicyState{Policies: desired}, unavailable)
	if err != nil {
		c.logHostPolicyError("reconcile IPv6 host policy", err)
		for _, policy := range desired {
			err = errors.Join(err, c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", policy.Name, map[string]any{"phase": "Pending", "reason": "HostPolicyApplyFailed", "dryRun": c.DryRun}))
		}
		return err
	}
	if !c.DryRun {
		if err := writeIPv6HostPolicyState(path, applied); err != nil {
			c.logHostPolicyError("write IPv6 host policy state", err)
			return err
		}
	}
	for _, policy := range desired {
		if policy.isSourceRule() {
			continue
		}
		if unavailable[policy.Name] || transient[policy.Name] {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", policy.Name, map[string]any{"phase": "Applied", "dryRun": c.DryRun}); err != nil {
			return err
		}
	}
	return nil
}

func (c IPv4PolicyRouteController) logHostPolicyError(message string, err error) {
	if c.Logger != nil {
		c.Logger.Error(message, "error", err)
	}
}

func (c IPv4PolicyRouteController) desiredIPv6HostPolicies(ctx context.Context) ([]ipv6HostPolicy, map[string]bool, error) {
	aliases := c.aliases()
	var policies []ipv6HostPolicy
	unavailable := map[string]bool{}
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "EgressRoutePolicy" {
			continue
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil {
			return nil, nil, err
		}
		if !spec.HostTraffic || firstNonEmpty(spec.Family, "ipv4") != "ipv6" {
			continue
		}
		for _, candidate := range spec.Candidates {
			if egressRoutePolicyCandidateDisabled(candidate) {
				continue
			}
			logical := candidate.EffectiveInterface()
			ifname := firstNonEmpty(aliases[logical], logical)
			ready, err := c.ipv6HostPolicyDeviceReady(ctx, ifname)
			if err != nil || !ready {
				unavailable[res.Metadata.Name] = true
				continue
			}
			source, err := c.ipv6GlobalAddress(ctx, ifname)
			if err != nil {
				unavailable[res.Metadata.Name] = true
				continue
			}
			metric := candidate.EffectiveMetric()
			if metric == 0 {
				metric = 50
			}
			policies = append(policies, ipv6HostPolicy{Name: res.Metadata.Name, Owner: res.Metadata.Name, Priority: candidate.Priority, Table: candidate.EffectiveTable(), Gateway: candidate.Gateway, Interface: ifname, Source: source, Metric: metric})
		}
	}
	// A hostTraffic rule selects every local packet (iif lo), not merely the
	// outer packets of a DS-Lite tunnel.  Keep normal host traffic on the
	// configured physical-WAN policy, and let only a tunnel's explicit outer
	// source address bypass it to the VMAC-preferred main table.
	if len(policies) > 0 {
		nextPriority := 10100
		for _, res := range c.Router.Spec.Resources {
			if res.Kind != "DSLiteTunnel" {
				continue
			}
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				return nil, nil, err
			}
			// A tunnel using the physical WAN SLAAC address already matches the
			// normal iif lo host policy below and must stay in its dedicated table.
			// Only an endpoint derived from the delegated prefix needs to bypass
			// that policy and follow the VMAC-aware main table.
			if firstNonEmpty(spec.LocalAddressSource, "interface") != "delegatedAddress" {
				continue
			}
			value, err := resourcequery.ReadValue(c.Store, api.StatusValueSourceSpec{Resource: "DSLiteTunnel/" + res.Metadata.Name, Field: "localIPv6"})
			if err != nil {
				return nil, nil, err
			}
			local := strings.TrimSpace(value)
			if local == "" {
				continue
			}
			policies = append(policies, ipv6HostPolicy{Name: "dslite-source-" + res.Metadata.Name, Owner: policies[0].Owner, Priority: nextPriority, Lookup: "main", Source: local, RuleFrom: true})
			nextPriority++
		}
	}
	return policies, unavailable, nil
}

func (c IPv4PolicyRouteController) applyIPv6HostPolicyState(ctx context.Context, previous, desired ipv6HostPolicyState, retained map[string]bool) (ipv6HostPolicyState, map[string]bool, error) {
	desiredByName := map[string]ipv6HostPolicy{}
	for _, policy := range desired.Policies {
		desiredByName[policy.Name] = policy
	}
	for _, old := range previous.Policies {
		if retained[old.Name] {
			continue
		}
		if next, ok := desiredByName[old.Name]; !ok || next != old {
			if err := c.deleteIPv6HostPolicy(ctx, old); err != nil {
				return ipv6HostPolicyState{}, nil, err
			}
		}
	}
	applied := ipv6HostPolicyState{}
	transient := map[string]bool{}
	for _, policy := range desired.Policies {
		if retained[policy.Name] {
			applied.Policies = append(applied.Policies, policy)
			continue
		}
		if c.DryRun {
			applied.Policies = append(applied.Policies, policy)
			continue
		}
		if !policy.isSourceRule() {
			if out, err := c.commandOutput(ctx, "ip", "-6", "route", "replace", "default", "via", policy.Gateway, "dev", policy.Interface, "table", strconv.Itoa(policy.Table), "metric", strconv.Itoa(policy.Metric), "src", policy.Source); err != nil {
				if transientIPv6HostPolicyError(out, err) {
					c.logHostPolicyTransient(policy, "route replace", out, err)
					transient[policy.Name] = true
					continue
				}
				return ipv6HostPolicyState{}, nil, fmt.Errorf("ip -6 route replace: %w: %s", err, strings.TrimSpace(string(out)))
			}
		}
		isTransient, err := c.ensureIPv6HostRule(ctx, policy)
		if err != nil {
			return ipv6HostPolicyState{}, nil, err
		}
		if isTransient {
			transient[policy.Name] = true
			continue
		}
		applied.Policies = append(applied.Policies, policy)
	}
	return applied, transient, nil
}

func (c IPv4PolicyRouteController) ipv6HostPolicyDeviceReady(ctx context.Context, ifname string) (bool, error) {
	out, err := c.commandOutput(ctx, "ip", "-o", "link", "show", "dev", ifname)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field == "state" && i+1 < len(fields) && fields[i+1] == "UP" {
				return true, nil
			}
		}
	}
	return false, nil
}

func (c IPv4PolicyRouteController) ipv6GlobalAddress(ctx context.Context, ifname string) (string, error) {
	out, err := c.commandOutput(ctx, "ip", "-6", "-o", "addr", "show", "dev", ifname, "scope", "global")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		ineligible := false
		for _, field := range fields {
			if field == "tentative" || field == "deprecated" {
				ineligible = true
				break
			}
		}
		if ineligible {
			continue
		}
		for i, field := range fields {
			if field == "inet6" && i+1 < len(fields) {
				return strings.Split(fields[i+1], "/")[0], nil
			}
		}
	}
	return "", fmt.Errorf("no global IPv6 address on %s", ifname)
}

func (c IPv4PolicyRouteController) ensureIPv6HostRule(ctx context.Context, policy ipv6HostPolicy) (bool, error) {
	out, err := c.commandOutput(ctx, "ip", "-6", "rule", "show")
	if err != nil {
		return false, err
	}
	priority := strconv.Itoa(policy.Priority)
	table := firstNonEmpty(policy.Lookup, strconv.Itoa(policy.Table))
	for _, line := range strings.Split(string(out), "\n") {
		selector := "iif lo"
		if policy.isSourceRule() {
			// ip rule show renders a host prefix as the bare address, while
			// ip rule add requires the explicit /128 we persist in state.
			selector = "from " + policy.Source
		}
		if strings.Contains(line, priority+":") && strings.Contains(line, selector) && strings.Contains(line, "lookup "+table) {
			return false, nil
		}
	}
	args := []string{"-6", "rule", "add", "priority", priority}
	if policy.isSourceRule() {
		args = append(args, "from", policy.Source+"/128")
	} else {
		args = append(args, "iif", "lo")
	}
	args = append(args, "lookup", table)
	if out, err := c.commandOutput(ctx, "ip", args...); err != nil {
		if transientIPv6HostPolicyError(out, err) {
			c.logHostPolicyTransient(policy, "rule add", out, err)
			return true, nil
		}
		return false, fmt.Errorf("ip -6 rule add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return false, nil
}

func transientIPv6HostPolicyError(out []byte, err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(message, "nexthop device is not up") || strings.Contains(message, "invalid source address")
}

func (c IPv4PolicyRouteController) logHostPolicyTransient(policy ipv6HostPolicy, operation string, out []byte, err error) {
	if c.Logger != nil {
		c.Logger.Warn("IPv6 host policy operation temporarily unavailable", "policy", policy.Name, "operation", operation, "error", err, "output", strings.TrimSpace(string(out)))
	}
}

func (c IPv4PolicyRouteController) deleteIPv6HostPolicy(ctx context.Context, policy ipv6HostPolicy) error {
	if c.DryRun {
		return nil
	}
	rule := []string{"-6", "rule", "del", "priority", strconv.Itoa(policy.Priority)}
	if policy.isSourceRule() {
		rule = append(rule, "from", policy.Source+"/128")
	} else {
		rule = append(rule, "iif", "lo")
	}
	rule = append(rule, "lookup", firstNonEmpty(policy.Lookup, strconv.Itoa(policy.Table)))
	commands := [][]string{rule}
	if !policy.isSourceRule() {
		commands = append(commands, []string{"-6", "route", "del", "default", "via", policy.Gateway, "dev", policy.Interface, "table", strconv.Itoa(policy.Table), "metric", strconv.Itoa(policy.Metric), "src", policy.Source})
	}
	for _, args := range commands {
		out, err := c.commandOutput(ctx, "ip", args...)
		if err != nil && !missingIPv6HostPolicy(out, err) {
			return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func missingIPv6HostPolicy(out []byte, err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(message, "no such process") || strings.Contains(message, "no such file or directory") || strings.Contains(message, "not in table") || strings.Contains(message, "cannot find")
}

func loadIPv6HostPolicyState(path string) (ipv6HostPolicyState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ipv6HostPolicyState{}, nil
	}
	if err != nil {
		return ipv6HostPolicyState{}, err
	}
	var state ipv6HostPolicyState
	if err := json.Unmarshal(data, &state); err != nil {
		return ipv6HostPolicyState{}, err
	}
	return state, nil
}

func writeIPv6HostPolicyState(path string, state ipv6HostPolicyState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".ipv6-host-policy-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
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

func (c IPv4PolicyRouteController) activeTargetCandidates(ctx context.Context) (map[string]bool, error) {
	active := map[string]bool{}
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "EgressRoutePolicy" {
			continue
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil || spec.HostTraffic || firstNonEmpty(spec.Mode, "") != "priority" {
			continue
		}
		if unsupportedPrioritySelection(spec) {
			continue
		}
		healthy, err := c.availableDefaultRouteCandidates(ctx, spec)
		if err != nil {
			return nil, err
		}
		candidate, ok := selectDefaultRouteCandidate(healthy)
		if ok && len(candidate.Targets) > 0 {
			active[egressCandidateKey(res.Metadata.Name, candidate)] = true
		}
	}
	return active, nil
}

func (c IPv4PolicyRouteController) applyRouteTables(ctx context.Context, _ map[string]string) error {
	desired, err := c.desiredPolicyIPv4Effects(ctx)
	if err != nil {
		return err
	}
	return c.reconcilePolicyIPv4Effects(ctx, desired)
}

func (c IPv4PolicyRouteController) desiredPolicyIPv4Effects(ctx context.Context) ([]policyIPv4Effect, error) {
	aliases := c.aliases()
	var effects []policyIPv4Effect
	applyTarget := func(owner string, target api.EgressRoutePolicyTarget, skipMissing bool) error {
		available, err := c.egressTargetAvailable(ctx, aliases, target)
		if err != nil || !available {
			return err
		}
		install, err := c.shouldInstallPolicyRouteForHealthCheck(target.HealthCheck, target.Mark)
		if err != nil || !install {
			return err
		}
		value, err := resourcequery.ReadValue(c.Store, target.GatewayFrom)
		if err != nil {
			return err
		}
		gateway := firstNonEmpty(value, strings.TrimSpace(target.Gateway))
		next, err := c.policyIPv4Target(ctx, aliases, owner, target.Name, target.EffectiveInterface(), target.EffectiveTable(), target.Priority, target.Mark, target.EffectiveMetric(), firstNonEmpty(target.GatewaySource, "none"), gateway, skipMissing)
		effects = append(effects, next...)
		return err
	}
	applyCandidate := func(owner string, candidate api.EgressRoutePolicyCandidate) error {
		if egressRoutePolicyCandidateDisabled(candidate) {
			return nil
		}
		// A candidate can become eligible as soon as its when condition flips,
		// before the referenced DS-Lite tunnel has been recreated.  Do not turn
		// that normal HA transition into a controller-wide hard error, and do not
		// install a bootstrap rule from a stale health-check result.
		available, err := c.egressCandidateAvailable(candidate)
		if err != nil || !available {
			return err
		}
		install, err := c.shouldInstallPolicyRouteForHealthCheck(candidate.HealthCheck, candidate.Mark)
		if err != nil || !install {
			return err
		}
		device, err := c.candidateDevice(candidate)
		if err != nil {
			return err
		}
		gateway, err := c.candidateGateway(candidate)
		if err != nil {
			return err
		}
		next, err := c.policyIPv4Target(ctx, aliases, owner, firstNonEmpty(candidate.Name, candidate.EffectiveInterface()), device, candidate.EffectiveTable(), candidate.Priority, candidate.Mark, candidate.EffectiveMetric(), firstNonEmpty(candidate.GatewaySource, "none"), gateway, c.candidateReferencesDSLite(candidate))
		effects = append(effects, next...)
		return err
	}
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "EgressRoutePolicy" {
			continue
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil {
			return nil, err
		}
		if spec.HostTraffic {
			continue
		}
		mode := firstNonEmpty(spec.Mode, "")
		switch mode {
		case "priority", "mark", "hash":
		default:
			continue
		}
		if mode == "priority" && unsupportedPrioritySelection(spec) {
			continue
		}
		for _, candidate := range spec.Candidates {
			if egressRoutePolicyCandidateDisabled(candidate) {
				continue
			}
			if len(candidate.Targets) > 0 {
				for i, target := range candidate.Targets {
					if target.Name == "" {
						target.Name = fmt.Sprintf("%s-%d", firstNonEmpty(candidate.Name, res.Metadata.Name), i)
					}
					if err := applyTarget(res.ID(), target, true); err != nil {
						return nil, err
					}
				}
				continue
			}
			if candidate.Mark == 0 {
				continue
			}
			if err := applyCandidate(res.ID(), candidate); err != nil {
				return nil, err
			}
		}
	}
	return sortedPolicyIPv4Effects(effects)
}

func (c IPv4PolicyRouteController) shouldInstallPolicyRouteForHealthCheck(name string, mark int) (bool, error) {
	healthy, err := c.targetHealthy(name)
	if err != nil || healthy {
		return healthy, err
	}
	return c.healthCheckUsesFwMark(name, mark), nil
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
		resolved, err := healthcheck.ResolveSpecForResource(c.Router, name, spec)
		return err == nil && resolved.FwMark == mark
	}
	return false
}

func (c IPv4PolicyRouteController) policyIPv4Target(ctx context.Context, aliases map[string]string, owner, name, outboundInterface string, table, priority, mark, metric int, gatewaySource, gateway string, skipMissing bool) ([]policyIPv4Effect, error) {
	ifname := firstNonEmpty(aliases[outboundInterface], outboundInterface)
	if ifname == "" {
		return nil, fmt.Errorf("%s target %s has no outbound interface", owner, name)
	}
	if metric == 0 {
		metric = 50
	}
	index := 1
	if !c.DryRun {
		var present bool
		var err error
		index, present, err = c.policyIPv4Index(ctx, ifname)
		if err != nil {
			return nil, err
		}
		if !present {
			if skipMissing {
				return nil, nil
			}
			return nil, fmt.Errorf("%s target %s interface %s is absent", owner, name, ifname)
		}
	}
	gateway, err := c.routeGateway(ctx, ifname, gatewaySource, gateway)
	if err != nil {
		return nil, fmt.Errorf("%s target %s gateway: %w", owner, name, err)
	}
	return []policyIPv4Effect{
		{Kind: "route", Table: table, Metric: metric, Interface: ifname, Gateway: gateway, IfIndex: index},
		{Kind: "rule", Table: table, Priority: priority, Mark: mark},
	}, nil
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
		return c.currentIPv4DefaultGatewayForInterface(ctx, ifname)
	default:
		return "", fmt.Errorf("unsupported gatewaySource %q", gatewaySource)
	}
}

func (c IPv4PolicyRouteController) currentIPv4DefaultGatewayForInterface(ctx context.Context, ifname string) (string, error) {
	out, err := c.commandOutput(ctx, "ip", "-4", "route", "show", "default", "dev", ifname)
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
	router, err := c.effectivePolicyRouteRouter(ctx, activeTargetCandidates)
	if err != nil {
		return err
	}
	data, err := render.NftablesIPv4PolicyRoutes(router)
	if err != nil {
		return err
	}
	return c.applyNftTable(ctx, nft, path, "ip", "routerd_policy", data)
}

func (c IPv4PolicyRouteController) applyDefaultRoutePolicies(ctx context.Context, nft, path string) error {
	var chunks [][]byte
	type policyStatus struct {
		name      string
		status    map[string]any
		candidate *api.EgressRoutePolicyCandidate
		previous  map[string]any
		role      egressRoleObservation
	}
	var results []policyStatus
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "EgressRoutePolicy" {
			continue
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil || spec.HostTraffic || firstNonEmpty(spec.Mode, "") != "priority" {
			if err != nil {
				return err
			}
			continue
		}
		if unsupportedPrioritySelection(spec) {
			results = append(results, policyStatus{name: res.Metadata.Name, status: map[string]any{
				"phase":   "Pending",
				"reason":  egressroute.ReasonUnsupported,
				"message": fmt.Sprintf("selection %q is reserved but not implemented", firstNonEmpty(spec.Selection, egressroute.SelectionHighestWeightReady)),
				"dryRun":  c.DryRun,
			}})
			continue
		}
		healthy, err := c.availableDefaultRouteCandidates(ctx, spec)
		if err != nil {
			return err
		}
		active, ok := selectDefaultRouteCandidate(healthy)
		if !ok {
			results = append(results, policyStatus{name: res.Metadata.Name, status: map[string]any{"phase": "Pending", "reason": "NoReadyCandidates", "dryRun": c.DryRun}})
			continue
		}
		data, err := render.NftablesEgressRoutePolicyDefaultMarks(res.ID(), spec, active, healthy)
		if err != nil {
			return err
		}
		chunks = append(chunks, data)
		status := map[string]any{
			"phase":             "Applied",
			"family":            firstNonEmpty(spec.Family, "ipv4"),
			"selectedCandidate": egressCandidateName(active),
			"selectedTargets":   len(active.Targets),
			"selectedInterface": active.EffectiveInterface(),
			"selectedSource":    active.Source,
			"selectedWeight":    active.Weight,
			"dryRun":            c.DryRun,
			"candidates":        priorityStatusCandidates(spec.Candidates, healthy),
		}
		if len(active.Targets) == 0 {
			device, err := c.candidateDevice(active)
			if err != nil {
				return err
			}
			gateway, err := c.candidateGateway(active)
			if err != nil {
				return err
			}
			status["selectedDevice"] = device
			status["selectedGateway"] = gateway
			status["selectedGatewaySource"] = firstNonEmpty(active.GatewaySource, "none")
			status["selectedRouteTable"] = active.EffectiveTable()
			status["selectedMetric"] = active.EffectiveMetric()
		}
		previous, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", res.Metadata.Name)
		if err != nil {
			return err
		}
		role, err := c.readHADatapathRole(res.Metadata.Name)
		if err != nil {
			return err
		}
		results = append(results, policyStatus{name: res.Metadata.Name, status: status, candidate: &active, previous: previous, role: role})
	}
	applyErr := c.applyNftTable(ctx, nft, path, "ip", "routerd_default_route", bytes.Join(chunks, []byte("\n")))
	var statusErr error
	for _, result := range results {
		if applyErr != nil {
			result.status["phase"] = "Error"
			result.status["reason"] = "ApplyFailed"
			result.status["error"] = applyErr.Error()
		}
		if applyErr == nil && result.candidate != nil {
			now := c.now()
			result.status["updatedAt"] = now.Format(time.RFC3339Nano)
			c.addHADatapathStatus(result.name, *result.candidate, result.previous, result.status, now, result.role)
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", result.name, result.status); err != nil {
			statusErr = errors.Join(statusErr, fmt.Errorf("save policy %s result: %w", result.name, err))
		}
	}
	return errors.Join(applyErr, statusErr)
}

func (c IPv4PolicyRouteController) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

type egressVRRPMasterGate struct {
	Kind string
	Name string
}

func (g egressVRRPMasterGate) reference() string {
	return g.Kind + "/" + g.Name
}

type egressRoleObservation struct {
	role         string
	transitionAt string
}

func (c IPv4PolicyRouteController) readHADatapathRole(policyName string) (egressRoleObservation, error) {
	spec, ok := c.declaredEgressRoutePolicySpec(policyName)
	if !ok {
		return egressRoleObservation{}, nil
	}
	gate, ok := egressPolicyVRRPMasterGate(spec)
	if !ok {
		return egressRoleObservation{}, nil
	}
	status, err := c.Store.ReadObjectStatus(api.NetAPIVersion, gate.Kind, gate.Name)
	if err != nil {
		return egressRoleObservation{}, err
	}
	return egressRoleObservation{role: cleanStatusString(status["role"]), transitionAt: cleanStatusString(status["lastRoleTransitionAt"])}, nil
}

func (c IPv4PolicyRouteController) addHADatapathStatus(policyName string, active api.EgressRoutePolicyCandidate, previous, status map[string]any, now time.Time, observation egressRoleObservation) {
	spec, ok := c.declaredEgressRoutePolicySpec(policyName)
	if !ok {
		return
	}
	gate, ok := egressPolicyVRRPMasterGate(spec)
	if !ok {
		return
	}
	role, transitionAt := observation.role, observation.transitionAt
	status["datapathRoleResource"] = gate.reference()
	status["datapathRole"] = role
	status["datapathRoleTransitionAt"] = transitionAt
	status["datapathFallbackBehavior"] = "select-next-ready-candidate"

	ready := role == "master" && (whenHasVRRPMasterGate(spec.When, gate) || whenHasVRRPMasterGate(active.When, gate))
	status["datapathReady"] = ready
	switch {
	case role != "master":
		status["datapathState"] = "Standby"
		status["newFlowBehavior"] = "fallback"
	case !ready:
		status["datapathState"] = "Converging"
		status["newFlowBehavior"] = "fallback"
	default:
		status["datapathState"] = "Ready"
		status["newFlowBehavior"] = "selected-ha-candidate"
	}

	if transitionAt != "" && transitionAt == strings.TrimSpace(fmt.Sprint(previous["datapathRoleTransitionAt"])) {
		for _, key := range []string{"datapathReadyAt", "datapathReadyDuration", "datapathReadyDurationMillis"} {
			if value, exists := previous[key]; exists {
				status[key] = value
			}
		}
	}
	if !ready || status["datapathReadyAt"] != nil {
		return
	}
	status["datapathReadyAt"] = now.Format(time.RFC3339Nano)
	if started, err := time.Parse(time.RFC3339Nano, transitionAt); err == nil && !started.After(now) {
		duration := now.Sub(started)
		status["datapathReadyDuration"] = duration.Round(time.Millisecond).String()
		status["datapathReadyDurationMillis"] = duration.Milliseconds()
	}
}

func (c IPv4PolicyRouteController) declaredEgressRoutePolicySpec(name string) (api.EgressRoutePolicySpec, bool) {
	router := c.DeclaredRouter
	if router == nil {
		router = c.Router
	}
	if router == nil {
		return api.EgressRoutePolicySpec{}, false
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "EgressRoutePolicy" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.EgressRoutePolicySpec()
		return spec, err == nil
	}
	return api.EgressRoutePolicySpec{}, false
}

func egressPolicyVRRPMasterGate(spec api.EgressRoutePolicySpec) (egressVRRPMasterGate, bool) {
	if gate, ok := firstVRRPMasterGate(spec.When); ok {
		return gate, true
	}
	for _, candidate := range spec.Candidates {
		if gate, ok := firstVRRPMasterGate(candidate.When); ok {
			return gate, true
		}
	}
	return egressVRRPMasterGate{}, false
}

func firstVRRPMasterGate(when api.ResourceWhenSpec) (egressVRRPMasterGate, bool) {
	for reference, match := range when.State {
		if !stateMatchIncludesMaster(match) {
			continue
		}
		kind, name, ok := whenStatusDependencyRef(reference)
		if ok && kind == "VirtualAddress" && whenStateReferenceField(reference) == "role" {
			return egressVRRPMasterGate{Kind: kind, Name: name}, true
		}
	}
	for _, child := range append(append([]api.ResourceWhenSpec(nil), when.All...), when.Any...) {
		if gate, ok := firstVRRPMasterGate(child); ok {
			return gate, true
		}
	}
	return egressVRRPMasterGate{}, false
}

func whenHasVRRPMasterGate(when api.ResourceWhenSpec, want egressVRRPMasterGate) bool {
	gate, ok := firstVRRPMasterGate(when)
	return ok && gate == want
}

func stateMatchIncludesMaster(match api.StateMatchSpec) bool {
	if strings.EqualFold(strings.TrimSpace(match.Equals), "master") {
		return true
	}
	for _, value := range match.In {
		if strings.EqualFold(strings.TrimSpace(value), "master") {
			return true
		}
	}
	return false
}

func whenStateReferenceField(reference string) string {
	reference = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(reference, "${"), "}"))
	if _, field, ok := strings.Cut(reference, ".status."); ok {
		return field
	}
	index := strings.LastIndex(reference, ".")
	if index < 0 {
		return ""
	}
	return reference[index+1:]
}

func (c IPv4PolicyRouteController) availableDefaultRouteCandidates(ctx context.Context, spec api.EgressRoutePolicySpec) ([]api.EgressRoutePolicyCandidate, error) {
	var out []api.EgressRoutePolicyCandidate
	aliases := c.aliases()
	for _, candidate := range spec.Candidates {
		if egressRoutePolicyCandidateDisabled(candidate) {
			continue
		}
		healthy, err := c.targetHealthy(candidate.HealthCheck)
		if err != nil {
			return nil, err
		}
		if !healthy {
			continue
		}
		if len(candidate.Targets) > 0 {
			for _, target := range candidate.Targets {
				healthy, err := c.targetHealthy(target.HealthCheck)
				if err != nil {
					return nil, err
				}
				if !healthy {
					continue
				}
				available, err := c.egressTargetAvailable(ctx, aliases, target)
				if err != nil {
					return nil, err
				}
				if !available {
					continue
				}
				if ifname := aliases[target.EffectiveInterface()]; ifname != "" && c.linkExists(ctx, ifname) {
					out = append(out, candidate)
					break
				}
			}
			continue
		}
		device, err := c.candidateDevice(candidate)
		if err != nil {
			return nil, err
		}
		available, err := c.egressCandidateAvailable(candidate)
		if err != nil {
			return nil, err
		}
		if !available {
			continue
		}
		if ifname := firstNonEmpty(aliases[device], device); ifname != "" && c.linkExists(ctx, ifname) {
			out = append(out, candidate)
		}
	}
	return out, nil
}

// egressTargetAvailable rejects a stale health-check result until its DS-Lite
// tunnel has been reconciled for the current master.  A link name alone is not
// sufficient: a previous master can leave a stale status briefly while its
// tunnel no longer exists on this host.
func (c IPv4PolicyRouteController) egressTargetAvailable(ctx context.Context, aliases map[string]string, target api.EgressRoutePolicyTarget) (bool, error) {
	logical := target.EffectiveInterface()
	ifname := firstNonEmpty(aliases[logical], logical)
	if ifname == "" || !c.linkExists(ctx, ifname) {
		return false, nil
	}
	return c.dsliteResourceReady(logical)
}

func (c IPv4PolicyRouteController) egressCandidateAvailable(candidate api.EgressRoutePolicyCandidate) (bool, error) {
	ready, err := resourcequery.ReadDependenciesReady(c.Store, candidate.DependsOn)
	if err != nil || !ready {
		return false, err
	}
	for _, reference := range []string{candidate.Source, candidate.DeviceFrom.Resource} {
		if c.dsliteResourceReference(reference) {
			ready, err := c.dsliteResourceReady(reference)
			if err != nil || !ready {
				return false, err
			}
		}
	}
	return true, nil
}

func (c IPv4PolicyRouteController) dsliteResourceReady(reference string) (bool, error) {
	if c.Router == nil || reference == "" || !c.dsliteResourceReference(reference) {
		return true, nil
	}
	kind, name, qualified := resourcequery.SplitResource(reference)
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "DSLiteTunnel" {
			continue
		}
		if (qualified && (kind != "DSLiteTunnel" || name != res.Metadata.Name)) || (!qualified && reference != res.Metadata.Name) {
			continue
		}
		// DSLiteTunnel is reconciled by routerd itself, not by a daemon whose
		// observed substatus should override the controller phase.  In
		// particular, phase=Disabled/reason=WhenFalse must win over a retained
		// observed.phase=Up from the previous MASTER generation.
		status, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "DSLiteTunnel", res.Metadata.Name)
		if err != nil {
			return false, err
		}
		return strings.TrimSpace(fmt.Sprint(status["phase"])) == "Up", nil
	}
	return false, nil
}

func (c IPv4PolicyRouteController) dsliteResourceReference(reference string) bool {
	if c.Router == nil || strings.TrimSpace(reference) == "" {
		return false
	}
	kind, _, qualified := resourcequery.SplitResource(reference)
	if qualified {
		return kind == "DSLiteTunnel"
	}
	for _, res := range c.Router.Spec.Resources {
		if res.Kind != "DSLiteTunnel" {
			continue
		}
		if reference == res.Metadata.Name {
			return true
		}
	}
	return false
}

func (c IPv4PolicyRouteController) candidateReferencesDSLite(candidate api.EgressRoutePolicyCandidate) bool {
	return c.dsliteResourceReference(candidate.Source) || c.dsliteResourceReference(candidate.DeviceFrom.Resource)
}

func (c IPv4PolicyRouteController) candidateDevice(candidate api.EgressRoutePolicyCandidate) (string, error) {
	device, err := resourcequery.ReadValue(c.Store, candidate.DeviceFrom)
	if err != nil {
		return "", err
	}
	if device != "" {
		return device, nil
	}
	logical := candidate.EffectiveInterface()
	return firstNonEmpty(c.aliases()[logical], logical), nil
}

func (c IPv4PolicyRouteController) candidateGateway(candidate api.EgressRoutePolicyCandidate) (string, error) {
	value, err := resourcequery.ReadValue(c.Store, candidate.GatewayFrom)
	return firstNonEmpty(value, candidate.Gateway), err
}

func (c IPv4PolicyRouteController) effectivePolicyRouteRouter(ctx context.Context, activeTargetCandidates map[string]bool) (*api.Router, error) {
	if c.Router == nil {
		return nil, nil
	}
	out := *c.Router
	out.Spec.Resources = make([]api.Resource, 0, len(c.Router.Spec.Resources))
	aliases := c.aliases()
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
		if spec.HostTraffic {
			continue
		}
		mode := firstNonEmpty(spec.Mode, "")
		if mode == "priority" {
			var candidates []api.EgressRoutePolicyCandidate
			for _, candidate := range spec.Candidates {
				if egressRoutePolicyCandidateDisabled(candidate) || len(candidate.Targets) == 0 || !activeTargetCandidates[egressCandidateKey(res.Metadata.Name, candidate)] {
					continue
				}
				targets, ready, err := c.targetsWithRuntimeReadiness(ctx, aliases, candidate.Targets)
				if err != nil {
					return nil, err
				}
				if !ready {
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
			if egressRoutePolicyCandidateDisabled(candidate) {
				continue
			}
			healthy, err := c.targetHealthy(candidate.HealthCheck)
			if err != nil {
				return nil, err
			}
			if !healthy {
				continue
			}
			available, err := c.egressCandidateAvailable(candidate)
			if err != nil {
				return nil, err
			}
			if !available {
				continue
			}
			if len(candidate.Targets) > 0 {
				targets, ready, err := c.targetsWithRuntimeReadiness(ctx, aliases, candidate.Targets)
				if err != nil {
					return nil, err
				}
				if !ready {
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
	return &out, nil
}

func (c IPv4PolicyRouteController) targetsWithRuntimeReadiness(ctx context.Context, aliases map[string]string, declared []api.EgressRoutePolicyTarget) ([]api.EgressRoutePolicyTarget, bool, error) {
	targets := append([]api.EgressRoutePolicyTarget(nil), declared...)
	readyCount := 0
	for i := range targets {
		ready, err := c.targetHealthy(targets[i].HealthCheck)
		if err != nil {
			return nil, false, err
		}
		if ready && c.dsliteResourceReference(targets[i].EffectiveInterface()) {
			ready, err = c.egressTargetAvailable(ctx, aliases, targets[i])
			if err != nil {
				return nil, false, err
			}
		}
		targets[i].RuntimeReady = new(bool)
		*targets[i].RuntimeReady = ready
		if ready {
			readyCount++
		}
	}
	return targets, readyCount > 0, nil
}

func (c IPv4PolicyRouteController) targetHealthy(name string) (bool, error) {
	if name == "" {
		return true, nil
	}
	if c.Router == nil {
		return false, nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "HealthCheck" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.HealthCheckSpec()
		if err != nil {
			return false, err
		}
		resolved, err := healthcheck.ResolveSpecWithStoreForResource(c.Router, c.Store, name, spec)
		if err != nil {
			return false, err
		}
		observation, err := c.Store.HealthObservation(name)
		if err != nil {
			return false, err
		}
		return observation.Usable(c.now(), resolved), nil
	}
	return false, nil
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

func unsupportedPrioritySelection(spec api.EgressRoutePolicySpec) bool {
	return firstNonEmpty(spec.Selection, egressroute.SelectionHighestWeightReady) != egressroute.SelectionHighestWeightReady
}

func selectDefaultRouteCandidate(candidates []api.EgressRoutePolicyCandidate) (api.EgressRoutePolicyCandidate, bool) {
	if len(candidates) == 0 {
		return api.EgressRoutePolicyCandidate{}, false
	}
	states := make([]egressroute.CandidateState, 0, len(candidates))
	for i, candidate := range candidates {
		states = append(states, egressroute.CandidateState{
			Name:     egressCandidateName(candidate),
			Ready:    true,
			Weight:   candidate.Weight,
			Priority: candidate.Priority,
			Index:    i,
		})
	}
	selected, ok := egressroute.SelectHighestWeightReady(states)
	if !ok {
		return api.EgressRoutePolicyCandidate{}, false
	}
	return candidates[selected.Index], true
}

func priorityStatusCandidates(candidates, readyCandidates []api.EgressRoutePolicyCandidate) []map[string]any {
	ready := map[string]bool{}
	for _, candidate := range readyCandidates {
		ready[egressCandidateName(candidate)] = true
	}
	out := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		item := map[string]any{
			"name":          egressCandidateName(candidate),
			"source":        candidate.Source,
			"gateway":       candidate.Gateway,
			"gatewaySource": firstNonEmpty(candidate.GatewaySource, "none"),
			"routeTable":    candidate.EffectiveTable(),
			"metric":        candidate.EffectiveMetric(),
			"weight":        candidate.Weight,
			"priority":      candidate.Priority,
			"ready":         ready[egressCandidateName(candidate)],
			"disabled":      egressRoutePolicyCandidateDisabled(candidate),
			"targets":       len(candidate.Targets),
		}
		if len(candidate.Targets) == 0 {
			item["device"] = candidate.EffectiveInterface()
		}
		out = append(out, item)
	}
	return out
}

func egressCandidateKey(policy string, candidate api.EgressRoutePolicyCandidate) string {
	return policy + "/" + egressCandidateName(candidate)
}

func egressCandidateName(candidate api.EgressRoutePolicyCandidate) string {
	return firstNonEmpty(candidate.Name, candidate.EffectiveInterface(), "targets")
}

func (c IPv4PolicyRouteController) applyNftTable(ctx context.Context, nft, path, family, table string, data []byte) error {
	if c.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	current, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	if readErr != nil || !bytes.Equal(current, data) {
		if err := nftstate.Invalidate(path); err != nil {
			return err
		}
	}
	changed, err := writeFileIfChanged(path, data, 0644, false)
	if err != nil {
		return err
	}
	_, listErr := c.commandOutput(ctx, nft, "list", "table", family, table)
	missing := listErr != nil
	if !changed && !missing && nftstate.RecentlyVerified(path, time.Now().UTC()) {
		return nil
	}
	if err := nftstate.Invalidate(path); err != nil {
		return err
	}
	if len(data) == 0 {
		// A failed inventory read is not proof of absence. Only a successful
		// deletion or the kernel's explicit missing-table result acknowledges it.
		out, err := c.commandOutput(ctx, nft, "delete", "table", family, table)
		if err != nil && !strings.Contains(string(out), "No such file or directory") {
			return fmt.Errorf("%s delete table %s %s: %w: %s", nft, family, table, err, strings.TrimSpace(string(out)))
		}
		return nftstate.MarkVerified(path, time.Now().UTC())
	}
	if out, err := c.commandOutput(ctx, nft, "-c", "-f", path); err != nil {
		return fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if out, err := c.commandOutput(ctx, nft, "-f", path); err != nil {
		return fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if err := nftstate.MarkVerified(path, time.Now().UTC()); err != nil {
		return err
	}
	if (changed || missing) && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ipv4.policy_route.applied", daemonapi.SeverityInfo)
		event.Attributes = map[string]string{"table": table, "path": path}
		_ = c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c IPv4PolicyRouteController) commandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.CommandOutput != nil {
		return c.CommandOutput(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (c IPv4PolicyRouteController) linkExists(ctx context.Context, ifname string) bool {
	_, err := c.commandOutput(ctx, "ip", "link", "show", "dev", ifname)
	return err == nil
}
