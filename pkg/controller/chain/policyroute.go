// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/egressroute"
	"github.com/imksoo/routerd/pkg/healthcheck"
	"github.com/imksoo/routerd/pkg/nftstate"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resource"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

type IPv4PolicyRouteController struct {
	Router              *api.Router
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
	// Host-only IPv6 policy is deliberately last and isolated: an RA address
	// gap must not roll back the already-complete IPv4 DS-Lite policy path.
	c.reconcileIPv6HostPolicies(ctx)
	return nil
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

func (c IPv4PolicyRouteController) reconcileIPv6HostPolicies(ctx context.Context) {
	path := firstNonEmpty(c.HostPolicyStatePath, "/run/routerd/ipv6-host-policy.json")
	previous, err := loadIPv6HostPolicyState(path)
	if err != nil {
		c.logHostPolicyError("load IPv6 host policy state", err)
		return
	}
	desired, unavailable, err := c.desiredIPv6HostPolicies(ctx)
	if err != nil {
		c.logHostPolicyError("derive IPv6 host policy", err)
		return
	}
	previousByName := make(map[string]ipv6HostPolicy, len(previous.Policies))
	for _, policy := range previous.Policies {
		previousByName[policy.Name] = policy
	}
	for name := range unavailable {
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", name, map[string]any{
			"phase": "Pending", "reason": "PreferredSourceUnavailable", "dryRun": c.DryRun,
		})
		if old, ok := previousByName[name]; ok {
			desired = append(desired, old) // retain a known-good rule until RA returns.
		}
	}
	if len(unavailable) > 0 && len(desired) == 0 && len(previous.Policies) == 0 {
		return // first acquisition is intentionally unapplied.
	}
	applied, transient, err := c.applyIPv6HostPolicyState(ctx, previous, ipv6HostPolicyState{Policies: desired}, unavailable)
	if err != nil {
		c.logHostPolicyError("reconcile IPv6 host policy", err)
		for _, policy := range desired {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", policy.Name, map[string]any{"phase": "Pending", "reason": "HostPolicyApplyFailed", "dryRun": c.DryRun})
		}
		return
	}
	if !c.DryRun {
		if err := writeIPv6HostPolicyState(path, applied); err != nil {
			c.logHostPolicyError("write IPv6 host policy state", err)
			return
		}
	}
	for _, policy := range desired {
		if policy.isSourceRule() {
			continue
		}
		if unavailable[policy.Name] || transient[policy.Name] {
			continue
		}
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", policy.Name, map[string]any{"phase": "Applied", "dryRun": c.DryRun})
	}
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
			local := strings.TrimSpace(resourcequery.Value(c.Store, api.StatusValueSourceSpec{Resource: "DSLiteTunnel/" + res.Metadata.Name, Field: "localIPv6"}))
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

func (c IPv4PolicyRouteController) activeTargetCandidates() map[string]bool {
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
		if !c.egressTargetAvailable(ctx, aliases, target) {
			return
		}
		if !c.shouldInstallPolicyRouteForHealthCheck(target.HealthCheck, target.Mark) {
			return
		}
		gateway := firstNonEmpty(resourcequery.Value(c.Store, target.GatewayFrom), strings.TrimSpace(target.Gateway))
		c.applyRouteTarget(ctx, aliases, owner, target.Name, target.EffectiveInterface(), target.EffectiveTable(), target.Priority, target.Mark, target.EffectiveMetric(), firstNonEmpty(target.GatewaySource, "none"), gateway, skipMissing, &failures)
	}
	applyCandidate := func(owner string, candidate api.EgressRoutePolicyCandidate) {
		if egressRoutePolicyCandidateDisabled(candidate) {
			return
		}
		// A candidate can become eligible as soon as its when condition flips,
		// before the referenced DS-Lite tunnel has been recreated.  Do not turn
		// that normal HA transition into a controller-wide hard error, and do not
		// install a bootstrap rule from a stale health-check result.
		if !c.egressCandidateAvailable(candidate) {
			return
		}
		if !c.shouldInstallPolicyRouteForHealthCheck(candidate.HealthCheck, candidate.Mark) {
			return
		}
		c.applyRouteTarget(ctx, aliases, owner, firstNonEmpty(candidate.Name, candidate.EffectiveInterface()), c.candidateDevice(candidate), candidate.EffectiveTable(), candidate.Priority, candidate.Mark, candidate.EffectiveMetric(), firstNonEmpty(candidate.GatewaySource, "none"), c.candidateGateway(candidate), c.candidateReferencesDSLite(candidate), &failures)
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
	if err := c.cleanupLedgerOwnedPolicyRoutes(ctx, aliases); err != nil {
		return err
	}
	return nil
}

