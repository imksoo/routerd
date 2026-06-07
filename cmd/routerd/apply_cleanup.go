// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resource"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

var runCleanupCommand = runLogged

func filterRouterByWhen(router *api.Router, store routerstate.Store) *api.Router {
	return resourcequery.FilterRouterByWhen(router, store)
}

func resourceWhen(res api.Resource) api.ResourceWhenSpec {
	return resourcequery.ResourceWhen(res)
}

func resourceWhenMatches(when api.ResourceWhenSpec, store routerstate.Store) bool {
	return resourcequery.ResourceWhenMatches(when, store)
}

func stateMatch(store routerstate.Store, name string, match api.StateMatchSpec) bool {
	return resourcequery.StateMatch(store, name, match)
}

func appendPrefixDelegationStateWarnings(result *apply.Result, router *api.Router, store routerstate.Store) {
	for _, res := range router.Spec.Resources {
		if res.Kind != "DHCPv6PrefixDelegation" {
			continue
		}
		base := "ipv6PrefixDelegation." + res.Metadata.Name
		lease, _ := routerstate.PDLeaseFromStore(store, base)
		if lease.CurrentPrefix != "" {
			continue
		}
		msg := fmt.Sprintf("%s is not currently observable", res.ID())
		if lease.LastPrefix != "" {
			msg += "; last delegated prefix was " + lease.LastPrefix
		} else {
			msg += "; no delegated prefix has been recorded locally yet"
		}
		if lease.LastObservedAt != "" {
			msg += " observed at " + lease.LastObservedAt
		}
		msg += ". The OS DHCPv6 client must renew or reacquire PD before the upstream lease expires."
		result.Warnings = append(result.Warnings, msg)
	}
}

func appendLedgerOwnedOrphans(result *apply.Result, router *api.Router, ledgerPath string, transient bool) error {
	if ledgerPath == "" {
		return nil
	}
	ledger, err := loadPlanLedger(ledgerPath, transient)
	if err != nil {
		return err
	}
	defer func() { _ = ledger.Close() }()
	engine := apply.New()
	orphans, _, err := engine.LedgerOwnedOrphans(router, ledger)
	if err != nil {
		return err
	}
	if len(orphans) == 0 {
		return nil
	}
	result.Orphans = appendUniqueOrphans(result.Orphans, orphans)
	result.Warnings = append(result.Warnings, fmt.Sprintf("%d ledger-owned orphaned artifacts found", len(orphans)))
	if result.Phase == "Healthy" {
		result.Phase = "Drifted"
	}
	return nil
}

func loadPlanLedger(path string, transient bool) (resource.Ledger, error) {
	if !transient {
		return resource.LoadLedger(path)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return resource.NewLedger(), nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return resource.NewLedger(), nil
	} else if err != nil {
		return nil, err
	}
	return resource.LoadLedger(path)
}

func appendUniqueOrphans(existing, additions []apply.OrphanedArtifact) []apply.OrphanedArtifact {
	seen := map[string]int{}
	for i, orphan := range existing {
		seen[orphan.Name+"/"+orphan.Remediation] = i
	}
	for _, orphan := range additions {
		id := orphan.Name + "/" + orphan.Remediation
		if index, ok := seen[id]; ok {
			if existing[index].Owner == "" && orphan.Owner != "" {
				existing[index] = orphan
			}
			continue
		}
		seen[id] = len(existing)
		existing = append(existing, orphan)
	}
	return existing
}

func cleanupLedgerOwnedOrphans(router *api.Router, ledgerPath string) ([]string, error) {
	return cleanupLedgerOwnedOrphansMatching(router, ledgerPath, func(resource.Artifact) bool { return true })
}

