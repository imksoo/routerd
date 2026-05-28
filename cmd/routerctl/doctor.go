// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	doctorPass = "pass"
	doctorWarn = "warn"
	doctorFail = "fail"
	doctorSkip = "skip"
)

type doctorCheck struct {
	Area   string `json:"area" yaml:"area"`
	Name   string `json:"name" yaml:"name"`
	Status string `json:"status" yaml:"status"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
	Remedy string `json:"remedy,omitempty" yaml:"remedy,omitempty"`
}

type doctorSummary struct {
	Overall string `json:"overall" yaml:"overall"`
	Pass    int    `json:"pass" yaml:"pass"`
	Warn    int    `json:"warn" yaml:"warn"`
	Fail    int    `json:"fail" yaml:"fail"`
	Skip    int    `json:"skip" yaml:"skip"`
}

type doctorReport struct {
	Summary doctorSummary `json:"summary" yaml:"summary"`
	Checks  []doctorCheck `json:"checks" yaml:"checks"`
}

type doctorRunner struct {
	opts   diagnoseOptions
	router *api.Router
	store  routerstate.Store
}

var doctorAreas = []string{"wan", "dns", "dslite", "dhcpv6-pd", "nat", "firewall", "rollback", "disk", "mgmt", "reconcile", "runtime"}

// doctorReconcileWarnThreshold and doctorReconcileFailThreshold are total error
// counts (across all controllers) that promote the reconcile area to warn/fail.
const (
	doctorReconcileWarnThreshold = 1
	doctorReconcileFailThreshold = 10
)

// reconcileStatusFetcher allows tests to stub the controllers fetch.
var reconcileStatusFetcher = fetchReconcileControllers

// runtimeStatsFetcher allows tests to stub the runtime-stats fetch.
var runtimeStatsFetcher = fetchRuntimeStats

// doctorRuntimeGoroutineWarn and doctorRuntimeFDWarnPercent are conservative,
// observational thresholds. They never fail the run; they only flag footprints
// worth a closer look during a resource-leak investigation.
const (
	// 10000 goroutines is far above routerd's steady-state (a few hundred); a
	// count this high usually signals a leak (e.g. blocked-forever goroutines).
	doctorRuntimeGoroutineWarn = 10000
	// 80% of RLIMIT_NOFILE leaves little headroom before accept()/open() start
	// failing with EMFILE.
	doctorRuntimeFDWarnPercent = 80
)

func doctorCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := parseDiagnoseOptions("doctor", args, stdout)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		usage(stderr)
		return err
	}
	if opts.Target != "" && !validDoctorArea(opts.Target) {
		return fmt.Errorf("unknown doctor area %q", opts.Target)
	}
	router, store, err := loadDiagnoseInputs(opts)
	if err != nil {
		return err
	}
	runner := doctorRunner{opts: opts, router: router, store: store}
	areas := doctorAreas
	if opts.Target != "" {
		areas = []string{opts.Target}
	}
	report := doctorReport{}
	for _, area := range areas {
		report.Checks = append(report.Checks, runner.runArea(area)...)
	}
	report.Summary = summarizeDoctorChecks(report.Checks)
	if err := writeDoctorReport(stdout, report, opts.Output); err != nil {
		return err
	}
	if report.Summary.Fail > 0 {
		return errors.New("doctor found failing checks")
	}
	return nil
}

func validDoctorArea(area string) bool {
	for _, candidate := range doctorAreas {
		if area == candidate {
			return true
		}
	}
	return false
}

func (r doctorRunner) runArea(area string) []doctorCheck {
	switch area {
	case "wan":
		return r.doctorWAN()
	case "dns":
		return r.doctorDNS()
	case "dslite":
		return r.doctorDSLite()
	case "dhcpv6-pd":
		return r.doctorDHCPv6PD()
	case "nat":
		return r.doctorNAT()
	case "firewall":
		return r.doctorFirewall()
	case "rollback":
		return r.doctorRollback()
	case "disk":
		return r.doctorDisk()
	case "mgmt":
		return r.doctorMgmt()
	case "reconcile":
		return r.doctorReconcile()
	case "runtime":
		return r.doctorRuntime()
	default:
		return []doctorCheck{{Area: area, Name: "area", Status: doctorSkip, Detail: "unknown area"}}
	}
}

func (r doctorRunner) doctorReconcile() []doctorCheck {
	controllers, err := reconcileStatusFetcher(r.opts.Socket, r.opts.Timeout)
	if err != nil {
		return []doctorCheck{{Area: "reconcile", Name: "controllers", Status: doctorSkip, Detail: "routerd status socket unavailable: " + err.Error()}}
	}
	if len(controllers) == 0 {
		return []doctorCheck{{Area: "reconcile", Name: "controllers", Status: doctorSkip, Detail: "no controller history reported"}}
	}
	since := r.opts.Since
	cutoff := time.Time{}
	window := "all-time"
	if since > 0 {
		cutoff = time.Now().UTC().Add(-since)
		window = "last " + since.String()
	}
	totalErrors := 0
	affectedControllers := 0
	currentlyFailing := 0
	var samples []string
	for _, controller := range controllers {
		count := 0
		for _, entry := range controller.ReconcileErrorHistory {
			if !cutoff.IsZero() && entry.CompletedAt.Before(cutoff) {
				continue
			}
			count++
			if len(samples) < 5 {
				resource := entry.ResourceKind
				if entry.ResourceName != "" {
					if resource != "" {
						resource = resource + "/" + entry.ResourceName
					} else {
						resource = entry.ResourceName
					}
				}
				sample := controller.Name + "@" + entry.CompletedAt.Format(time.RFC3339) + " trigger=" + firstNonEmpty(entry.Trigger, "-")
				if resource != "" {
					sample += " resource=" + resource
				}
				sample += " error=" + truncateForDetail(entry.Error, 80)
				samples = append(samples, sample)
			}
		}
		if count > 0 {
			affectedControllers++
		}
		totalErrors += count
		if controller.CurrentError {
			currentlyFailing++
		}
	}
	status := doctorPass
	switch {
	case totalErrors >= doctorReconcileFailThreshold:
		status = doctorFail
	case totalErrors >= doctorReconcileWarnThreshold:
		status = doctorWarn
	}
	if currentlyFailing > 0 && status == doctorPass {
		status = doctorWarn
	}
	detail := fmt.Sprintf("%d reconcile errors in %s across %d controllers (current failures=%d, controllers=%d)", totalErrors, window, affectedControllers, currentlyFailing, len(controllers))
	if len(samples) > 0 {
		detail = detail + "; " + strings.Join(samples, " | ")
	}
	check := doctorCheck{Area: "reconcile", Name: "controllers", Status: status, Detail: detail}
	if status != doctorPass {
		check.Remedy = "inspect routerctl status --show-errors and routerd logs for the affected controllers"
	}
	return []doctorCheck{check}
}

func fetchReconcileControllers(socketPath string, timeout time.Duration) ([]controlapi.ControllerStatus, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	status, err := controlapi.NewUnixClient(socketPath).Status(ctx)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, nil
	}
	return status.Status.Controllers, nil
}

// doctorRuntime reports routerd's own process footprint (heap, goroutines, fds)
// from the read-only status socket. It is purely observational: success emits an
// informational pass with the footprint summary, and unusual footprints are
// downgraded to warn. It never fails the run.
func (r doctorRunner) doctorRuntime() []doctorCheck {
	stats, err := runtimeStatsFetcher(r.opts.Socket, r.opts.Timeout)
	if err != nil {
		return []doctorCheck{{Area: "runtime", Name: "process", Status: doctorSkip, Detail: "routerd status socket unavailable: " + err.Error()}}
	}
	if stats == nil {
		return []doctorCheck{{Area: "runtime", Name: "process", Status: doctorSkip, Detail: "no runtime stats reported"}}
	}
	heapMiB := float64(stats.HeapAllocBytes) / (1024 * 1024)
	fdSummary := "n/a"
	if stats.MaxFDs > 0 {
		fdSummary = fmt.Sprintf("%d/%d", stats.OpenFDs, stats.MaxFDs)
	} else if stats.OpenFDs > 0 {
		fdSummary = fmt.Sprintf("%d/?", stats.OpenFDs)
	}
	detail := fmt.Sprintf("heapAlloc=%.1fMiB heapObjects=%d numGoroutine=%d numGC=%d openFds=%s",
		heapMiB, stats.HeapObjects, stats.NumGoroutine, stats.NumGC, fdSummary)

	status := doctorPass
	remedy := ""
	if stats.NumGoroutine > doctorRuntimeGoroutineWarn {
		status = doctorWarn
		detail = appendDoctorDetail(detail, fmt.Sprintf("unusually high goroutine count (%d > %d)", stats.NumGoroutine, doctorRuntimeGoroutineWarn))
		remedy = "capture a goroutine profile and inspect routerd for leaked goroutines"
	}
	if stats.MaxFDs > 0 && uint64(stats.OpenFDs)*100/stats.MaxFDs >= doctorRuntimeFDWarnPercent {
		status = doctorWarn
		detail = appendDoctorDetail(detail, fmt.Sprintf("fd usage >=%d%% of RLIMIT_NOFILE (%d/%d)", doctorRuntimeFDWarnPercent, stats.OpenFDs, stats.MaxFDs))
		remedy = "inspect /proc/<pid>/fd for leaked descriptors or raise RLIMIT_NOFILE"
	}
	return []doctorCheck{{Area: "runtime", Name: "process", Status: status, Detail: detail, Remedy: remedy}}
}

func fetchRuntimeStats(socketPath string, timeout time.Duration) (*controlapi.RuntimeStats, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return controlapi.NewUnixClient(socketPath).Runtime(ctx)
}

func (r doctorRunner) doctorWAN() []doctorCheck {
	var checks []doctorCheck
	policies := selectResources(r.router.Spec.Resources, "EgressRoutePolicy", "")
	if len(policies) == 0 {
		checks = append(checks, doctorCheck{Area: "wan", Name: "EgressRoutePolicy", Status: doctorSkip, Detail: "no EgressRoutePolicy resources configured"})
	} else {
		for _, res := range policies {
			checks = append(checks, doctorResourceCheck("wan", res, objectStatus(r.store, res.APIVersion, res.Kind, res.Metadata.Name), healthyPhases("Applied", "Active", "Ready")))
			spec, _ := res.EgressRoutePolicySpec()
			for _, candidate := range spec.Candidates {
				if candidate.HealthCheck == "" {
					continue
				}
				status := objectStatus(r.store, api.NetAPIVersion, "HealthCheck", candidate.HealthCheck)
				checks = append(checks, doctorNamedStatusCheck("wan", "HealthCheck/"+candidate.HealthCheck, status, healthyPhases("Healthy", "Applied", "Ready")))
			}
		}
	}
	if r.opts.Host {
		ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
		defer cancel()
		checks = append(checks,
			doctorCommandStatus("wan", runDiagnosticCommand(ctx, "default route ipv4", "ip", "-4", "route", "show", "default"), doctorFail, "install or repair the IPv4 default route"),
			doctorCommandStatus("wan", runDiagnosticCommand(ctx, "default route ipv6", "ip", "-6", "route", "show", "default"), doctorWarn, "install IPv6 default routing if this router should provide IPv6"),
		)
	} else {
		checks = append(checks, doctorHostSkipped("wan", "default routes"))
	}
	return checks
}

func (r doctorRunner) doctorDNS() []doctorCheck {
	resolvers := selectResources(r.router.Spec.Resources, "DNSResolver", "")
	if len(resolvers) == 0 {
		return []doctorCheck{{Area: "dns", Name: "DNSResolver", Status: doctorSkip, Detail: "no DNSResolver resources configured"}}
	}
	var checks []doctorCheck
	for _, res := range resolvers {
		checks = append(checks, doctorResourceCheck("dns", res, objectStatus(r.store, res.APIVersion, res.Kind, res.Metadata.Name), healthyPhases("Applied", "Active", "Ready")))
	}
	if r.opts.Host {
		ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
		defer cancel()
		name := firstNonEmpty(firstCSV(r.opts.Names), "example.com")
		checks = append(checks, doctorCommandStatus("dns", runDiagnosticCommand(ctx, "dig @127.0.0.1 "+name, "dig", "@127.0.0.1", name, "A", "+time=2", "+tries=1"), doctorFail, "check routerd-dns-resolver and local DNS listener"))
	} else {
		checks = append(checks, doctorHostSkipped("dns", "dig @127.0.0.1"))
	}
	return checks
}

func (r doctorRunner) doctorDSLite() []doctorCheck {
	tunnels := selectResources(r.router.Spec.Resources, "DSLiteTunnel", "")
	if len(tunnels) == 0 {
		return []doctorCheck{{Area: "dslite", Name: "DSLiteTunnel", Status: doctorSkip, Detail: "no DSLiteTunnel resources configured"}}
	}
	var checks []doctorCheck
	for _, res := range tunnels {
		status := objectStatus(r.store, res.APIVersion, res.Kind, res.Metadata.Name)
		resourceCheck := doctorResourceCheck("dslite", res, status, healthyPhases("Applied", "Active", "Ready", "Up"))
		checks = append(checks, resourceCheck)
		spec, _ := res.DSLiteTunnelSpec()
		aftr := firstNonEmpty(spec.AFTRFQDN, stringStatus(status, "aftrFQDN"), stringStatus(status, "aftrName"))
		device := firstNonEmpty(spec.TunnelName, stringStatus(status, "device"), stringStatus(status, "tunnelName"))
		selectedEvidence := ""
		if resourceCheck.Status == doctorPass {
			selectedEvidence = r.dsliteSelectedEvidence(res.Metadata.Name, device)
		}
		if r.opts.Host {
			ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
			if aftr != "" {
				checks = append(checks, doctorDSLiteCommandStatus(runDiagnosticCommand(ctx, "dig AFTR "+aftr, "dig", aftr, "AAAA", "+time=2", "+tries=1"), doctorWarn, "check AFTR DNS and DNSResolver forwarding", selectedEvidence))
			} else {
				checks = append(checks, doctorCheck{Area: "dslite", Name: "AFTR FQDN", Status: doctorSkip, Detail: "no AFTR FQDN in spec or status"})
			}
			if device != "" {
				checks = append(checks, doctorDSLiteCommandStatus(runDiagnosticCommand(ctx, "ip link show "+device, "ip", "link", "show", "dev", device), doctorFail, "check DSLiteTunnel rendering and tunnel creation", selectedEvidence))
			} else {
				checks = append(checks, doctorCheck{Area: "dslite", Name: "tunnel device", Status: doctorSkip, Detail: "no tunnel device in spec or status"})
			}
			cancel()
		} else {
			checks = append(checks, doctorHostSkipped("dslite", "AFTR DNS and tunnel device"))
		}
	}
	return checks
}

func (r doctorRunner) dsliteSelectedEvidence(tunnelName, device string) string {
	for _, policy := range selectResources(r.router.Spec.Resources, "EgressRoutePolicy", "") {
		policyStatus := objectStatus(r.store, policy.APIVersion, policy.Kind, policy.Metadata.Name)
		if !doctorStatusPass(policyStatus, healthyPhases("Applied", "Active", "Ready", "Healthy")) {
			continue
		}
		selectedCandidate := stringStatus(policyStatus, "selectedCandidate")
		selectedSource := stringStatus(policyStatus, "selectedSource")
		selectedDevice := stringStatus(policyStatus, "selectedDevice")
		spec, err := policy.EgressRoutePolicySpec()
		if err != nil {
			continue
		}
		candidate, candidateOK := selectedEgressCandidate(spec.Candidates, selectedCandidate)
		if !egressPolicySelectsDSLite(selectedSource, selectedDevice, candidate, candidateOK, tunnelName, device) {
			continue
		}
		detail := "selected via EgressRoutePolicy/" + policy.Metadata.Name + ", gatewayHealth-aligned"
		if candidateOK {
			if hc := firstNonEmpty(candidate.HealthCheck, selectedTargetHealthCheck(candidate)); hc != "" {
				hcStatus := objectStatus(r.store, api.NetAPIVersion, "HealthCheck", hc)
				if doctorStatusPass(hcStatus, healthyPhases("Healthy", "Applied", "Ready")) {
					detail += ", HealthCheck/" + hc + " healthy"
				}
			}
		}
		return detail
	}
	return ""
}

func selectedEgressCandidate(candidates []api.EgressRoutePolicyCandidate, selected string) (api.EgressRoutePolicyCandidate, bool) {
	if selected == "" {
		return api.EgressRoutePolicyCandidate{}, false
	}
	for _, candidate := range candidates {
		if candidate.Name == selected {
			return candidate, true
		}
	}
	return api.EgressRoutePolicyCandidate{}, false
}

func egressPolicySelectsDSLite(selectedSource, selectedDevice string, candidate api.EgressRoutePolicyCandidate, candidateOK bool, tunnelName, device string) bool {
	if resourceRefMatches(selectedSource, "DSLiteTunnel", tunnelName) {
		return true
	}
	if !candidateOK {
		return false
	}
	return egressCandidateUsesDSLite(candidate, tunnelName, device) || device != "" && selectedDevice == device
}

func egressCandidateUsesDSLite(candidate api.EgressRoutePolicyCandidate, tunnelName, device string) bool {
	if resourceRefMatches(candidate.Source, "DSLiteTunnel", tunnelName) {
		return true
	}
	for _, value := range []string{candidate.Interface, candidate.Device, candidate.EffectiveInterface()} {
		if value != "" && value == device {
			return true
		}
	}
	for _, target := range candidate.Targets {
		for _, value := range []string{target.Interface, target.OutboundInterface, target.EffectiveInterface()} {
			if value != "" && value == device {
				return true
			}
		}
	}
	return false
}

func selectedTargetHealthCheck(candidate api.EgressRoutePolicyCandidate) string {
	for _, target := range candidate.Targets {
		if target.HealthCheck != "" {
			return target.HealthCheck
		}
	}
	return ""
}

func resourceRefMatches(ref, kind, name string) bool {
	ref = strings.TrimSpace(ref)
	return ref == name || ref == kind+"/"+name || strings.HasSuffix(ref, "/"+kind+"/"+name)
}

func (r doctorRunner) doctorDHCPv6PD() []doctorCheck {
	pds := selectResources(r.router.Spec.Resources, "DHCPv6PrefixDelegation", "")
	if len(pds) == 0 {
		return []doctorCheck{{Area: "dhcpv6-pd", Name: "DHCPv6PrefixDelegation", Status: doctorSkip, Detail: "no DHCPv6PrefixDelegation resources configured"}}
	}
	var checks []doctorCheck
	for _, res := range pds {
		status := objectStatus(r.store, res.APIVersion, res.Kind, res.Metadata.Name)
		name := res.Kind + "/" + res.Metadata.Name
		phase := stringStatus(status, "phase")
		if len(status) == 0 {
			checks = append(checks, doctorCheck{Area: "dhcpv6-pd", Name: name, Status: doctorWarn, Detail: "status is missing", Remedy: "wait for routerd to observe DHCPv6-PD state"})
			continue
		}
		if phase != "Bound" {
			checks = append(checks, doctorCheck{Area: "dhcpv6-pd", Name: name, Status: doctorWarn, Detail: doctorStatusDetail(status), Remedy: "check WAN DHCPv6-PD reachability and client logs"})
			continue
		}
		prefix := firstNonEmpty(stringStatus(status, "currentPrefix"), stringStatus(status, "delegatedPrefix"), stringStatus(status, "prefix"))
		if prefix == "" {
			checks = append(checks, doctorCheck{Area: "dhcpv6-pd", Name: name, Status: doctorWarn, Detail: doctorStatusDetail(status), Remedy: "wait for delegated prefix status to be recorded"})
			continue
		}
		checks = append(checks, doctorCheck{Area: "dhcpv6-pd", Name: name, Status: doctorPass, Detail: "bound prefix " + prefix})
	}
	return checks
}

func (r doctorRunner) doctorNAT() []doctorCheck {
	rules := selectResources(r.router.Spec.Resources, "NAT44Rule", "")
	if len(rules) == 0 {
		return []doctorCheck{{Area: "nat", Name: "NAT44Rule", Status: doctorSkip, Detail: "no NAT44Rule resources configured"}}
	}
	var checks []doctorCheck
	natCounts := doctorResourceStatusCounts{}
	for _, res := range rules {
		status := objectStatus(r.store, res.APIVersion, res.Kind, res.Metadata.Name)
		checks = append(checks, doctorResourceCheck("nat", res, status, healthyPhases("Applied", "Active", "Ready")))
		natCounts.tally(status, healthyPhases("Applied", "Active", "Ready"))
	}
	if r.opts.Host {
		ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
		defer cancel()
		command := runDiagnosticCommand(ctx, "nft list table ip routerd_nat", "nft", "list", "table", "ip", "routerd_nat")
		extra := fmt.Sprintf("NAT44Rule active=%d pending=%d", natCounts.Active, natCounts.Pending)
		checks = append(checks, doctorNftCheckStatus("nat", command, "ip", "routerd_nat", doctorFail, "apply NAT44Rule resources or inspect nftables errors", extra))
	} else {
		checks = append(checks, doctorHostSkipped("nat", "nft routerd_nat"))
	}
	return checks
}

func (r doctorRunner) doctorFirewall() []doctorCheck {
	zones := selectResources(r.router.Spec.Resources, "FirewallZone", "")
	policies := selectResources(r.router.Spec.Resources, "FirewallPolicy", "")
	firewallResources := append(zones, policies...)
	if len(firewallResources) == 0 {
		return []doctorCheck{{Area: "firewall", Name: "FirewallZone/Policy", Status: doctorWarn, Detail: "no firewall zones or policy configured; router may be permissive", Remedy: "declare FirewallZone and FirewallPolicy resources"}}
	}
	var checks []doctorCheck
	zoneCounts := doctorResourceStatusCounts{}
	for _, res := range firewallResources {
		status := objectStatus(r.store, res.APIVersion, res.Kind, res.Metadata.Name)
		checks = append(checks, doctorResourceCheck("firewall", res, status, healthyPhases("Applied", "Active", "Ready")))
		if res.Kind == "FirewallZone" {
			zoneCounts.tally(status, healthyPhases("Applied", "Active", "Ready"))
		}
	}
	if r.opts.Host {
		ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
		defer cancel()
		command := runDiagnosticCommand(ctx, "nft list table inet routerd_filter", "nft", "list", "table", "inet", "routerd_filter")
		extra := fmt.Sprintf("FirewallZone active=%d", zoneCounts.Active)
		check := doctorNftCheckStatus("firewall", command, "inet", "routerd_filter", doctorFail, "apply firewall resources or inspect nftables errors", extra)
		if command.OK && (!strings.Contains(command.Stdout, "hook input") || !strings.Contains(command.Stdout, "policy drop")) {
			check.Status = doctorWarn
			check.Detail = appendDoctorDetail("routerd_filter exists but input policy drop was not found", "table=inet/routerd_filter")
			check.Detail = appendDoctorDetail(check.Detail, extra)
			check.Remedy = "check rendered firewall policy"
		}
		checks = append(checks, check)
	} else {
		checks = append(checks, doctorHostSkipped("firewall", "nft routerd_filter"))
	}
	return checks
}

func (r doctorRunner) doctorRollback() []doctorCheck {
	history, ok := r.store.(routerstate.GenerationHistoryReader)
	if !ok {
		return []doctorCheck{{Area: "rollback", Name: "generations", Status: doctorSkip, Detail: "state store does not expose generation history"}}
	}
	records, err := history.ListGenerations(1)
	if err != nil {
		return []doctorCheck{{Area: "rollback", Name: "generations", Status: doctorWarn, Detail: err.Error(), Remedy: "check routerd state database permissions"}}
	}
	if len(records) == 0 {
		return []doctorCheck{{Area: "rollback", Name: "generations", Status: doctorWarn, Detail: "no saved generations found", Remedy: "run a successful routerd apply before relying on rollback"}}
	}
	return []doctorCheck{{Area: "rollback", Name: "generations", Status: doctorPass, Detail: fmt.Sprintf("latest generation %d", records[0].Generation)}}
}

func (r doctorRunner) doctorDisk() []doctorCheck {
	if !r.opts.Host {
		return []doctorCheck{doctorHostSkipped("disk", "df /var/lib/routerd /run/routerd")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
	defer cancel()
	command := runDiagnosticCommand(ctx, "df routerd runtime", "df", "-Pk", "/var/lib/routerd", "/run/routerd")
	if !command.OK {
		return []doctorCheck{{Area: "disk", Name: command.Name, Status: doctorWarn, Detail: firstNonEmpty(command.Error, command.Output), Remedy: "check routerd runtime and state paths"}}
	}
	return doctorDFChecks(command.Output)
}

func (r doctorRunner) doctorMgmt() []doctorCheck {
	var checks []doctorCheck
	if len(selectResources(r.router.Spec.Resources, "ManagementAccess", "")) > 0 {
		findings := config.CheckManagementPlane(r.router)
		if len(findings) == 0 {
			return []doctorCheck{{Area: "mgmt", Name: "ManagementAccess", Status: doctorPass, Detail: "management plane checks passed"}}
		}
		for _, finding := range findings {
			status := doctorWarn
			if finding.Severity == config.ManagementPlaneFail {
				status = doctorFail
			}
			checks = append(checks, doctorCheck{Area: "mgmt", Name: finding.Resource, Status: status, Detail: finding.Message, Remedy: finding.Remedy})
		}
		return checks
	}
	mgmtIfaces := r.mgmtInterfaces()
	if len(mgmtIfaces) == 0 {
		checks = append(checks, doctorCheck{Area: "mgmt", Name: "management interface", Status: doctorSkip, Detail: "no obvious management interface in config"})
	} else {
		checks = append(checks, doctorCheck{Area: "mgmt", Name: "management interface", Status: doctorPass, Detail: strings.Join(mgmtIfaces, ",")})
	}
	consoles := selectResources(r.router.Spec.Resources, "WebConsole", "")
	if len(consoles) == 0 {
		checks = append(checks, doctorCheck{Area: "mgmt", Name: "WebConsole", Status: doctorSkip, Detail: "no WebConsole resource configured"})
		return checks
	}
	for _, res := range consoles {
		spec, _ := res.WebConsoleSpec()
		name := "WebConsole/" + res.Metadata.Name
		listen := firstNonEmpty(spec.ListenAddress, stringStatus(objectStatus(r.store, res.APIVersion, res.Kind, res.Metadata.Name), "listenAddress"))
		if listen == "" {
			checks = append(checks, doctorCheck{Area: "mgmt", Name: name, Status: doctorSkip, Detail: "listen address is derived or unavailable"})
			continue
		}
		if listen == "0.0.0.0" || listen == "::" {
			checks = append(checks, doctorCheck{Area: "mgmt", Name: name, Status: doctorWarn, Detail: "WebConsole listens on all addresses", Remedy: "bind WebConsole to a management or LAN address"})
			continue
		}
		checks = append(checks, doctorCheck{Area: "mgmt", Name: name, Status: doctorPass, Detail: "listenAddress=" + listen})
	}
	return checks
}

func (r doctorRunner) mgmtInterfaces() []string {
	var out []string
	mgmtNames := map[string]bool{}
	for _, res := range selectResources(r.router.Spec.Resources, "FirewallZone", "") {
		spec, err := res.FirewallZoneSpec()
		if err != nil || spec.Role != "mgmt" {
			continue
		}
		for _, iface := range spec.Interfaces {
			mgmtNames[strings.TrimPrefix(iface, "Interface/")] = true
		}
	}
	for _, res := range selectResources(r.router.Spec.Resources, "Interface", "") {
		name := res.Metadata.Name
		if mgmtNames[name] || strings.Contains(strings.ToLower(name), "mgmt") || strings.Contains(strings.ToLower(name), "admin") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func doctorResourceCheck(area string, res api.Resource, status map[string]any, pass map[string]bool) doctorCheck {
	return doctorNamedStatusCheck(area, res.Kind+"/"+res.Metadata.Name, status, pass)
}

func doctorNamedStatusCheck(area, name string, status map[string]any, pass map[string]bool) doctorCheck {
	if len(status) == 0 {
		return doctorCheck{Area: area, Name: name, Status: doctorWarn, Detail: "status is missing", Remedy: "wait for routerd to reconcile this resource"}
	}
	if health := stringStatus(status, "health"); health != "" {
		switch strings.ToLower(health) {
		case "ok", "healthok", "healthy", "pass", "passing":
			return doctorCheck{Area: area, Name: name, Status: doctorPass, Detail: doctorStatusDetail(status)}
		case "degraded", "healthdegraded", "warning", "warn":
			return doctorCheck{Area: area, Name: name, Status: doctorWarn, Detail: doctorStatusDetail(status), Remedy: doctorRemedy(status)}
		case "fail", "healthfail", "failed", "error", "unhealthy":
			return doctorCheck{Area: area, Name: name, Status: doctorFail, Detail: doctorStatusDetail(status), Remedy: doctorRemedy(status)}
		}
	}
	phase := stringStatus(status, "phase")
	switch {
	case pass[phase]:
		return doctorCheck{Area: area, Name: name, Status: doctorPass, Detail: doctorStatusDetail(status)}
	case strings.EqualFold(phase, "Error") || strings.EqualFold(phase, "Failed") || strings.EqualFold(phase, "Unhealthy"):
		return doctorCheck{Area: area, Name: name, Status: doctorFail, Detail: doctorStatusDetail(status), Remedy: doctorRemedy(status)}
	case phase == "":
		return doctorCheck{Area: area, Name: name, Status: doctorWarn, Detail: doctorStatusDetail(status), Remedy: "status has no phase; inspect routerd state"}
	default:
		return doctorCheck{Area: area, Name: name, Status: doctorWarn, Detail: doctorStatusDetail(status), Remedy: doctorRemedy(status)}
	}
}

func doctorCommandStatus(area string, command diagnoseCommandCheck, failStatus, remedy string) doctorCheck {
	if command.OK {
		detail := command.Output
		if detail == "" {
			detail = "command succeeded"
		}
		return doctorCheck{Area: area, Name: command.Name, Status: doctorPass, Detail: oneLine(detail)}
	}
	return doctorCheck{Area: area, Name: command.Name, Status: failStatus, Detail: firstNonEmpty(command.Error, oneLine(command.Output)), Remedy: remedy}
}

// doctorResourceStatusCounts tallies resource status phases for compact
// reporting alongside nftables checks. Active counts pass-mapped phases,
// Pending counts everything else with a non-empty status, and Missing counts
// resources without observed status.
type doctorResourceStatusCounts struct {
	Active  int
	Pending int
	Missing int
}

func (c *doctorResourceStatusCounts) tally(status map[string]any, pass map[string]bool) {
	if len(status) == 0 {
		c.Missing++
		return
	}
	if pass[stringStatus(status, "phase")] {
		c.Active++
		return
	}
	c.Pending++
}

// doctorNftCheckStatus produces a doctorCheck from an nft command invocation.
// When the command exits non-zero but the requested table still appears in
// stdout the check is downgraded to warn (the listing is usable, but stderr
// flagged something). When stdout is empty and the command failed, detail
// records command/exit/stderr/stdout for triage. Successful checks append an
// optional structured detail (e.g. resource counts).
func doctorNftCheckStatus(area string, command diagnoseCommandCheck, family, table, failStatus, remedy, extra string) doctorCheck {
	tableLabel := "table=" + family + "/" + table
	if command.OK {
		detail := strings.TrimSpace(command.Stdout)
		if detail == "" {
			detail = "command succeeded"
		}
		base := appendDoctorDetail(tableLabel, oneLine(detail))
		if extra != "" {
			base = appendDoctorDetail(base, extra)
		}
		check := doctorCheck{Area: area, Name: command.Name, Status: doctorPass, Detail: base}
		if stderr := strings.TrimSpace(command.Stderr); stderr != "" {
			check.Detail = appendDoctorDetail(check.Detail, "stderr="+truncateForDetail(stderr, 200))
		}
		return check
	}
	status := failStatus
	tableMarker := "table " + family + " " + table
	if strings.Contains(command.Stdout, tableMarker) {
		status = doctorWarn
	}
	detail := nftFailureDetail(command, tableLabel)
	if extra != "" {
		detail = appendDoctorDetail(detail, extra)
	}
	return doctorCheck{Area: area, Name: command.Name, Status: status, Detail: detail, Remedy: remedy}
}

func nftFailureDetail(command diagnoseCommandCheck, tableLabel string) string {
	parts := []string{tableLabel}
	if command.Command != "" {
		parts = append(parts, "cmd="+command.Command)
	}
	parts = append(parts, fmt.Sprintf("exit=%d", command.ExitCode))
	if stderr := strings.TrimSpace(command.Stderr); stderr != "" {
		parts = append(parts, "stderr="+truncateForDetail(stderr, 200))
	}
	if stdout := strings.TrimSpace(command.Stdout); stdout != "" {
		parts = append(parts, "stdout="+truncateForDetail(stdout, 200))
	}
	return strings.Join(parts, " ")
}

func truncateForDetail(value string, max int) string {
	value = strings.ReplaceAll(value, "\n", " | ")
	value = strings.TrimSpace(value)
	if max > 0 && len(value) > max {
		return value[:max] + "..."
	}
	return value
}

func doctorHostSkipped(area, name string) doctorCheck {
	return doctorCheck{Area: area, Name: name, Status: doctorSkip, Detail: "host commands disabled by --no-host"}
}

func healthyPhases(phases ...string) map[string]bool {
	out := map[string]bool{}
	for _, phase := range phases {
		out[phase] = true
	}
	return out
}

func doctorStatusDetail(status map[string]any) string {
	if len(status) == 0 {
		return "status is missing"
	}
	var parts []string
	for _, key := range []string{"phase", "health", "reason", "message", "waiting", "selectedCandidate", "selectedDevice", "currentPrefix"} {
		if value, ok := status[key]; ok && fmt.Sprint(value) != "" {
			parts = append(parts, key+"="+fmt.Sprint(value))
		}
	}
	if len(parts) == 0 {
		return compactDiagnoseMap(status)
	}
	return strings.Join(parts, ",")
}

func doctorRemedy(status map[string]any) string {
	if reason := stringStatus(status, "reason"); reason != "" {
		return "inspect routerd logs for " + reason
	}
	if waiting := stringStatus(status, "waiting"); waiting != "" {
		return "wait for or repair dependency " + waiting
	}
	return "inspect routerd status and logs for this resource"
}

func doctorStatusPass(status map[string]any, pass map[string]bool) bool {
	return doctorNamedStatusCheck("", "", status, pass).Status == doctorPass
}

func doctorDSLiteCommandStatus(command diagnoseCommandCheck, failStatus, remedy, selectedEvidence string) doctorCheck {
	check := doctorCommandStatus("dslite", command, failStatus, remedy)
	if selectedEvidence == "" {
		return check
	}
	if check.Status == doctorPass {
		check.Detail = appendDoctorDetail(check.Detail, selectedEvidence)
		return check
	}
	check.Status = doctorPass
	check.Detail = appendDoctorDetail(selectedEvidence, "host observation ignored: "+firstNonEmpty(command.Error, oneLine(command.Output)))
	check.Remedy = ""
	return check
}

func appendDoctorDetail(base, detail string) string {
	base = strings.TrimSpace(base)
	detail = strings.TrimSpace(detail)
	switch {
	case base == "":
		return detail
	case detail == "":
		return base
	default:
		return base + "; " + detail
	}
}

func stringStatus(status map[string]any, key string) string {
	if status == nil {
		return ""
	}
	value, ok := status[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstCSV(value string) string {
	values := splitCSV(value)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func oneLine(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " | ")
	if len(value) > 240 {
		return value[:240] + "..."
	}
	return value
}

func doctorDFChecks(output string) []doctorCheck {
	var checks []doctorCheck
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		avail, _ := strconv.ParseInt(fields[3], 10, 64)
		usedPctText := strings.TrimSuffix(fields[4], "%")
		usedPct, _ := strconv.Atoi(usedPctText)
		mount := fields[len(fields)-1]
		status := doctorPass
		remedy := ""
		if usedPct >= 98 || avail < 64*1024 {
			status = doctorFail
			remedy = "free disk space before applying router changes"
		} else if usedPct >= 90 || avail < 256*1024 {
			status = doctorWarn
			remedy = "free disk space or move routerd state to a larger filesystem"
		}
		checks = append(checks, doctorCheck{
			Area:   "disk",
			Name:   "df " + mount,
			Status: status,
			Detail: fmt.Sprintf("available=%dKiB used=%d%%", avail, usedPct),
			Remedy: remedy,
		})
	}
	if len(checks) == 0 {
		return []doctorCheck{{Area: "disk", Name: "df", Status: doctorWarn, Detail: "could not parse df output", Remedy: "check df output manually"}}
	}
	return checks
}

func summarizeDoctorChecks(checks []doctorCheck) doctorSummary {
	summary := doctorSummary{Overall: doctorPass}
	for _, check := range checks {
		switch check.Status {
		case doctorPass:
			summary.Pass++
		case doctorWarn:
			summary.Warn++
		case doctorFail:
			summary.Fail++
		case doctorSkip:
			summary.Skip++
		}
	}
	if summary.Fail > 0 {
		summary.Overall = doctorFail
	} else if summary.Warn > 0 {
		summary.Overall = doctorWarn
	} else if summary.Pass == 0 && summary.Skip > 0 {
		summary.Overall = doctorSkip
	}
	return summary
}

func writeDoctorReport(stdout io.Writer, report doctorReport, output string) error {
	switch output {
	case "", "table":
		return writeDoctorTable(stdout, report)
	case "json":
		return writeJSON(stdout, report)
	case "yaml":
		return writeYAML(stdout, report)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeDoctorTable(stdout io.Writer, report doctorReport) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "DOCTOR\t%s\tpass=%d warn=%d fail=%d skip=%d\n", strings.ToUpper(report.Summary.Overall), report.Summary.Pass, report.Summary.Warn, report.Summary.Fail, report.Summary.Skip)
	fmt.Fprintln(w, "AREA\tSTATUS\tCHECK\tDETAIL\tREMEDY")
	for _, check := range report.Checks {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", check.Area, strings.ToUpper(check.Status), check.Name, displayCell(check.Detail), displayCell(check.Remedy))
	}
	return w.Flush()
}