func (c IPv4PolicyRouteController) shouldInstallPolicyRouteForHealthCheck(name string, mark int) bool {
	if c.targetHealthy(name) {
		return true
	}
	return c.healthCheckUsesFwMark(name, mark)
}

func (c IPv4PolicyRouteController) cleanupLedgerOwnedPolicyRoutes(ctx context.Context, aliases map[string]string) error {
	if c.DryRun || c.LedgerPath == "" || c.Router == nil {
		return nil
	}
	ledger, err := resource.LoadLedger(c.LedgerPath)
	if err != nil {
		return err
	}
	defer func() { _ = ledger.Close() }()
	desired := map[string]resource.Artifact{}
	desiredTables := map[int]bool{}
	for _, artifact := range apply.DesiredOwnedArtifacts(c.Router, aliases) {
		if artifact.Kind != "linux.ipv4.fwmarkRule" && artifact.Kind != "linux.ipv4.routeTable" {
			continue
		}
		desired[artifact.Identity()] = artifact
		if table, ok := artifactIPv4Table(artifact); ok {
			desiredTables[table] = true
		}
	}
	actual, err := c.currentPolicyRouteArtifacts(ctx)
	if err != nil {
		return err
	}
	var stale []resource.Artifact
	for _, owned := range ledger.All() {
		switch owned.Kind {
		case "linux.ipv4.fwmarkRule", "linux.ipv4.routeTable":
		default:
			continue
		}
		if _, ok := desired[owned.Identity()]; ok {
			continue
		}
		if observed, ok := actual[owned.Identity()]; ok {
			stale = append(stale, observed)
		} else {
			stale = append(stale, owned)
		}
	}
	sort.SliceStable(stale, func(i, j int) bool {
		return policyRouteArtifactCleanupOrder(stale[i]) < policyRouteArtifactCleanupOrder(stale[j])
	})
	var forgotten []resource.Artifact
	for _, artifact := range stale {
		switch artifact.Kind {
		case "linux.ipv4.fwmarkRule":
			rule, ok := ipv4FwmarkRuleFromPolicyArtifact(artifact)
			if !ok {
				continue
			}
			if actual[artifact.Identity()].Kind != "" {
				if err := c.deleteIPv4FwmarkRule(ctx, rule); err != nil {
					return err
				}
			}
			forgotten = append(forgotten, artifact)
		case "linux.ipv4.routeTable":
			table, ok := artifactIPv4Table(artifact)
			if !ok {
				continue
			}
			if actual[artifact.Identity()].Kind != "" && !desiredTables[table] {
				if err := c.flushIPv4RouteTable(ctx, table); err != nil {
					return err
				}
			}
			forgotten = append(forgotten, artifact)
		}
	}
	if len(forgotten) == 0 {
		return nil
	}
	ledger.Forget(forgotten)
	return ledger.Save(c.LedgerPath)
}