func cleanupLedgerOwnedOrphansMatching(router *api.Router, ledgerPath string, match func(resource.Artifact) bool) ([]string, error) {
	if ledgerPath == "" {
		return nil, nil
	}
	ledger, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = ledger.Close() }()
	engine := apply.New()
	plan, err := engine.LedgerOwnedOrphanPlan(router, ledger)
	if err != nil {
		return nil, err
	}
	var removed []string
	var removedArtifacts []resource.Artifact
	for _, action := range plan.Actions {
		if action.Type != lifecycle.GCActionRemoveArtifact {
			continue
		}
		artifact := action.Artifact
		if match != nil && !match(artifact) {
			continue
		}
		label, err := cleanupLedgerOwnedArtifact(artifact)
		if err != nil {
			return removed, err
		}
		if label == "" {
			continue
		}
		removed = append(removed, label)
		removedArtifacts = append(removedArtifacts, artifact)
	}
	if len(removedArtifacts) > 0 {
		ledger.Forget(removedArtifacts)
		if err := ledger.Save(ledgerPath); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func cleanupLedgerOwnedArtifact(artifact resource.Artifact) (string, error) {
	return lifecycle.CleanupArtifact(routerdArtifactTeardownExecutor{}, artifact)
}

type routerdArtifactTeardownExecutor struct{}

func (routerdArtifactTeardownExecutor) Features() platform.Features {
	return platformFeatures
}

func (routerdArtifactTeardownExecutor) Run(name string, args ...string) error {
	return runCleanupCommand(name, args...)
}

func (routerdArtifactTeardownExecutor) Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (routerdArtifactTeardownExecutor) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

func (routerdArtifactTeardownExecutor) DeleteIPv4FwmarkRule(priority, mark, table int) error {
	return deleteIPv4FwmarkRule(ipv4FwmarkRule{Priority: priority, Mark: mark, Table: table})
}

func (routerdArtifactTeardownExecutor) FlushIPv4RouteTable(table int) error {
	return flushIPv4RouteTable(table)
}

func desiredDSLiteTunnelIfNames(router *api.Router) map[string]bool {
	desired := map[string]bool{}
	if router == nil {
		return desired
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			continue
		}
		name := strings.TrimSpace(spec.TunnelName)
		if name == "" {
			name = res.Metadata.Name
		}
		if name != "" {
			desired[name] = true
		}
	}
	return desired
}

func cleanupStaleFreeBSDDSLiteTunnels(desired map[string]bool) ([]string, error) {
	out, err := exec.Command("ifconfig").CombinedOutput()
	if err != nil {
		return nil, nil
	}
	var removed []string
	for name, block := range parseIfconfigBlocks(string(out)) {
		if desired[name] || !looksFreeBSDDSLiteTunnel(name, block) {
			continue
		}
		if err := runLogged("ifconfig", name, "destroy"); err != nil {
			return removed, err
		}
		removed = append(removed, "freebsd.gif.tunnel/"+name)
	}
	return removed, nil
}

func cleanupStaleFreeBSDDSLiteIPv4Aliases(desired map[string]bool) ([]string, error) {
	out, err := exec.Command("ifconfig").CombinedOutput()
	if err != nil {
		return nil, nil
	}
	var removed []string
	seen := map[string]bool{}
	for _, artifact := range parseIfconfigIPv4AddressArtifacts(string(out)) {
		ifname, address, ok := strings.Cut(artifact.Name, ":")
		if !ok || !desired[ifname] || !staleDSLiteIPv4Address(address) {
			continue
		}
		id := ifname + ":" + address
		if seen[id] {
			continue
		}
		seen[id] = true
		addr := strings.SplitN(address, "/", 2)[0]
		if err := runLogged("ifconfig", ifname, "inet", addr, "-alias"); err != nil {
			return removed, err
		}
		removed = append(removed, "net.ipv4.address/"+id)
	}
	return removed, nil
}

func staleDSLiteIPv4Address(address string) bool {
	host := strings.SplitN(address, "/", 2)[0]
	switch host {
	case "192.168.160.249", "192.168.160.250", "192.168.160.251", "192.168.160.252",
		"172.18.255.249", "172.18.255.250", "172.18.255.251", "172.18.255.252":
		return true
	default:
		return false
	}
}