func policyRouteArtifactCleanupOrder(artifact resource.Artifact) int {
	switch artifact.Kind {
	case "linux.ipv4.fwmarkRule":
		return 0
	case "linux.ipv4.routeTable":
		return 10
	default:
		return 100
	}
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
	if ifname == "" && outboundInterface != "" {
		ifname = outboundInterface
	}
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
		if err != nil || spec.HostTraffic || firstNonEmpty(spec.Mode, "") != "priority" {
			if err != nil {
				return err
			}
			continue
		}
		if unsupportedPrioritySelection(spec) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", res.Metadata.Name, map[string]any{
				"phase":   "Pending",
				"reason":  egressroute.ReasonUnsupported,
				"message": fmt.Sprintf("selection %q is reserved but not implemented", firstNonEmpty(spec.Selection, egressroute.SelectionHighestWeightReady)),
				"dryRun":  c.DryRun,
			})
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
		status := map[string]any{
			"phase":             "Applied",
			"family":            firstNonEmpty(spec.Family, "ipv4"),
			"selectedCandidate": egressCandidateName(active),
			"selectedTargets":   len(active.Targets),
			"selectedInterface": active.EffectiveInterface(),
			"selectedSource":    active.Source,
			"selectedWeight":    active.Weight,
			"dryRun":            c.DryRun,
			"updatedAt":         time.Now().UTC().Format(time.RFC3339Nano),
			"candidates":        priorityStatusCandidates(spec.Candidates, healthy),
		}
		if len(active.Targets) == 0 {
			status["selectedDevice"] = c.candidateDevice(active)
			status["selectedGateway"] = c.candidateGateway(active)
			status["selectedGatewaySource"] = firstNonEmpty(active.GatewaySource, "none")
			status["selectedRouteTable"] = active.EffectiveTable()
			status["selectedMetric"] = active.EffectiveMetric()
		}
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", res.Metadata.Name, status)
	}
	return c.applyNftTable(ctx, nft, path, "ip", "routerd_default_route", bytes.Join(chunks, []byte("\n")))
}

func (c IPv4PolicyRouteController) availableDefaultRouteCandidates(spec api.EgressRoutePolicySpec) []api.EgressRoutePolicyCandidate {
	var out []api.EgressRoutePolicyCandidate
	aliases := c.aliases()
	for _, candidate := range spec.Candidates {
		if egressRoutePolicyCandidateDisabled(candidate) {
			continue
		}
		if !c.targetHealthy(candidate.HealthCheck) {
			continue
		}
		if len(candidate.Targets) > 0 {
			for _, target := range candidate.Targets {
				if !c.targetHealthy(target.HealthCheck) {
					continue
				}
				if !c.egressTargetAvailable(context.Background(), aliases, target) {
					continue
				}
				if ifname := aliases[target.EffectiveInterface()]; ifname != "" && c.linkExists(context.Background(), ifname) {
					out = append(out, candidate)
					break
				}
			}
			continue
		}
		device := c.candidateDevice(candidate)
		if !c.egressCandidateAvailable(candidate) {
			continue
		}
		if ifname := firstNonEmpty(aliases[device], device); ifname != "" && c.linkExists(context.Background(), ifname) {
			out = append(out, candidate)
		}
	}
	return out
}

// egressTargetAvailable rejects a stale health-check result until its DS-Lite
// tunnel has been reconciled for the current master.  A link name alone is not
// sufficient: a previous master can leave a stale status briefly while its
// tunnel no longer exists on this host.
func (c IPv4PolicyRouteController) egressTargetAvailable(ctx context.Context, aliases map[string]string, target api.EgressRoutePolicyTarget) bool {
	logical := target.EffectiveInterface()
	ifname := firstNonEmpty(aliases[logical], logical)
	if ifname == "" || !c.linkExists(ctx, ifname) {
		return false
	}
	return c.dsliteResourceReady(logical)
}

func (c IPv4PolicyRouteController) egressCandidateAvailable(candidate api.EgressRoutePolicyCandidate) bool {
	if !resourcequery.DependenciesReady(c.Store, candidate.DependsOn) {
		return false
	}
	for _, reference := range []string{candidate.Source, candidate.DeviceFrom.Resource} {
		if c.dsliteResourceReference(reference) && !c.dsliteResourceReady(reference) {
			return false
		}
	}
	return true
}