func parseIfconfigIPv4AddressArtifacts(output string) []resource.Artifact {
	var artifacts []resource.Artifact
	var ifname string
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				ifname = ""
				continue
			}
			ifname = strings.TrimSuffix(fields[0], ":")
			continue
		}
		if ifname == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "inet" {
			continue
		}
		address := fields[1]
		for i, field := range fields {
			if field == "netmask" && i+1 < len(fields) {
				if prefix := freeBSDIPv4MaskPrefixForCleanup(fields[i+1]); prefix != "" {
					address += "/" + prefix
				}
				break
			}
		}
		artifacts = append(artifacts, resource.Artifact{
			Kind: "net.ipv4.address",
			Name: ifname + ":" + address,
		})
	}
	return artifacts
}

func freeBSDIPv4MaskPrefixForCleanup(mask string) string {
	mask = strings.TrimSpace(strings.ToLower(mask))
	if strings.HasPrefix(mask, "0x") {
		value, err := strconv.ParseUint(strings.TrimPrefix(mask, "0x"), 16, 32)
		if err != nil {
			return ""
		}
		bits := 0
		for i := 31; i >= 0; i-- {
			if value&(1<<uint(i)) == 0 {
				break
			}
			bits++
		}
		return strconv.Itoa(bits)
	}
	ip := net.ParseIP(mask).To4()
	if ip == nil {
		return ""
	}
	bits, _ := net.IPMask(ip).Size()
	if bits < 0 {
		return ""
	}
	return strconv.Itoa(bits)
}

func parseIfconfigBlocks(output string) map[string]string {
	blocks := map[string]string{}
	current := ""
	var lines []string
	flush := func() {
		if current != "" {
			blocks[current] = strings.Join(lines, "\n")
		}
	}
	header := regexp.MustCompile(`^([A-Za-z0-9_.:-]+):\s+flags=`)
	for _, line := range strings.Split(output, "\n") {
		if match := header.FindStringSubmatch(line); match != nil {
			flush()
			current = match[1]
			lines = []string{line}
			continue
		}
		if current != "" {
			lines = append(lines, line)
		}
	}
	flush()
	return blocks
}

func looksFreeBSDDSLiteTunnel(name, block string) bool {
	if !strings.HasPrefix(name, "gif") {
		return false
	}
	return strings.Contains(block, "tunnel inet6 ") &&
		strings.Contains(block, " --> ") &&
		strings.Contains(block, "inet ") &&
		strings.Contains(block, "--> 192.0.0.1")
}

func rememberAppliedArtifacts(router *api.Router, ledgerPath string, generation int64) (int, error) {
	if ledgerPath == "" {
		return 0, nil
	}
	engine := apply.New()
	artifacts, err := engine.AppliedOwnedArtifacts(router)
	if err != nil {
		return 0, err
	}
	ledger, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = ledger.Close() }()
	if sqliteLedger, ok := ledger.(interface{ SetGeneration(int64) }); ok {
		sqliteLedger.SetGeneration(generation)
	}
	ledger.Remember(artifacts)
	if err := ledger.Save(ledgerPath); err != nil {
		return 0, err
	}
	return len(adoptedArtifactsForResult(artifacts)), nil
}

func recordLastAppliedPath(router *api.Router, store routerstate.Store, path string) error {
	if path == "" {
		return nil
	}
	applySourceStore, ok := store.(routerstate.ObjectApplySourceStore)
	if !ok {
		return nil
	}
	for _, res := range router.Spec.Resources {
		if err := applySourceStore.SaveObjectApplySource(res.APIVersion, res.Kind, res.Metadata.Name, path); err != nil {
			return err
		}
	}
	return nil
}

func controllerDefaultStatuses() []controlapi.ControllerStatus {
	names := []string{
		"address",
		"bgp",
		"dhcp-lease-sync",
		"dhcpv4client",
		"dhcpv6",
		"dns-resolver",
		"dslite",
		"firewall",
		"ingress",
		"kernel-module",
		"nat",
		"network-adoption",
		"package",
		"pppoesession",
		"route",
		"service-unit",
		"vrrp",
	}
	out := make([]controlapi.ControllerStatus, 0, len(names))
	for _, name := range names {
		out = append(out, controlapi.ControllerStatus{
			Name:          name,
			Mode:          "live",
			Reason:        controlapi.ControllerModeReasonLive,
			Message:       "controller is reconciling declared router state",
			ResourceKinds: controllerResourceKinds(name),
		})
	}
	return out
}