func (c IPv4PolicyRouteController) dsliteResourceReady(reference string) bool {
	if c.Router == nil || reference == "" || !c.dsliteResourceReference(reference) {
		return true
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
		return strings.TrimSpace(fmt.Sprint(c.Store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", res.Metadata.Name)["phase"])) == "Up"
	}
	return false
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

func (c IPv4PolicyRouteController) candidateDevice(candidate api.EgressRoutePolicyCandidate) string {
	if device := resourcequery.Value(c.Store, candidate.DeviceFrom); device != "" {
		return device
	}
	logical := candidate.EffectiveInterface()
	return firstNonEmpty(c.aliases()[logical], logical)
}

func (c IPv4PolicyRouteController) candidateGateway(candidate api.EgressRoutePolicyCandidate) string {
	return firstNonEmpty(resourcequery.Value(c.Store, candidate.GatewayFrom), candidate.Gateway)
}

func (c IPv4PolicyRouteController) effectivePolicyRouteRouter(activeTargetCandidates map[string]bool) *api.Router {
	if c.Router == nil {
		return nil
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
				targets := make([]api.EgressRoutePolicyTarget, 0, len(candidate.Targets))
				for _, target := range candidate.Targets {
					if c.targetHealthy(target.HealthCheck) && (!c.dsliteResourceReference(target.EffectiveInterface()) || c.egressTargetAvailable(context.Background(), aliases, target)) {
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
			if egressRoutePolicyCandidateDisabled(candidate) {
				continue
			}
			if !c.targetHealthy(candidate.HealthCheck) {
				continue
			}
			if !c.egressCandidateAvailable(candidate) {
				continue
			}
			if len(candidate.Targets) > 0 {
				targets := make([]api.EgressRoutePolicyTarget, 0, len(candidate.Targets))
				for _, target := range candidate.Targets {
					if c.targetHealthy(target.HealthCheck) && (!c.dsliteResourceReference(target.EffectiveInterface()) || c.egressTargetAvailable(context.Background(), aliases, target)) {
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
	status := healthCheckEffectiveStatus(c.Store.ObjectStatus(api.NetAPIVersion, "HealthCheck", name))
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

func healthCheckEffectiveStatus(status map[string]any) map[string]any {
	observed, ok := status["observed"].(map[string]any)
	if !ok || len(observed) == 0 {
		return status
	}
	return observed
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
	if len(data) == 0 {
		if c.DryRun {
			return nil
		}
		exists := exec.CommandContext(ctx, nft, "list", "table", family, table).Run() == nil
		if !exists && nftstate.RecentlyVerified(path, time.Now().UTC()) {
			return nil
		}
		if exists {
			_ = exec.CommandContext(ctx, nft, "delete", "table", family, table).Run()
		}
		_ = nftstate.MarkVerified(path, time.Now().UTC())
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
	if !changed && !missing && nftstate.RecentlyVerified(path, time.Now().UTC()) {
		_ = nftstate.MarkVerified(path, time.Now().UTC())
		return nil
	}
	if out, err := exec.CommandContext(ctx, nft, "-c", "-f", path).CombinedOutput(); err != nil {
		return fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.CommandContext(ctx, nft, "-f", path).CombinedOutput(); err != nil {
		return fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	_ = nftstate.MarkVerified(path, time.Now().UTC())
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

type ipv4PolicyFwmarkRule struct {
	Priority int
	Mark     int
	Table    int
}

func (c IPv4PolicyRouteController) currentPolicyRouteArtifacts(ctx context.Context) (map[string]resource.Artifact, error) {
	out := map[string]resource.Artifact{}
	rules, err := c.commandOutput(ctx, "ip", "-4", "rule", "show")
	if err != nil {
		return nil, err
	}
	for _, artifact := range parseIPv4PolicyFwmarkRuleArtifacts(string(rules)) {
		out[artifact.Identity()] = artifact
	}
	tables, err := c.commandOutput(ctx, "ip", "-4", "route", "show", "table", "all")
	if err != nil {
		return nil, err
	}
	for _, artifact := range parseIPv4PolicyRouteTableArtifacts(string(tables)) {
		out[artifact.Identity()] = artifact
	}
	return out, nil
}

func (c IPv4PolicyRouteController) deleteIPv4FwmarkRule(ctx context.Context, rule ipv4PolicyFwmarkRule) error {
	out, err := c.commandOutput(ctx, "ip", "-4", "rule", "del", "priority", fmt.Sprintf("%d", rule.Priority), "fwmark", fmt.Sprintf("0x%x", rule.Mark), "table", fmt.Sprintf("%d", rule.Table))
	if err != nil {
		return fmt.Errorf("ip -4 rule del priority %d fwmark 0x%x table %d: %w: %s", rule.Priority, rule.Mark, rule.Table, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c IPv4PolicyRouteController) flushIPv4RouteTable(ctx context.Context, table int) error {
	out, err := c.commandOutput(ctx, "ip", "-4", "route", "flush", "table", fmt.Sprintf("%d", table))
	if err != nil {
		return fmt.Errorf("ip -4 route flush table %d: %w: %s", table, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c IPv4PolicyRouteController) commandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.CommandOutput != nil {
		return c.CommandOutput(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func parseIPv4PolicyFwmarkRuleArtifacts(output string) []resource.Artifact {
	var artifacts []resource.Artifact
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		rule := ipv4PolicyFwmarkRule{}
		priority, err := strconv.Atoi(strings.TrimSuffix(fields[0], ":"))
		if err != nil {
			continue
		}
		rule.Priority = priority
		for i, field := range fields {
			switch field {
			case "fwmark":
				if i+1 >= len(fields) {
					continue
				}
				mark, err := strconv.ParseInt(strings.SplitN(fields[i+1], "/", 2)[0], 0, 64)
				if err != nil {
					continue
				}
				rule.Mark = int(mark)
			case "lookup":
				if i+1 >= len(fields) {
					continue
				}
				table, err := strconv.Atoi(fields[i+1])
				if err != nil {
					continue
				}
				rule.Table = table
			}
		}
		if rule.Mark != 0 && rule.Table != 0 {
			artifacts = append(artifacts, ipv4PolicyFwmarkRuleArtifact(rule))
		}
	}
	return artifacts
}

func parseIPv4PolicyRouteTableArtifacts(output string) []resource.Artifact {
	seen := map[int]bool{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field != "table" || i+1 >= len(fields) {
				continue
			}
			table, err := strconv.Atoi(fields[i+1])
			if err == nil && table != 0 {
				seen[table] = true
			}
		}
	}
	var tables []int
	for table := range seen {
		tables = append(tables, table)
	}
	sort.Ints(tables)
	artifacts := make([]resource.Artifact, 0, len(tables))
	for _, table := range tables {
		artifacts = append(artifacts, ipv4PolicyRouteTableArtifact(table))
	}
	return artifacts
}

func ipv4PolicyFwmarkRuleArtifact(rule ipv4PolicyFwmarkRule) resource.Artifact {
	return resource.Artifact{
		Kind: "linux.ipv4.fwmarkRule",
		Name: fmt.Sprintf("priority=%d,mark=0x%x,table=%d", rule.Priority, rule.Mark, rule.Table),
		Attributes: map[string]string{
			"priority": fmt.Sprintf("%d", rule.Priority),
			"mark":     fmt.Sprintf("0x%x", rule.Mark),
			"table":    fmt.Sprintf("%d", rule.Table),
		},
	}
}

func ipv4PolicyRouteTableArtifact(table int) resource.Artifact {
	return resource.Artifact{
		Kind: "linux.ipv4.routeTable",
		Name: fmt.Sprintf("table=%d", table),
		Attributes: map[string]string{
			"table": fmt.Sprintf("%d", table),
		},
	}
}

func ipv4FwmarkRuleFromPolicyArtifact(artifact resource.Artifact) (ipv4PolicyFwmarkRule, bool) {
	priority, err := strconv.Atoi(artifact.Attributes["priority"])
	if err != nil {
		return ipv4PolicyFwmarkRule{}, false
	}
	mark, err := strconv.ParseInt(artifact.Attributes["mark"], 0, 64)
	if err != nil {
		return ipv4PolicyFwmarkRule{}, false
	}
	table, err := strconv.Atoi(artifact.Attributes["table"])
	if err != nil {
		return ipv4PolicyFwmarkRule{}, false
	}
	return ipv4PolicyFwmarkRule{Priority: priority, Mark: int(mark), Table: table}, true
}

func artifactIPv4Table(artifact resource.Artifact) (int, bool) {
	if artifact.Attributes == nil {
		return 0, false
	}
	table, err := strconv.Atoi(artifact.Attributes["table"])
	return table, err == nil
}

func (c IPv4PolicyRouteController) linkExists(ctx context.Context, ifname string) bool {
	return exec.CommandContext(ctx, "ip", "link", "show", "dev", ifname).Run() == nil
}
