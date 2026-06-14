// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/hybrid"
	"github.com/imksoo/routerd/pkg/platform"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	doctorPass = "pass"
	doctorWarn = "warn"
	doctorFail = "fail"
	doctorSkip = "skip"

	doctorObservedEventType             = "routerd.client.ipv4.observed"
	doctorExpiredEventType              = "routerd.client.ipv4.expired"
	doctorFederationSourceProviderOwner = "provider-discovery"
)

var doctorNow = time.Now

var doctorMACAddressPattern = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{2}:){5}[0-9a-f]{2}\b`)

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
	Summary  doctorSummary         `json:"summary" yaml:"summary"`
	Checks   []doctorCheck         `json:"checks" yaml:"checks"`
	Incident *doctorIncidentReport `json:"incident,omitempty" yaml:"incident,omitempty"`
}

type doctorIncidentReport struct {
	GeneratedAt    string                        `json:"generatedAt" yaml:"generatedAt"`
	Status         *controlapi.Status            `json:"status,omitempty" yaml:"status,omitempty"`
	Runtime        *controlapi.RuntimeStats      `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	ObjectStatuses []routerstate.ObjectStatus    `json:"objectStatuses,omitempty" yaml:"objectStatuses,omitempty"`
	PluginRuns     []routerstate.PluginRunRecord `json:"pluginRuns,omitempty" yaml:"pluginRuns,omitempty"`
	Events         []routerstate.StoredEvent     `json:"events,omitempty" yaml:"events,omitempty"`
	Commands       []diagnoseCommandCheck        `json:"commands,omitempty" yaml:"commands,omitempty"`
	Error          string                        `json:"error,omitempty" yaml:"error,omitempty"`
}

type doctorRunner struct {
	opts   diagnoseOptions
	router *api.Router
	store  routerstate.Store
}

var doctorAreas = []string{"wan", "dns", "dslite", "dhcpv6-pd", "nat", "firewall", "rollback", "disk", "mgmt", "reconcile", "runtime", "dynamic", "routes", "plugin", "hybrid", "sam"}

// doctorReconcileWarnThreshold is the total historical error count (across all
// controllers) that promotes the reconcile area to warn. Current controller
// failures promote the check to fail.
const (
	doctorReconcileWarnThreshold = 1
)

// reconcileStatusFetcher allows tests to stub the controllers fetch.
var reconcileStatusFetcher = fetchReconcileControllers

// doctorStatusFetcher allows tests to stub the main status fetch.
var doctorStatusFetcher = fetchStatus

// runtimeStatsFetcher allows tests to stub the runtime-stats fetch.
var runtimeStatsFetcher = fetchRuntimeStats

var doctorRunDiagnosticCommand = runDiagnosticCommand
var doctorCurrentOS = platform.CurrentOS

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
	if opts.Probe != "" {
		return doctorProbeCommand(opts, stdout)
	}
	if opts.Target != "" && opts.Target != "incident" && !validDoctorArea(opts.Target) {
		return fmt.Errorf("unknown doctor area %q", opts.Target)
	}
	router, store, err := loadDiagnoseInputs(opts)
	if err != nil {
		if opts.Target == "dynamic" || opts.Target == "plugin" {
			report := doctorReport{Checks: []doctorCheck{{Area: opts.Target, Name: "inputs", Status: doctorSkip, Detail: err.Error()}}}
			report.Summary = summarizeDoctorChecks(report.Checks)
			return writeDoctorReport(stdout, report, opts.Output)
		}
		return err
	}
	runner := doctorRunner{opts: opts, router: router, store: store}
	collectChecks := opts.Target != "incident"
	areas := doctorAreas
	if opts.Target != "" && opts.Target != "incident" {
		areas = []string{opts.Target}
	}
	report := doctorReport{}
	if collectChecks {
		for _, area := range areas {
			report.Checks = append(report.Checks, runner.runArea(area)...)
		}
	}
	if opts.Incident || opts.Target == "incident" {
		incident := runner.doctorIncidentDump()
		report.Incident = &incident
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

func doctorProbeCommand(opts diagnoseOptions, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(opts.Socket).Probe(ctx, controlapi.ProbeRequest{Subject: opts.Probe, Target: opts.Target})
	if err != nil {
		return err
	}
	switch opts.Output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "PROBE\t%s\t%s\n", result.Subject, displayCell(result.Target))
		fmt.Fprintln(w, "NAME\tSTATUS\tDETAIL")
		for _, check := range result.Checks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", check.Name, check.Status, displayCell(check.Detail))
		}
		return w.Flush()
	case "json":
		return writeJSON(stdout, result)
	case "yaml":
		return writeYAML(stdout, result)
	default:
		return fmt.Errorf("unsupported output %q", opts.Output)
	}
}

func fetchStatus(socketPath string, timeout time.Duration) (*controlapi.Status, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return controlapi.NewUnixClient(socketPath).Status(ctx)
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
	case "dynamic":
		return r.doctorDynamic()
	case "routes":
		return r.doctorRoutes()
	case "plugin":
		return r.doctorPlugin()
	case "hybrid":
		return r.doctorHybrid()
	case "sam":
		return r.doctorSAM()
	default:
		return []doctorCheck{{Area: area, Name: "area", Status: doctorSkip, Detail: "unknown area"}}
	}
}

func (r doctorRunner) doctorIncidentDump() doctorIncidentReport {
	dump := doctorIncidentReport{GeneratedAt: doctorNow().UTC().Format(time.RFC3339)}
	if status, err := doctorStatusFetcher(r.opts.Socket, r.opts.Timeout); err == nil {
		dump.Status = status
	} else {
		dump.Error = appendDoctorDetail(dump.Error, "status: "+err.Error())
	}
	if runtime, err := runtimeStatsFetcher(r.opts.Socket, r.opts.Timeout); err == nil {
		dump.Runtime = runtime
	} else {
		dump.Error = appendDoctorDetail(dump.Error, "runtime: "+err.Error())
	}
	if lister, ok := r.store.(routerstate.ObjectStatusLister); ok {
		objectStatuses, err := lister.ListObjectStatuses()
		if err != nil {
			dump.Error = appendDoctorDetail(dump.Error, "object status list: "+err.Error())
		} else {
			dump.ObjectStatuses = objectStatuses
		}
	}
	if lister, ok := r.store.(routerstate.PluginRunLister); ok {
		pluginRuns, err := lister.ListPluginRuns("")
		if err != nil {
			dump.Error = appendDoctorDetail(dump.Error, "plugin run list: "+err.Error())
		} else {
			dump.PluginRuns = pluginRuns
			if len(dump.PluginRuns) > 25 {
				dump.PluginRuns = dump.PluginRuns[:25]
			}
		}
	}
	if lister, ok := r.store.(routerstate.EventLister); ok {
		events, err := lister.ListEvents(routerstate.EventQuery{Limit: 50})
		if err != nil {
			dump.Error = appendDoctorDetail(dump.Error, "events list: "+err.Error())
		} else {
			dump.Events = events
			if len(dump.Events) > 50 {
				dump.Events = dump.Events[:50]
			}
		}
	}
	if r.opts.Host {
		dump.Commands = collectDoctorIncidentCommands(r)
	}
	return dump
}

func collectDoctorIncidentCommands(r doctorRunner) []diagnoseCommandCheck {
	if doctorCurrentOS() != platform.OSLinux {
		return []diagnoseCommandCheck{{
			Name:  "host command snapshots",
			OK:    false,
			Error: "host command snapshots are linux-only",
		}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
	defer cancel()
	commands := []struct {
		label string
		cmd   string
		args  []string
	}{
		{label: "ip -4 route show table all", cmd: "ip", args: []string{"-4", "route", "show", "table", "all"}},
		{label: "ip -6 route show table all", cmd: "ip", args: []string{"-6", "route", "show", "table", "all"}},
		{label: "ip -4 rule show", cmd: "ip", args: []string{"-4", "rule", "show"}},
		{label: "ip -4 neigh show", cmd: "ip", args: []string{"-4", "neigh", "show"}},
		{label: "ip -s -s -d link show", cmd: "ip", args: []string{"-s", "-s", "-d", "link", "show"}},
		{label: "sysctl net.ipv4.ip_forward", cmd: "sysctl", args: []string{"net.ipv4.ip_forward"}},
		{label: "sysctl net.ipv4.conf.all.rp_filter", cmd: "sysctl", args: []string{"net.ipv4.conf.all.rp_filter"}},
		{label: "sysctl net.ipv4.conf.all.src_valid_mark", cmd: "sysctl", args: []string{"net.ipv4.conf.all.src_valid_mark"}},
		{label: "journalctl -u routerd", cmd: "journalctl", args: []string{"-u", "routerd", "--no-pager", "-n", "120"}},
		{label: "stat /tmp /var/tmp", cmd: "stat", args: []string{"-c", "%n %a %u %g %F", "/tmp", "/var/tmp"}},
	}
	out := make([]diagnoseCommandCheck, 0, len(commands))
	for _, command := range commands {
		out = append(out, doctorRunDiagnosticCommand(ctx, command.label, command.cmd, command.args...))
	}
	return out
}

func (r doctorRunner) doctorSAM() []doctorCheck {
	if r.router == nil {
		return []doctorCheck{{Area: "sam", Name: "startup config", Status: doctorSkip, Detail: "startup config unavailable"}}
	}
	pools := selectResources(r.router.Spec.Resources, "MobilityPool", "")
	if len(pools) == 0 {
		return []doctorCheck{{Area: "sam", Name: "MobilityPool", Status: doctorSkip, Detail: "no MobilityPool configured"}}
	}
	var checks []doctorCheck
	for _, res := range pools {
		status := objectStatus(r.store, res.APIVersion, res.Kind, res.Metadata.Name)
		checks = append(checks, doctorResourceCheck("sam", res, status, healthyPhases("Ready", "BGPPlanned")))
		poolSpec, err := res.MobilityPoolSpec()
		if err != nil {
			checks = append(checks, doctorCheck{Area: "sam", Name: "MobilityPool/" + res.Metadata.Name + " federation discovery", Status: doctorFail, Detail: err.Error(), Remedy: "fix MobilityPool spec and retry"})
			continue
		}
		checks = append(checks, r.doctorSAMFederationDiscoveryChecks(res.Metadata.Name, poolSpec, status)...)
		checks = append(checks, doctorSAMOwnershipConflictCheck(res.Metadata.Name, status))
		checks = append(checks, r.doctorSAMOwnerTableRouteChecks(res.Metadata.Name, poolSpec, status)...)
		checks = append(checks, r.doctorSAMBGPDeliveryChecks(res)...)
		phase := stringStatus(status, "providerActionPhase")
		if phase == "Failed" {
			errMsg := stringStatus(status, "providerActionError")
			detail := "provider action failed"
			if errMsg != "" {
				detail += ": " + errMsg
			}
			checks = append(checks, doctorCheck{
				Area:   "sam",
				Name:   "MobilityPool/" + res.Metadata.Name + " provider-action",
				Status: doctorWarn,
				Detail: detail,
				Remedy: "check provider API limits (e.g. secondary IP quota) and retry",
			})
		}
	}
	return checks
}

func (r doctorRunner) doctorSAMBGPDeliveryChecks(pool api.Resource) []doctorCheck {
	spec, err := pool.MobilityPoolSpec()
	if err != nil {
		return []doctorCheck{{
			Area:   "sam",
			Name:   "MobilityPool/" + pool.Metadata.Name + " BGP delivery",
			Status: doctorFail,
			Detail: "invalid MobilityPool spec: " + err.Error(),
			Remedy: "fix MobilityPool spec before accepting SAM dataplane readiness",
		}}
	}
	mode := strings.TrimSpace(spec.DeliveryPolicy.Mode)
	if mode != "" && mode != "bgp" {
		return nil
	}
	routerRefs := r.samTransportBGPRouterRefs()
	if len(routerRefs) == 0 {
		return nil
	}
	checks := make([]doctorCheck, 0, len(routerRefs))
	for _, routerName := range routerRefs {
		status := objectStatus(r.store, api.NetAPIVersion, "BGPRouter", routerName)
		name := "MobilityPool/" + pool.Metadata.Name + " BGP delivery BGPRouter/" + routerName
		if len(status) == 0 {
			checks = append(checks, doctorCheck{
				Area:   "sam",
				Name:   name,
				Status: doctorWarn,
				Detail: "BGPRouter status is missing",
				Remedy: "wait for routerd-bgp observation before accepting BGP-delivery SAM dataplane readiness",
			})
			continue
		}
		phase := stringStatus(status, "phase")
		established := statusInt(status["establishedPeers"])
		fibMissing := statusInt(status["fibMissingRoutes"])
		fibUnsupported := statusInt(status["fibUnsupportedRoutes"])
		detail := appendDoctorDetail("phase="+firstNonEmpty(phase, "missing"), fmt.Sprintf("establishedPeers=%d fibMissingRoutes=%d fibUnsupportedRoutes=%d", established, fibMissing, fibUnsupported))
		check := doctorCheck{
			Area:   "sam",
			Name:   name,
			Status: doctorPass,
			Detail: detail,
		}
		switch {
		case phase == "":
			check.Status = doctorWarn
			check.Remedy = "wait for routerd-bgp to publish BGPRouter phase before accepting SAM dataplane readiness"
		case !strings.EqualFold(phase, "Established"):
			check.Status = doctorFail
			check.Remedy = "repair BGP peer establishment before accepting BGP-delivery SAM dataplane readiness"
		case established == 0:
			check.Status = doctorFail
			check.Remedy = "repair BGP peer establishment before accepting BGP-delivery SAM dataplane readiness"
		case fibMissing > 0 || fibUnsupported > 0:
			check.Status = doctorFail
			check.Remedy = "repair BGP FIB installation before accepting BGP-delivery SAM dataplane readiness"
		}
		checks = append(checks, check)
	}
	return checks
}

func (r doctorRunner) samTransportBGPRouterRefs() []string {
	seen := map[string]bool{}
	var out []string
	if r.router == nil {
		return out
	}
	for _, res := range r.router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "SAMTransportProfile" {
			continue
		}
		spec, err := res.SAMTransportProfileSpec()
		if err != nil {
			continue
		}
		ref := strings.TrimSpace(spec.BGP.RouterRef)
		if ref == "" {
			continue
		}
		kind, name, ok := strings.Cut(ref, "/")
		if !ok {
			name = ref
			kind = "BGPRouter"
		}
		if kind != "BGPRouter" || strings.TrimSpace(name) == "" {
			continue
		}
		name = strings.TrimSpace(name)
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (r doctorRunner) doctorSAMFederationDiscoveryChecks(pool string, poolSpec api.MobilityPoolSpec, status map[string]any) []doctorCheck {
	label := "MobilityPool/" + pool + " federation discovery"
	if r.router == nil {
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorSkip, Detail: "startup config unavailable"}}
	}
	groupRef := strings.TrimSpace(poolSpec.GroupRef)
	if groupRef == "" {
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorWarn, Detail: "groupRef is empty; federation checks skipped"}}
	}
	groupSpec, ok, err := doctorSAMEventGroupSpec(r.router, groupRef)
	if err != nil {
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorFail, Detail: err.Error(), Remedy: "fix EventGroup spec and retry"}}
	}
	if !ok {
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorSkip, Detail: "EventGroup " + groupRef + " not found"}}
	}
	selfNode := strings.TrimSpace(groupSpec.NodeName)
	if selfNode == "" {
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorFail, Detail: "EventGroup spec.nodeName is required", Remedy: "set EventGroup.nodeName to this node id"}}
	}
	peers := map[string]bool{}
	for _, member := range poolSpec.Members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		if nodeRef == "" || nodeRef == selfNode {
			continue
		}
		peers[nodeRef] = true
	}
	if len(peers) == 0 {
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorSkip, Detail: "no other MobilityPool members to monitor"}}
	}
	federationStore, ok := r.store.(routerstate.FederationEventStore)
	if !ok {
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorWarn, Detail: "state backend does not expose federation events"}}
	}
	now := doctorNow().UTC()
	events, err := federationStore.ListFederationEvents(groupRef, false, now.Unix())
	if err != nil {
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorFail, Detail: err.Error(), Remedy: "check state backend and retry"}}
	}
	seenByPeer := map[string]bool{}
	seenTotal := 0
	for _, ev := range events {
		source := strings.TrimSpace(ev.SourceNode)
		if source == "" || source == selfNode || !peers[source] {
			continue
		}
		t := strings.TrimSpace(ev.Type)
		if t != doctorObservedEventType && t != doctorExpiredEventType {
			continue
		}
		if strings.TrimSpace(ev.Payload["source"]) != doctorFederationSourceProviderOwner {
			continue
		}
		if strings.TrimSpace(ev.Payload["pool"]) != pool {
			continue
		}
		timeKey := ev.ObservedAt
		if timeKey.IsZero() {
			timeKey = now
		}
		if now.Sub(timeKey) > 15*time.Minute {
			continue
		}
		seenByPeer[source] = true
		seenTotal++
	}
	if seenTotal == 0 {
		if ok, detail := doctorSAMOwnershipTableCoversFederation(status); ok {
			return []doctorCheck{{Area: "sam", Name: label, Status: doctorPass, Detail: detail}}
		}
		missing := make([]string, 0, len(peers))
		for node := range peers {
			missing = append(missing, node)
		}
		sort.Strings(missing)
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorWarn, Detail: "no recent provider-discovery events for: " + strings.Join(missing, ",") + ", from peers"}}
	}
	missing := make([]string, 0, len(peers)-len(seenByPeer))
	for node := range peers {
		if !seenByPeer[node] {
			missing = append(missing, node)
		}
	}
	if len(missing) == 0 {
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorPass, Detail: fmt.Sprintf("received provider-discovery updates from %d peer(s)", len(peers))}}
	}
	if ok, detail := doctorSAMOwnershipTableCoversFederation(status); ok {
		sort.Strings(missing)
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorPass, Detail: detail + "; stale/missing event peers=" + strings.Join(missing, ",")}}
	}
	sort.Strings(missing)
	return []doctorCheck{{Area: "sam", Name: label, Status: doctorWarn, Detail: "stale/missing provider-discovery updates from: " + strings.Join(missing, ",")}}
}

func doctorSAMOwnershipTableCoversFederation(status map[string]any) (bool, string) {
	if !strings.EqualFold(stringStatus(status, "ownershipResolverPhase"), "Resolved") {
		return false, ""
	}
	if !boolStatus(status, "bgpRIBObserved") {
		return false, ""
	}
	rows, ok := status["ownershipResolverOwnerTable"].([]any)
	if !ok {
		if typed, typedOK := status["ownershipResolverOwnerTable"].([]map[string]any); typedOK {
			rows = make([]any, 0, len(typed))
			for _, row := range typed {
				rows = append(rows, row)
			}
		}
	}
	if len(rows) == 0 {
		return false, ""
	}
	ready := 0
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		state := stringStatus(row, "state")
		if state == "" || strings.EqualFold(state, "OK") || strings.EqualFold(state, "Resolved") {
			ready++
		}
	}
	if ready == 0 {
		return false, ""
	}
	return true, fmt.Sprintf("ownership resolver is using BGP/provider owner table (%d ready row(s)); provider-discovery event freshness is advisory", ready)
}

func boolStatus(status map[string]any, key string) bool {
	switch value := status[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "true")
	}
}

func doctorSAMEventGroupSpec(router *api.Router, group string) (api.EventGroupSpec, bool, error) {
	for _, res := range router.Spec.Resources {
		if res.Kind != "EventGroup" || strings.TrimSpace(res.Metadata.Name) != strings.TrimSpace(group) {
			continue
		}
		spec, err := res.EventGroupSpec()
		if err != nil {
			return api.EventGroupSpec{}, false, err
		}
		return spec, true, nil
	}
	return api.EventGroupSpec{}, false, nil
}

func doctorSAMOwnershipConflictCheck(pool string, status map[string]any) doctorCheck {
	label := "MobilityPool/" + pool + " ownership conflicts"
	if strings.EqualFold(stringStatus(status, "ownershipResolverPhase"), "Conflict") || statusInt(status["ownershipResolverConflictCount"]) > 0 {
		conflicts := statusMaps(status["ownershipResolverConflicts"])
		count := statusInt(status["ownershipResolverConflictCount"])
		if len(conflicts) > count {
			count = len(conflicts)
		}
		detail := fmt.Sprintf("%d ownership conflict(s)", count)
		if len(conflicts) > 0 {
			detail = appendDoctorDetail(detail, doctorSAMOwnerRowsSummary(conflicts, 3))
		} else if reason := stringStatus(status, "ownershipResolverReason"); reason != "" {
			detail = appendDoctorDetail(detail, reason)
		}
		return doctorCheck{
			Area:   "sam",
			Name:   label,
			Status: doctorFail,
			Detail: detail,
			Remedy: "fix duplicate /32 ownership before applying provider capture actions; inspect ownershipResolverOwnerTable",
		}
	}
	if len(status) == 0 {
		return doctorCheck{Area: "sam", Name: label, Status: doctorSkip, Detail: "MobilityPool status unavailable"}
	}
	return doctorCheck{Area: "sam", Name: label, Status: doctorPass, Detail: "no ownership conflicts"}
}

func (r doctorRunner) doctorSAMOwnerTableRouteChecks(pool string, poolSpec api.MobilityPoolSpec, status map[string]any) []doctorCheck {
	label := "MobilityPool/" + pool + " owner-table route drift"
	rows := statusMaps(status["ownershipResolverOwnerTable"])
	if len(rows) == 0 {
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorSkip, Detail: "ownershipResolverOwnerTable unavailable"}}
	}
	if !r.opts.Host {
		return []doctorCheck{doctorHostSkipped("sam", label)}
	}
	var checks []doctorCheck
	ctx := context.Background()
	snapshotCommand := doctorRunDiagnosticCommand(ctx, "ip -4 route show table main", "ip", "-4", "route", "show", "table", "main")
	actualRoutes := parseDoctorIPv4MainRoutes(snapshotCommand.Stdout)
	if !snapshotCommand.OK {
		checks = append(checks, doctorCheck{Area: "sam", Name: label + " actual FIB snapshot", Status: doctorWarn, Detail: firstNonEmpty(snapshotCommand.Error, oneLine(snapshotCommand.Output), "main table route snapshot unavailable"), Remedy: "inspect ip -4 route show table main"})
	}
	expectedPrefixes := map[string]bool{}
	for _, row := range rows {
		address := strings.TrimSpace(fmt.Sprint(row["address"]))
		if normalized := normalizeDoctorIPv4RoutePrefix(address); normalized != "" {
			expectedPrefixes[normalized] = true
		}
		if address == "" || !doctorSAMOwnerRowNeedsLocalFIB(row) {
			continue
		}
		ip := strings.TrimSuffix(address, "/32")
		if ip == "" || strings.Contains(ip, "/") {
			continue
		}
		name := label + " " + address
		actual := actualRoutes[normalizeDoctorIPv4RoutePrefix(address)]
		command := doctorRunDiagnosticCommand(ctx, "ip route get "+ip, "ip", "-4", "route", "get", ip)
		if !command.OK {
			if badLine := doctorSAMForbiddenLocalOwnerRoute(actual); badLine != "" {
				checks = append(checks, doctorCheck{Area: "sam", Name: name, Status: doctorFail, Detail: appendDoctorDetail("expected local/provider-owned route, actual "+badLine, "actual FIB snapshot has stale BGP/SAM route and route get failed"), Remedy: "reconcile routerd and remove stale remote /32 FIB state; expected local/cloud route to win"})
				continue
			}
			checks = append(checks, doctorCheck{Area: "sam", Name: name, Status: doctorWarn, Detail: firstNonEmpty(command.Error, oneLine(command.Output), "route lookup failed"), Remedy: "inspect Linux route selection for local owned address"})
			continue
		}
		out := oneLine(command.Stdout)
		if strings.Contains(out, " dev sam") || strings.Contains(out, " dev wg-") || strings.Contains(out, " dev ipip") {
			checks = append(checks, doctorCheck{Area: "sam", Name: name, Status: doctorFail, Detail: appendDoctorDetail(out, "expected local/provider-owned route, route get selects SAM/overlay device"), Remedy: "reconcile routerd and remove stale remote /32 FIB state; expected local/cloud route to win"})
			continue
		}
		detail := out
		if len(actual) > 0 {
			detail = appendDoctorDetail(detail, "actual="+strings.Join(actual, " | "))
		}
		checks = append(checks, doctorCheck{Area: "sam", Name: name, Status: doctorPass, Detail: detail})
	}
	for _, route := range doctorSAMObservedBGPReturnRoutes(status) {
		expectedPrefixes[route] = true
	}
	if snapshotCommand.OK {
		checks = append(checks, doctorSAMUnexpectedRouteResidueChecks(pool, poolSpec, expectedPrefixes, actualRoutes)...)
	}
	if len(checks) == 0 {
		return []doctorCheck{{Area: "sam", Name: label, Status: doctorSkip, Detail: "no local owner rows"}}
	}
	return checks
}

func doctorSAMObservedBGPReturnRoutes(status map[string]any) []string {
	var out []string
	switch value := status["observedBGPReturnRoutes"].(type) {
	case []string:
		out = append(out, value...)
	case []any:
		for _, item := range value {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
	case string:
		out = append(out, strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\n' || r == '\t'
		})...)
	}
	seen := map[string]bool{}
	var normalized []string
	for _, raw := range out {
		prefix := normalizeDoctorIPv4RoutePrefix(raw)
		if prefix == "" || seen[prefix] {
			continue
		}
		seen[prefix] = true
		normalized = append(normalized, prefix)
	}
	sort.Strings(normalized)
	return normalized
}

func doctorSAMOwnerRowNeedsLocalFIB(row map[string]any) bool {
	if strings.TrimSpace(fmt.Sprint(row["localNode"])) == "" {
		return false
	}
	if state := strings.TrimSpace(fmt.Sprint(row["state"])); state != "" && state != "OK" {
		return false
	}
	switch strings.TrimSpace(fmt.Sprint(row["class"])) {
	case "LocalHomeOwned", "LocalRouterSelf", "StaticOwned", "StaticHandover":
		return true
	default:
		return false
	}
}

func parseDoctorIPv4MainRoutes(output string) map[string][]string {
	out := map[string][]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "default" {
			continue
		}
		prefix := normalizeDoctorIPv4RoutePrefix(fields[0])
		if prefix == "" {
			continue
		}
		out[prefix] = append(out[prefix], line)
	}
	return out
}

func normalizeDoctorIPv4RoutePrefix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Addr().Is4() {
		return prefix.Masked().String()
	}
	if addr, err := netip.ParseAddr(value); err == nil && addr.Is4() {
		return netip.PrefixFrom(addr, 32).String()
	}
	return ""
}

func doctorSAMForbiddenLocalOwnerRoute(lines []string) string {
	for _, line := range lines {
		if strings.Contains(line, " proto bgp") ||
			strings.Contains(line, " dev sam") ||
			strings.Contains(line, " dev wg-") ||
			strings.Contains(line, " dev ipip") {
			return line
		}
	}
	return ""
}

func doctorSAMUnexpectedRouteResidueChecks(pool string, poolSpec api.MobilityPoolSpec, expected map[string]bool, actual map[string][]string) []doctorCheck {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(poolSpec.Prefix))
	if err != nil || !prefix.Addr().Is4() {
		return nil
	}
	prefix = prefix.Masked()
	var unexpected []string
	for routePrefix, lines := range actual {
		parsed, err := netip.ParsePrefix(routePrefix)
		if err != nil || !parsed.Addr().Is4() || parsed.Bits() != 32 {
			continue
		}
		parsed = parsed.Masked()
		normalized := parsed.String()
		if !prefix.Contains(parsed.Addr()) || expected[normalized] {
			continue
		}
		if doctorSAMRouteResidueIsProviderDHCPLink(lines) {
			continue
		}
		unexpected = append(unexpected, normalized+" actual="+strings.Join(lines, " | "))
	}
	sort.Strings(unexpected)
	checks := make([]doctorCheck, 0, len(unexpected))
	for _, detail := range unexpected {
		address, _, _ := strings.Cut(detail, " ")
		checks = append(checks, doctorCheck{
			Area:   "sam",
			Name:   "MobilityPool/" + pool + " owner-table unexpected route residue " + address,
			Status: doctorFail,
			Detail: "route inside MobilityPool prefix is present in the host FIB but absent from ownershipResolverOwnerTable; " + detail,
			Remedy: "inspect routerd/provider ownership state and remove stale /32 host route only after confirming it is not currently owned",
		})
	}
	return checks
}

func doctorSAMRouteResidueIsProviderDHCPLink(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	for _, line := range lines {
		if !doctorRouteLineHasTokenPair(line, "proto", "dhcp") || !doctorRouteLineHasTokenPair(line, "scope", "link") {
			return false
		}
	}
	return true
}

func doctorRouteLineHasTokenPair(line, key, value string) bool {
	fields := strings.Fields(line)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == key && fields[i+1] == value {
			return true
		}
	}
	return false
}

func doctorSAMOwnerRowsSummary(rows []map[string]any, limit int) string {
	if limit <= 0 || limit > len(rows) {
		limit = len(rows)
	}
	var parts []string
	for _, row := range rows[:limit] {
		address := strings.TrimSpace(fmt.Sprint(row["address"]))
		owner := firstNonEmpty(strings.TrimSpace(fmt.Sprint(row["homeOwnerNode"])), strings.TrimSpace(fmt.Sprint(row["ownerNode"])))
		local := firstNonEmpty(strings.TrimSpace(fmt.Sprint(row["localNodeRef"])), strings.TrimSpace(fmt.Sprint(row["localNode"])))
		reason := strings.TrimSpace(fmt.Sprint(row["conflictReason"]))
		item := address
		if owner != "" || local != "" {
			item += " owner=" + owner + " local=" + local
		}
		if reason != "" {
			item += " reason=" + reason
		}
		parts = append(parts, strings.TrimSpace(item))
	}
	if len(rows) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(rows)-limit))
	}
	return strings.Join(parts, "; ")
}

func (r doctorRunner) doctorHybrid() []doctorCheck {
	if r.router == nil {
		return []doctorCheck{{Area: "hybrid", Name: "startup config", Status: doctorSkip, Detail: "startup config unavailable"}}
	}
	routes := selectResources(r.router.Spec.Resources, "HybridRoute", "")
	peers := selectResources(r.router.Spec.Resources, "OverlayPeer", "")
	domains := selectResources(r.router.Spec.Resources, "AddressMobilityDomain", "")
	claims := selectResources(r.router.Spec.Resources, "RemoteAddressClaim", "")
	if len(routes) == 0 && len(peers) == 0 && len(domains) == 0 && len(claims) == 0 {
		return []doctorCheck{{Area: "hybrid", Name: "HybridRoute", Status: doctorSkip, Detail: "no hybrid resources configured"}}
	}
	peerMap := map[string]api.Resource{}
	for _, peer := range peers {
		peerMap[peer.Metadata.Name] = peer
	}
	domainMap := map[string]api.Resource{}
	for _, domain := range domains {
		domainMap[domain.Metadata.Name] = domain
	}
	wgInterfaces := map[string]bool{}
	for _, res := range selectResources(r.router.Spec.Resources, "WireGuardInterface", "") {
		wgInterfaces[res.Metadata.Name] = true
	}
	tunnelInterfaces := map[string]bool{}
	for _, res := range selectResources(r.router.Spec.Resources, "TunnelInterface", "") {
		tunnelInterfaces[res.Metadata.Name] = true
	}
	var checks []doctorCheck
	for _, peer := range peers {
		spec, err := peer.OverlayPeerSpec()
		if err != nil {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "OverlayPeer/" + peer.Metadata.Name, Status: doctorFail, Detail: err.Error(), Remedy: "fix OverlayPeer spec"})
			continue
		}
		if spec.Underlay.Type == "wireguard" {
			name := "OverlayPeer/" + peer.Metadata.Name + " underlay interface"
			if wgInterfaces[spec.Underlay.Interface] {
				checks = append(checks, doctorCheck{Area: "hybrid", Name: name, Status: doctorPass, Detail: "WireGuardInterface/" + spec.Underlay.Interface})
			} else {
				checks = append(checks, doctorCheck{Area: "hybrid", Name: name, Status: doctorFail, Detail: "missing WireGuardInterface/" + spec.Underlay.Interface, Remedy: "declare the WireGuardInterface or change OverlayPeer underlay.interface"})
			}
		}
		if spec.Underlay.Type == "ipip" || spec.Underlay.Type == "gre" {
			name := "OverlayPeer/" + peer.Metadata.Name + " underlay interface"
			if tunnelInterfaces[spec.Underlay.Interface] {
				checks = append(checks, doctorCheck{Area: "hybrid", Name: name, Status: doctorPass, Detail: "TunnelInterface/" + spec.Underlay.Interface})
			} else {
				checks = append(checks, doctorCheck{Area: "hybrid", Name: name, Status: doctorFail, Detail: "missing TunnelInterface/" + spec.Underlay.Interface, Remedy: "declare the TunnelInterface or change OverlayPeer underlay.interface"})
			}
		}
	}
	for _, route := range routes {
		spec, err := route.HybridRouteSpec()
		if err != nil {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "HybridRoute/" + route.Metadata.Name, Status: doctorFail, Detail: err.Error(), Remedy: "fix HybridRoute spec"})
			continue
		}
		peerName := doctorHybridRefName(spec.PeerRef, "OverlayPeer")
		if _, ok := peerMap[peerName]; ok {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "HybridRoute/" + route.Metadata.Name + " peerRef", Status: doctorPass, Detail: "OverlayPeer/" + peerName})
		} else {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "HybridRoute/" + route.Metadata.Name + " peerRef", Status: doctorFail, Detail: "missing OverlayPeer/" + peerName, Remedy: "declare the OverlayPeer or update spec.peerRef"})
		}
		checks = append(checks, doctorHybridDefaultRouteCheck(route.Metadata.Name, spec.DestinationCIDRs))
		checks = append(checks, r.doctorHybridMTUCheck(route.Metadata.Name, peerName))
		checks = append(checks, r.doctorHybridHealthCheck(route.Metadata.Name, spec.HealthCheckRef))
		checks = append(checks, r.doctorHybridRouteInstalledChecks(route.Metadata.Name, spec.DestinationCIDRs)...)
	}
	for _, domain := range domains {
		spec, err := domain.AddressMobilityDomainSpec()
		if err != nil {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "AddressMobilityDomain/" + domain.Metadata.Name, Status: doctorFail, Detail: err.Error(), Remedy: "fix AddressMobilityDomain spec"})
			continue
		}
		if spec.PeerRef == "" {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "AddressMobilityDomain/" + domain.Metadata.Name + " peerRef", Status: doctorSkip, Detail: "no default peerRef configured"})
			continue
		}
		peerName := doctorHybridRefName(spec.PeerRef, "OverlayPeer")
		if _, ok := peerMap[peerName]; ok {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "AddressMobilityDomain/" + domain.Metadata.Name + " peerRef", Status: doctorPass, Detail: "OverlayPeer/" + peerName})
		} else {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "AddressMobilityDomain/" + domain.Metadata.Name + " peerRef", Status: doctorFail, Detail: "missing OverlayPeer/" + peerName, Remedy: "declare the OverlayPeer or update spec.peerRef"})
		}
	}
	for _, claim := range claims {
		spec, err := claim.RemoteAddressClaimSpec()
		if err != nil {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "RemoteAddressClaim/" + claim.Metadata.Name, Status: doctorFail, Detail: err.Error(), Remedy: "fix RemoteAddressClaim spec"})
			continue
		}
		domainName := doctorHybridRefName(spec.DomainRef, "AddressMobilityDomain")
		if _, ok := domainMap[domainName]; ok {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "RemoteAddressClaim/" + claim.Metadata.Name + " domainRef", Status: doctorPass, Detail: "AddressMobilityDomain/" + domainName})
		} else {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "RemoteAddressClaim/" + claim.Metadata.Name + " domainRef", Status: doctorFail, Detail: "missing AddressMobilityDomain/" + domainName, Remedy: "declare the AddressMobilityDomain or update spec.domainRef"})
		}
		peerName := doctorHybridRefName(spec.Delivery.PeerRef, "OverlayPeer")
		if _, ok := peerMap[peerName]; ok {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "RemoteAddressClaim/" + claim.Metadata.Name + " delivery.peerRef", Status: doctorPass, Detail: "OverlayPeer/" + peerName})
		} else {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: "RemoteAddressClaim/" + claim.Metadata.Name + " delivery.peerRef", Status: doctorFail, Detail: "missing OverlayPeer/" + peerName, Remedy: "declare the OverlayPeer or update spec.delivery.peerRef"})
		}
		checks = append(checks, doctorHybridCaptureTypeCheck(claim.Metadata.Name, spec.Capture.Type))
		checks = append(checks, r.doctorSAMLiveChecks(claim.Metadata.Name, spec)...)
	}
	return checks
}

func (r doctorRunner) doctorSAMLiveChecks(name string, spec api.RemoteAddressClaimSpec) []doctorCheck {
	if !r.opts.Host {
		return []doctorCheck{doctorHostSkipped("hybrid", "RemoteAddressClaim/"+name+" SAM dataplane")}
	}
	if doctorCurrentOS() != platform.OSLinux {
		return []doctorCheck{{Area: "hybrid", Name: "RemoteAddressClaim/" + name + " SAM dataplane", Status: doctorSkip, Detail: "SAM capture not implemented on this OS"}}
	}
	statusReader, _ := r.store.(sam.StatusReader)
	gate := sam.EvaluateCaptureGate(spec.Capture, statusReader)
	var gateChecks []doctorCheck
	if gate.Type == "vrrp-master" || gate.VirtualAddressRef != "" {
		gateChecks = append(gateChecks, doctorSAMActiveWhenVRRPCheck(name, gate))
	}
	if !gate.Active {
		return append(gateChecks, doctorSAMInactiveCaptureChecks(name, spec, gate)...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
	defer cancel()
	address := strings.TrimSpace(spec.Address)
	routeName := sam.DeliveryRouteName(name)
	tunnel := strings.TrimSpace(spec.Delivery.TunnelInterface)
	if tunnel == "" {
		tunnel = "delivery tunnel interface unresolved from OverlayPeer in doctor"
	}
	captureInterface := strings.TrimSpace(spec.Capture.Interface)
	checks := append([]doctorCheck{}, gateChecks...)
	checks = append(checks,
		doctorSAMIPForwardCheck(ctx, name),
		doctorSAMDeliveryRouteCheck(ctx, name, routeName, address, tunnel),
		doctorSAMRouteGetCheck(ctx, name, address),
		doctorSAMMSSClampCheck(ctx, name, captureInterface, tunnel),
		doctorSAMForceFragmentCheck(ctx, name, captureInterface, tunnel),
		doctorSAMHostFirewallCheck(ctx, name, captureInterface, tunnel, r.doctorWireGuardListenPort(tunnel)),
	)
	if strings.TrimSpace(spec.Capture.Type) == "proxy-arp" {
		checks = append(checks, doctorSAMCaptureInterfaceCheck(ctx, name, captureInterface))
		checks = append(checks, doctorSAMProxyARPEnabledCheck(ctx, name, captureInterface))
		checks = append(checks, doctorSAMProxyNeighborCheck(ctx, name, address, captureInterface))
		if gate.Type == "vrrp-master" || gate.VirtualAddressRef != "" {
			checks = append(checks, doctorSAMProxyARPDuplicateResponderCheck(ctx, name, address, captureInterface, gate.Active))
		}
		checks = append(checks, doctorSAMRPFilterCheck(ctx, name, captureInterface))
		checks = append(checks, doctorSAMForwardPolicyCheck(ctx, name, captureInterface, tunnel, address))
	}
	if iface := strings.TrimSpace(spec.Delivery.TunnelInterface); iface != "" {
		checks = append(checks, doctorSAMRPFilterCheck(ctx, name, iface))
	}
	if strings.TrimSpace(spec.Capture.Type) == "provider-secondary-ip" {
		if !spec.Capture.ConfigureOSAddress {
			checks = append(checks, doctorSAMLocalAddressAbsentCheck(ctx, name, address))
		}
		checks = append(checks, doctorSAMForwardPolicyCheck(ctx, name, strings.TrimSpace(spec.Capture.Interface), tunnel, address))
	}
	return checks
}

func doctorSAMActiveWhenVRRPCheck(name string, gate sam.CaptureGateStatus) doctorCheck {
	label := "RemoteAddressClaim/" + name + " activeWhen vrrp-master"
	if gate.Active {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: gate.Message}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "capture gated inactive: " + gate.Message}
}

func doctorSAMInactiveCaptureChecks(name string, spec api.RemoteAddressClaimSpec, gate sam.CaptureGateStatus) []doctorCheck {
	base := doctorCheck{
		Area:   "hybrid",
		Name:   "RemoteAddressClaim/" + name + " SAM dataplane",
		Status: doctorSkip,
		Detail: "capture gated inactive: " + gate.Message,
	}
	if strings.TrimSpace(spec.Capture.Type) != "proxy-arp" {
		return []doctorCheck{base}
	}
	iface := strings.TrimSpace(spec.Capture.Interface)
	if iface == "" {
		return []doctorCheck{base}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	proxy := doctorSAMProxyNeighborAbsentCheck(ctx, name, strings.TrimSpace(spec.Address), iface, gate)
	proxyARP := doctorSAMProxyARPDisabledCheck(ctx, name, iface, gate)
	return []doctorCheck{base, proxy, proxyARP}
}

func (r doctorRunner) doctorWireGuardListenPort(iface string) int {
	if r.router == nil {
		return 0
	}
	for _, res := range r.router.Spec.Resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "WireGuardInterface" || res.Metadata.Name != iface {
			continue
		}
		spec, err := res.WireGuardInterfaceSpec()
		if err == nil {
			return spec.ListenPort
		}
	}
	return 0
}

func doctorSAMCaptureInterfaceCheck(ctx context.Context, name, iface string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " proxy-arp interface"
	if strings.TrimSpace(iface) == "" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "interface unavailable"}
	}
	command := doctorRunDiagnosticCommand(ctx, "ip link show "+iface, "ip", "-o", "link", "show", "dev", iface)
	if command.OK {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: firstNonEmpty(oneLine(command.Stdout), "interface "+iface+" found")}
	}
	return doctorCheck{
		Area:   "hybrid",
		Name:   label,
		Status: doctorFail,
		Detail: "RemoteAddressClaim/" + name + " proxy-arp interface " + iface + " not found",
		Remedy: "create or rename the interface; routerd cannot set proxy_arp on a missing interface",
	}
}

func doctorSAMIPForwardCheck(ctx context.Context, name string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " ip_forward"
	command := doctorRunDiagnosticCommand(ctx, "sysctl net.ipv4.ip_forward", "sysctl", "-n", "net.ipv4.ip_forward")
	if command.OK && strings.TrimSpace(command.Stdout) == "1" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: "net.ipv4.ip_forward=1"}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: firstNonEmpty(command.Error, oneLine(command.Output), "net.ipv4.ip_forward is not 1"), Remedy: "wait for routerd sysctl reconciliation or set net.ipv4.ip_forward=1"}
}

func doctorSAMRouteGetCheck(ctx context.Context, name, address string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " route get"
	ip := strings.TrimSuffix(strings.TrimSpace(address), "/32")
	if ip == "" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "address unavailable"}
	}
	command := doctorRunDiagnosticCommand(ctx, "ip route get "+ip, "ip", "-4", "route", "get", ip)
	if command.OK {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: oneLine(command.Stdout)}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: firstNonEmpty(command.Error, oneLine(command.Output), "route lookup failed"), Remedy: "inspect Linux route selection for the SAM claim address"}
}

func doctorSAMDeliveryRouteCheck(ctx context.Context, name, routeName, address, tunnel string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " delivery route"
	command := doctorRunDiagnosticCommand(ctx, "ip route show "+address, "ip", "-4", "route", "show", address)
	if command.OK && (strings.Contains(command.Stdout, strings.TrimSpace(address)) || strings.Contains(command.Stdout, strings.TrimSuffix(strings.TrimSpace(address), "/32"))) {
		if strings.HasPrefix(tunnel, "delivery tunnel interface unresolved") || strings.Contains(command.Stdout, "dev "+tunnel) {
			return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: appendDoctorDetail(oneLine(command.Stdout), "lowered IPv4Route/"+routeName)}
		}
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: appendDoctorDetail(oneLine(command.Stdout), "expected dev "+tunnel), Remedy: "inspect lowered IPv4Route/" + routeName + " and OverlayPeer delivery settings"}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: firstNonEmpty(command.Error, oneLine(command.Output), "route not found"), Remedy: "wait for routerd to install lowered IPv4Route/" + routeName}
}

func doctorSAMProxyARPEnabledCheck(ctx context.Context, name, iface string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " proxy_arp " + iface
	if strings.TrimSpace(iface) == "" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "interface unavailable"}
	}
	key := "net.ipv4.conf." + iface + ".proxy_arp"
	command := doctorRunDiagnosticCommand(ctx, "sysctl "+key, "sysctl", "-n", key)
	if command.OK && strings.TrimSpace(command.Stdout) == "1" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: key + "=1"}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: firstNonEmpty(command.Error, oneLine(command.Output), key+" is not 1"), Remedy: "wait for routerd SAM capture reconciliation or set " + key + "=1"}
}

func doctorSAMProxyNeighborCheck(ctx context.Context, name, address, iface string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " proxy neighbor"
	command := doctorRunDiagnosticCommand(ctx, "ip neigh show proxy "+address+" dev "+iface, "ip", "neigh", "show", "proxy", address, "dev", iface)
	if command.OK && strings.Contains(command.Stdout, strings.TrimSuffix(address, "/32")) {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: oneLine(command.Stdout)}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorFail, Detail: firstNonEmpty(command.Error, oneLine(command.Output), "proxy neighbor not found"), Remedy: "wait for routerd SAM capture reconciliation or inspect proxy_arp and netlink neighbor state"}
}

func doctorSAMProxyNeighborAbsentCheck(ctx context.Context, name, address, iface string, gate sam.CaptureGateStatus) doctorCheck {
	label := "RemoteAddressClaim/" + name + " proxy neighbor absent"
	command := doctorRunDiagnosticCommand(ctx, "ip neigh show proxy "+address+" dev "+iface, "ip", "neigh", "show", "proxy", address, "dev", iface)
	if command.OK && strings.Contains(command.Stdout, strings.TrimSuffix(address, "/32")) {
		return doctorCheck{
			Area:   "hybrid",
			Name:   label,
			Status: doctorFail,
			Detail: "capture gated inactive but proxy neighbor is present: " + oneLine(command.Stdout),
			Remedy: "wait for routerd SAM cleanup; if it persists, inspect VirtualAddress/" + gate.VirtualAddressRef + " role and remove the stale proxy neighbor",
		}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "capture gated inactive and proxy neighbor absent"}
}

func doctorSAMProxyARPDisabledCheck(ctx context.Context, name, iface string, gate sam.CaptureGateStatus) doctorCheck {
	label := "RemoteAddressClaim/" + name + " proxy_arp disabled"
	if strings.TrimSpace(iface) == "" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "interface unavailable"}
	}
	key := "net.ipv4.conf." + iface + ".proxy_arp"
	command := doctorRunDiagnosticCommand(ctx, "sysctl "+key, "sysctl", "-n", key)
	if command.OK && strings.TrimSpace(command.Stdout) == "0" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: key + "=0"}
	}
	return doctorCheck{
		Area:   "hybrid",
		Name:   label,
		Status: doctorFail,
		Detail: "capture gated inactive but route-based proxy_arp is enabled: " + firstNonEmpty(command.Error, oneLine(command.Output), key+" is not 0"),
		Remedy: "wait for routerd SAM cleanup; if it persists, inspect VirtualAddress/" + gate.VirtualAddressRef + " role and set " + key + "=0",
	}
}

func doctorSAMProxyARPDuplicateResponderCheck(ctx context.Context, name, address, iface string, localMaster bool) doctorCheck {
	label := "RemoteAddressClaim/" + name + " proxy-arp duplicate responders"
	if strings.TrimSpace(iface) == "" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "interface unavailable"}
	}
	ip, ok := doctorProbeIPv4(address)
	if !ok {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "IPv4 address unavailable"}
	}
	command := doctorRunDiagnosticCommand(ctx, "arping "+ip+" dev "+iface, "arping", "-c", "3", "-w", "2", "-I", iface, ip)
	macs := doctorUniqueMACAddresses(command.Output)
	switch {
	case len(macs) > 1:
		return doctorCheck{
			Area:   "hybrid",
			Name:   label,
			Status: doctorFail,
			Detail: fmt.Sprintf("multiple ARP responders for %s on %s: %s", ip, iface, strings.Join(macs, ", ")),
			Remedy: "split-brain proxy-ARP capture detected; verify only the VRRP master captures this /32 and inspect L2 loop/storm stability evidence",
		}
	case len(macs) == 1:
		if localMaster {
			localMAC := doctorLocalInterfaceMAC(ctx, iface)
			if localMAC != "" && !strings.EqualFold(macs[0], localMAC) {
				return doctorCheck{
					Area:   "hybrid",
					Name:   label,
					Status: doctorFail,
					Detail: fmt.Sprintf("local VRRP master plus peer ARP responder for %s on %s: peer %s, local %s", ip, iface, macs[0], localMAC),
					Remedy: "split-brain proxy-ARP capture detected; backup nodes must be fail-closed and only the VRRP master may answer this /32",
				}
			}
		}
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: fmt.Sprintf("single ARP responder for %s on %s: %s", ip, iface, macs[0])}
	case command.OK:
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: fmt.Sprintf("no duplicate ARP responders observed for %s on %s", ip, iface)}
	default:
		return doctorCheck{
			Area:   "hybrid",
			Name:   label,
			Status: doctorWarn,
			Detail: "duplicate responder probe unavailable: " + firstNonEmpty(command.Error, oneLine(command.Output), "arping produced no output"),
			Remedy: "install arping or run doctor with privileges that can send ARP probes to verify split-brain proxy-ARP capture",
		}
	}
}

func doctorLocalInterfaceMAC(ctx context.Context, iface string) string {
	if strings.TrimSpace(iface) == "" {
		return ""
	}
	command := doctorRunDiagnosticCommand(ctx, "cat /sys/class/net/"+iface+"/address", "cat", "/sys/class/net/"+iface+"/address")
	if !command.OK {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(command.Stdout))
}

func doctorProbeIPv4(address string) (string, bool) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", false
	}
	if prefix, err := netip.ParsePrefix(address); err == nil && prefix.Addr().Is4() {
		return prefix.Addr().String(), true
	}
	if addr, err := netip.ParseAddr(address); err == nil && addr.Is4() {
		return addr.String(), true
	}
	return "", false
}

func doctorUniqueMACAddresses(output string) []string {
	seen := map[string]bool{}
	for _, match := range doctorMACAddressPattern.FindAllString(output, -1) {
		seen[strings.ToLower(match)] = true
	}
	macs := make([]string, 0, len(seen))
	for mac := range seen {
		macs = append(macs, mac)
	}
	sort.Strings(macs)
	return macs
}

func doctorSAMLocalAddressAbsentCheck(ctx context.Context, name, address string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " local OS address"
	command := doctorRunDiagnosticCommand(ctx, "ip addr show "+address, "ip", "-o", "-4", "addr", "show", "to", address)
	if command.OK && strings.TrimSpace(command.Stdout) == "" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: strings.TrimSpace(address) + " absent from local interfaces"}
	}
	if command.OK {
		return doctorCheck{
			Area:   "hybrid",
			Name:   label,
			Status: doctorWarn,
			Detail: oneLine(command.Stdout),
			Remedy: "routerd enforces OS-absence and removes this address during reconcile when present; if it keeps reappearing, disable the guest cloud-init/netplan config for that IP",
		}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: firstNonEmpty(command.Error, oneLine(command.Output), "local address check failed"), Remedy: "verify the captured address is not assigned to the guest OS"}
}

func doctorSAMForwardPolicyCheck(ctx context.Context, name, captureIface, tunnel, address string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " FORWARD policy"
	command := doctorRunDiagnosticCommand(ctx, "nft list table inet routerd_filter", "nft", "list", "table", "inet", "routerd_filter")
	if !command.OK {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: doctorSAMForwardPolicyUnavailableDetail(command)}
	}
	detail := "SAM delivery traverses FORWARD"
	if captureIface != "" || tunnel != "" {
		detail = appendDoctorDetail(detail, "path "+firstNonEmpty(captureIface, "<capture-interface>")+" -> "+firstNonEmpty(tunnel, "<tunnel-interface>"))
	}
	if address != "" {
		detail = appendDoctorDetail(detail, "claim "+address)
	}
	if nftForwardPolicyDrop(command.Stdout) {
		return doctorCheck{
			Area:   "hybrid",
			Name:   label,
			Status: doctorWarn,
			Detail: appendDoctorDetail(detail, "default-drop FORWARD policy observed"),
			Remedy: "verify firewall policy permits SAM forwarding between the capture interface and tunnel interface for the captured /32",
		}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: detail}
}

func doctorSAMMSSClampCheck(ctx context.Context, name, captureIface, tunnel string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " MSS clamp"
	tunnel = strings.TrimSpace(tunnel)
	captureIface = strings.TrimSpace(captureIface)
	if tunnel == "" || strings.HasPrefix(tunnel, "delivery tunnel interface unresolved") {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "delivery tunnel interface unavailable"}
	}
	link := doctorRunDiagnosticCommand(ctx, "ip link show "+tunnel, "ip", "-o", "link", "show", "dev", tunnel)
	detail := "SAM delivery tunnel " + tunnel
	if link.OK {
		if mtu := doctorExtractLinkMTU(link.Stdout); mtu != "" {
			detail = appendDoctorDetail(detail, "mtu="+mtu)
		} else {
			detail = appendDoctorDetail(detail, oneLine(link.Stdout))
		}
	}
	if captureIface == "" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: appendDoctorDetail(detail, "capture interface unavailable"), Remedy: "set spec.capture.interface on the RemoteAddressClaim so routerd can derive and diagnose SAM MSS clamp rules"}
	}
	table := doctorRunDiagnosticCommand(ctx, "nft list table inet routerd_mss", "nft", "list", "table", "inet", "routerd_mss")
	if !table.OK {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: appendDoctorDetail(detail, doctorSAMMSSClampUnavailableDetail(table)), Remedy: "wait for routerd PathMTUController to install routerd_mss or inspect TCP MSS clamp rendering for the SAM path"}
	}
	if nftMSSClampHasPath(table.Stdout, captureIface, tunnel) {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: appendDoctorDetail(detail, "routerd_mss covers "+captureIface+" -> "+tunnel)}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: appendDoctorDetail(detail, "routerd_mss missing "+captureIface+" -> "+tunnel), Remedy: "ensure SAM Path MTU derivation includes the capture interface and WireGuard delivery tunnel"}
}

func doctorSAMMSSClampUnavailableDetail(command diagnoseCommandCheck) string {
	combined := strings.ToLower(strings.Join([]string{command.Error, command.Stderr, command.Stdout, command.Output}, " "))
	switch {
	case strings.Contains(combined, "executable file not found") || strings.Contains(combined, "no such file or directory") && strings.Contains(strings.ToLower(command.Error), "exec"):
		return "nft unavailable"
	case strings.Contains(combined, "permission denied") || strings.Contains(combined, "operation not permitted"):
		return "permission denied running nft"
	case strings.Contains(combined, "no such file or directory") || strings.Contains(combined, "no such table") || strings.Contains(combined, "table does not exist"):
		return "routerd_mss table absent"
	default:
		return firstNonEmpty(command.Error, oneLine(command.Output), "exit "+strconv.Itoa(command.ExitCode))
	}
}

func doctorSAMForceFragmentCheck(ctx context.Context, name, captureIface, tunnel string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " force-fragment"
	tunnel = strings.TrimSpace(tunnel)
	captureIface = strings.TrimSpace(captureIface)
	if tunnel == "" || strings.HasPrefix(tunnel, "delivery tunnel interface unresolved") {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "delivery tunnel interface unavailable"}
	}
	if captureIface == "" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "capture interface unavailable"}
	}
	table := doctorRunDiagnosticCommand(ctx, "nft list table ip routerd_forcefrag", "nft", "list", "table", "ip", "routerd_forcefrag")
	if !table.OK {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: appendDoctorDetail("optional IPv4 force-fragment not active", doctorForceFragmentUnavailableDetail(table))}
	}
	if nftForceFragmentHasPath(table.Stdout, captureIface, tunnel) {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: "routerd_forcefrag covers " + captureIface + " -> " + tunnel}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "routerd_forcefrag does not cover " + captureIface + " -> " + tunnel}
}

func doctorForceFragmentUnavailableDetail(command diagnoseCommandCheck) string {
	combined := strings.ToLower(strings.Join([]string{command.Error, command.Stderr, command.Stdout, command.Output}, " "))
	switch {
	case strings.Contains(combined, "executable file not found") || strings.Contains(combined, "no such file or directory") && strings.Contains(strings.ToLower(command.Error), "exec"):
		return "nft unavailable"
	case strings.Contains(combined, "permission denied") || strings.Contains(combined, "operation not permitted"):
		return "permission denied running nft"
	case strings.Contains(combined, "no such file or directory") || strings.Contains(combined, "no such table") || strings.Contains(combined, "table does not exist"):
		return "routerd_forcefrag table absent"
	default:
		return firstNonEmpty(command.Error, oneLine(command.Output), "exit "+strconv.Itoa(command.ExitCode))
	}
}

func doctorExtractLinkMTU(output string) string {
	fields := strings.Fields(output)
	for i, field := range fields {
		if field == "mtu" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func nftMSSClampHasPath(output, from, to string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, `iifname "`+from+`"`) && strings.Contains(line, `oifname "`+to+`"`) && strings.Contains(line, "maxseg") {
			return true
		}
	}
	return false
}

func nftForceFragmentHasPath(output, from, to string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, `iifname "`+from+`"`) &&
			(strings.Contains(line, `fib daddr oifname "`+to+`"`) || strings.Contains(line, `oifname "`+to+`"`)) &&
			strings.Contains(line, "frag-off set 0") {
			return true
		}
	}
	return false
}

func doctorSAMHostFirewallCheck(ctx context.Context, name, captureIface, tunnel string, listenPort int) doctorCheck {
	label := "RemoteAddressClaim/" + name + " host firewall"
	var warnings []string
	var details []string
	input := doctorRunDiagnosticCommand(ctx, "iptables -S INPUT", "iptables", "-S", "INPUT")
	if input.OK {
		if listenPort > 0 {
			if iptablesChainHasTerminalDropReject(input.Stdout) && !iptablesInputAllowsUDPPort(input.Stdout, listenPort) {
				warnings = append(warnings, fmt.Sprintf("INPUT has terminal drop/reject without UDP/%d accept", listenPort))
			} else {
				details = append(details, fmt.Sprintf("INPUT permits or does not block UDP/%d", listenPort))
			}
		}
	} else {
		details = append(details, "iptables INPUT unavailable: "+firstNonEmpty(input.Error, oneLine(input.Output), "exit "+strconv.Itoa(input.ExitCode)))
	}
	forward := doctorRunDiagnosticCommand(ctx, "iptables -S FORWARD", "iptables", "-S", "FORWARD")
	if forward.OK {
		if captureIface != "" && tunnel != "" && !strings.HasPrefix(tunnel, "delivery tunnel interface unresolved") {
			forwardOK := iptablesForwardAllowsPath(forward.Stdout, captureIface, tunnel) && iptablesForwardAllowsPath(forward.Stdout, tunnel, captureIface)
			if iptablesChainHasTerminalDropReject(forward.Stdout) && !forwardOK {
				warnings = append(warnings, "FORWARD has terminal drop/reject without "+captureIface+" <-> "+tunnel+" accept")
			} else {
				details = append(details, "FORWARD permits or does not block "+captureIface+" <-> "+tunnel)
			}
		} else {
			details = append(details, "FORWARD path unavailable")
		}
	} else {
		details = append(details, "iptables FORWARD unavailable: "+firstNonEmpty(forward.Error, oneLine(forward.Output), "exit "+strconv.Itoa(forward.ExitCode)))
	}
	if len(warnings) > 0 {
		return doctorCheck{
			Area:   "hybrid",
			Name:   label,
			Status: doctorWarn,
			Detail: strings.Join(warnings, "; "),
			Remedy: "permit WireGuard UDP ingress and forwarding between the SAM capture interface and delivery tunnel in the host firewall",
		}
	}
	if len(details) == 0 {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "host firewall state unavailable"}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: strings.Join(details, "; ")}
}

func iptablesChainHasTerminalDropReject(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "-P ") && (strings.HasSuffix(line, " DROP") || strings.HasSuffix(line, " REJECT")) {
			return true
		}
		if strings.Contains(line, "-j DROP") || strings.Contains(line, "-j REJECT") {
			return true
		}
	}
	return false
}

func iptablesInputAllowsUDPPort(output string, port int) bool {
	portString := strconv.Itoa(port)
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "-A INPUT") || !strings.Contains(line, "-j ACCEPT") {
			continue
		}
		if strings.Contains(line, "-p udp") && (strings.Contains(line, "--dport "+portString) || strings.Contains(line, "dpt:"+portString)) {
			return true
		}
	}
	return false
}

func iptablesForwardAllowsPath(output, from, to string) bool {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "-A FORWARD") || !strings.Contains(line, "-j ACCEPT") {
			continue
		}
		hasFrom := strings.Contains(line, "-i "+from) || strings.Contains(line, "-i "+shellQuoteForIptablesMatch(from))
		hasTo := strings.Contains(line, "-o "+to) || strings.Contains(line, "-o "+shellQuoteForIptablesMatch(to))
		if hasFrom && hasTo {
			return true
		}
	}
	return false
}

func shellQuoteForIptablesMatch(value string) string {
	return "'" + value + "'"
}

func doctorSAMForwardPolicyUnavailableDetail(command diagnoseCommandCheck) string {
	combined := strings.ToLower(strings.Join([]string{command.Error, command.Stderr, command.Stdout, command.Output}, " "))
	switch {
	case strings.Contains(combined, "executable file not found") || strings.Contains(combined, "no such file or directory") && strings.Contains(strings.ToLower(command.Error), "exec"):
		return "nft unavailable"
	case strings.Contains(combined, "permission denied") || strings.Contains(combined, "operation not permitted"):
		return "permission denied running nft"
	case strings.Contains(combined, "no such file or directory") || strings.Contains(combined, "no such table") || strings.Contains(combined, "table does not exist"):
		return "routerd_filter table absent; no routerd firewall policy observed"
	default:
		detail := firstNonEmpty(command.Error, oneLine(command.Output), "exit "+strconv.Itoa(command.ExitCode))
		return "nft list table inet routerd_filter failed: " + detail
	}
}

func nftForwardPolicyDrop(output string) bool {
	lower := strings.ToLower(output)
	for _, block := range strings.Split(lower, "chain ") {
		if !strings.Contains(block, "forward") {
			continue
		}
		if strings.Contains(block, "hook forward") && strings.Contains(block, "policy drop") {
			return true
		}
	}
	return false
}

func doctorSAMRPFilterCheck(ctx context.Context, name, iface string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " rp_filter " + iface
	if strings.TrimSpace(iface) == "" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: "interface unavailable"}
	}
	key := "net.ipv4.conf." + iface + ".rp_filter"
	command := doctorRunDiagnosticCommand(ctx, "sysctl "+key, "sysctl", "-n", key)
	value := strings.TrimSpace(command.Stdout)
	if command.OK && value == "1" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: key + "=1 (strict); SAM forwarded /32 traffic may be dropped", Remedy: "consider setting " + key + "=2 (loose) after validating router policy"}
	}
	if command.OK {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: key + "=" + value}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorSkip, Detail: firstNonEmpty(command.Error, oneLine(command.Output), "rp_filter unavailable")}
}

func doctorHybridCaptureTypeCheck(name, captureType string) doctorCheck {
	switch strings.TrimSpace(captureType) {
	case "provider-secondary-ip", "proxy-arp":
		return doctorCheck{Area: "hybrid", Name: "RemoteAddressClaim/" + name + " capture.type", Status: doctorPass, Detail: captureType}
	case "static-host-route", "garp":
		return doctorCheck{Area: "hybrid", Name: "RemoteAddressClaim/" + name + " capture.type", Status: doctorFail, Detail: captureType + " is reserved/not implemented in MVP", Remedy: "use provider-secondary-ip or proxy-arp"}
	default:
		return doctorCheck{Area: "hybrid", Name: "RemoteAddressClaim/" + name + " capture.type", Status: doctorFail, Detail: "unsupported capture type " + captureType, Remedy: "use provider-secondary-ip or proxy-arp"}
	}
}

func doctorHybridDefaultRouteCheck(name string, destinations []string) doctorCheck {
	for _, destination := range destinations {
		if doctorHybridDefaultDestination(destination) {
			return doctorCheck{Area: "hybrid", Name: "HybridRoute/" + name + " default route untouched", Status: doctorFail, Detail: "default destination " + destination + " is not allowed", Remedy: "remove default from spec.destinationCIDRs"}
		}
	}
	return doctorCheck{Area: "hybrid", Name: "HybridRoute/" + name + " default route untouched", Status: doctorPass, Detail: "no default destinations"}
}

func (r doctorRunner) doctorHybridMTUCheck(routeName, peerName string) doctorCheck {
	estimate, ok := hybrid.EstimateMTU(*r.router, peerName)
	name := "HybridRoute/" + routeName + " MTU estimate"
	if !ok || estimate.UnderlayMTU == 0 {
		return doctorCheck{Area: "hybrid", Name: name, Status: doctorSkip, Detail: "underlay MTU unavailable"}
	}
	detail := fmt.Sprintf("underlay=%d overhead=%d estimate=%d", estimate.UnderlayMTU, estimate.Overhead, estimate.EstimatedMTU)
	if estimate.Warning != "" {
		return doctorCheck{Area: "hybrid", Name: name, Status: doctorWarn, Detail: appendDoctorDetail(detail, estimate.Warning), Remedy: "increase underlay MTU or reduce overlay payload MTU"}
	}
	return doctorCheck{Area: "hybrid", Name: name, Status: doctorPass, Detail: detail}
}

func (r doctorRunner) doctorHybridHealthCheck(routeName, ref string) doctorCheck {
	name := "HybridRoute/" + routeName + " healthCheckRef"
	if strings.TrimSpace(ref) == "" {
		return doctorCheck{Area: "hybrid", Name: name, Status: doctorSkip, Detail: "no healthCheckRef configured"}
	}
	if !resourceExists(r.router.Spec.Resources, "HealthCheck", ref) {
		return doctorCheck{Area: "hybrid", Name: name, Status: doctorFail, Detail: "missing HealthCheck/" + ref, Remedy: "declare the HealthCheck or update spec.healthCheckRef"}
	}
	if !r.opts.Host {
		return doctorCheck{Area: "hybrid", Name: name, Status: doctorSkip, Detail: "host commands disabled by --no-host"}
	}
	status := objectStatus(r.store, api.NetAPIVersion, "HealthCheck", ref)
	return doctorNamedStatusCheck("hybrid", name, status, healthyPhases("Healthy", "Passing", "Applied", "Ready"))
}

func (r doctorRunner) doctorHybridRouteInstalledChecks(routeName string, destinations []string) []doctorCheck {
	if !r.opts.Host {
		return []doctorCheck{doctorHostSkipped("hybrid", "HybridRoute/"+routeName+" route installed")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
	defer cancel()
	var checks []doctorCheck
	for _, destination := range destinations {
		label := "HybridRoute/" + routeName + " route " + destination
		command := runDiagnosticCommand(ctx, "ip route show "+destination, "ip", "-4", "route", "show", destination)
		if command.OK && strings.Contains(command.Stdout, destination) {
			checks = append(checks, doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: oneLine(command.Stdout)})
			continue
		}
		detail := firstNonEmpty(command.Error, oneLine(command.Output), oneLine(command.Stdout), "route not found")
		checks = append(checks, doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: detail, Remedy: "wait for routerd to install the lowered IPv4Route or inspect route controller status"})
	}
	return checks
}

func doctorHybridDefaultDestination(value string) bool {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "default") {
		return true
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return false
	}
	prefix = prefix.Masked()
	return prefix.Bits() == 0 && (prefix.Addr().Is4() || prefix.Addr().Is6())
}

func doctorHybridRefName(ref, kind string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, kind+"/") {
		return strings.TrimPrefix(ref, kind+"/")
	}
	return ref
}

func resourceExists(resources []api.Resource, kind, name string) bool {
	name = doctorHybridRefName(name, kind)
	for _, resource := range resources {
		if resource.Kind == kind && resource.Metadata.Name == name {
			return true
		}
	}
	return false
}

func (r doctorRunner) doctorDynamic() []doctorCheck {
	if r.router == nil {
		return []doctorCheck{{Area: "dynamic", Name: "startup config", Status: doctorSkip, Detail: "startup config unavailable"}}
	}
	lister, ok := r.store.(routerstate.DynamicConfigPartLister)
	if !ok {
		return []doctorCheck{{Area: "dynamic", Name: "state database", Status: doctorSkip, Detail: "state store does not expose dynamic config parts"}}
	}
	records, err := lister.ListDynamicConfigParts()
	if err != nil {
		return []doctorCheck{{Area: "dynamic", Name: "state database", Status: doctorSkip, Detail: "dynamic config parts unavailable: " + err.Error()}}
	}
	now := time.Now().UTC()
	checks := []doctorCheck{}
	parts, err := dynamicPartsFromRecords(records)
	if err != nil {
		checks = append(checks, doctorCheck{Area: "dynamic", Name: "dynamic parts decode", Status: doctorFail, Detail: err.Error(), Remedy: "remove or replace the malformed DynamicConfigPart generation"})
		checks = append(checks, doctorCheck{Area: "dynamic", Name: "effective config builds", Status: doctorSkip, Detail: "skipped because stored dynamic parts did not decode"})
		return checks
	}
	resources, directives := 0, 0
	for _, part := range parts {
		resources += len(part.Spec.Resources)
		directives += len(part.Spec.Directives)
	}
	checks = append(checks, doctorCheck{Area: "dynamic", Name: "dynamic parts decode", Status: doctorPass, Detail: fmt.Sprintf("%d parts, %d resources, %d directives", len(parts), resources, directives)})

	activeParts := make([]dynamicconfig.DynamicConfigPart, 0, len(parts))
	activeBySource := map[string]int{}
	expiredBySource := map[string]int{}
	for _, part := range parts {
		if part.IsExpired(now) {
			expiredBySource[part.Spec.Source]++
			continue
		}
		activeParts = append(activeParts, part)
		activeBySource[part.Spec.Source]++
	}
	staleSources := []string{}
	for source, expired := range expiredBySource {
		if expired > 0 && activeBySource[source] == 0 {
			staleSources = append(staleSources, source)
		}
	}
	sort.Strings(staleSources)
	expiredStatus := doctorPass
	expiredDetail := fmt.Sprintf("%d active, %d expired (ignored)", len(activeParts), len(parts)-len(activeParts))
	if len(staleSources) > 0 {
		expiredStatus = doctorWarn
		expiredDetail = appendDoctorDetail(expiredDetail, "stale: source "+strings.Join(staleSources, ",")+" has only expired generations")
	}
	checks = append(checks, doctorCheck{Area: "dynamic", Name: "expired parts ignored", Status: expiredStatus, Detail: expiredDetail})

	policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*r.router)
	if err != nil {
		checks = append(checks, doctorCheck{Area: "dynamic", Name: "effective config builds", Status: doctorFail, Detail: err.Error(), Remedy: "fix DynamicOverridePolicy resources in startup config"})
		return checks
	}
	_, result, err := dynamicconfig.BuildEffectiveConfig(*r.router, activeParts, policies, now)
	if err != nil {
		checks = append(checks, doctorCheck{Area: "dynamic", Name: "effective config builds", Status: doctorFail, Detail: err.Error(), Remedy: "fix dynamic masks, conflicting resources, or override policy grants"})
	} else {
		checks = append(checks, doctorCheck{Area: "dynamic", Name: "effective config builds", Status: doctorPass, Detail: fmt.Sprintf("%d suppressed, %d dynamic resources added", len(result.Suppressed), len(result.AddedResources))})
	}
	checks = append(checks, r.doctorDynamicOverridePolicyCheck(activeParts, policies))
	return checks
}

func (r doctorRunner) doctorRoutes() []doctorCheck {
	lister, ok := r.store.(routerstate.ObjectStatusLister)
	if !ok {
		return []doctorCheck{{Area: "routes", Name: "IPv4Route drift", Status: doctorSkip, Detail: "state backend does not expose object statuses"}}
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return []doctorCheck{{Area: "routes", Name: "IPv4Route drift", Status: doctorFail, Detail: err.Error(), Remedy: "check state backend and retry"}}
	}
	var checks []doctorCheck
	for _, item := range statuses {
		if item.APIVersion != api.NetAPIVersion || item.Kind != "IPv4Route" {
			continue
		}
		status := item.Status
		if !strings.EqualFold(stringStatus(status, "phase"), "Installed") {
			continue
		}
		if boolStatus(status, "dryRun") {
			continue
		}
		checks = append(checks, r.doctorIPv4RouteStatusDriftCheck(item.Name, status))
	}
	if len(checks) == 0 {
		return []doctorCheck{{Area: "routes", Name: "IPv4Route drift", Status: doctorSkip, Detail: "no installed IPv4Route statuses"}}
	}
	return checks
}

func (r doctorRunner) doctorIPv4RouteStatusDriftCheck(name string, status map[string]any) doctorCheck {
	label := "IPv4Route/" + name + " host route"
	if !r.opts.Host {
		return doctorHostSkipped("routes", label)
	}
	if doctorCurrentOS() != platform.OSLinux {
		return doctorCheck{Area: "routes", Name: label, Status: doctorSkip, Detail: "host route drift checks require Linux iproute2"}
	}
	destination := firstNonEmpty(stringStatus(status, "destination"), "0.0.0.0/0")
	if normalizeDoctorIPv4RoutePrefix(destination) == "" {
		return doctorCheck{Area: "routes", Name: label, Status: doctorSkip, Detail: "non-IPv4 destination " + destination}
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
	defer cancel()
	command := doctorRunDiagnosticCommand(ctx, "ip -4 route show "+destination, "ip", "-4", "route", "show", destination)
	if !command.OK {
		return doctorCheck{Area: "routes", Name: label, Status: doctorFail, Detail: firstNonEmpty(command.Error, oneLine(command.Output), "route snapshot unavailable"), Remedy: "inspect ip -4 route show " + destination}
	}
	lines := doctorRouteLines(command.Stdout)
	if len(lines) == 0 {
		return doctorCheck{Area: "routes", Name: label, Status: doctorFail, Detail: "missing desired route; expected " + doctorIPv4RouteExpectedDetail(status), Remedy: "wait for routerd reconcile or inspect IPv4Route/" + name + " controller status"}
	}
	for _, line := range lines {
		if doctorIPv4RouteLineMatchesStatus(line, status) {
			return doctorCheck{Area: "routes", Name: label, Status: doctorPass, Detail: appendDoctorDetail(line, "matches "+doctorIPv4RouteExpectedDetail(status))}
		}
	}
	return doctorCheck{
		Area:   "routes",
		Name:   label,
		Status: doctorFail,
		Detail: appendDoctorDetail("mismatched desired route; expected "+doctorIPv4RouteExpectedDetail(status), "actual="+strings.Join(lines, " | ")),
		Remedy: "compare IPv4Route/" + name + " status with `ip -4 route show " + destination + "` and rerun routerd reconcile if the host FIB is stale",
	}
}

func doctorRouteLines(output string) []string {
	var out []string
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func doctorIPv4RouteLineMatchesStatus(line string, status map[string]any) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	routeType := firstNonEmpty(stringStatus(status, "type"), "unicast")
	destination := firstNonEmpty(stringStatus(status, "destination"), "0.0.0.0/0")
	if routeType == "blackhole" {
		if fields[0] != "blackhole" {
			return false
		}
		if len(fields) < 2 || !doctorIPv4RouteDestinationMatches(fields[1], destination) {
			return false
		}
	} else if !doctorIPv4RouteDestinationMatches(fields[0], destination) {
		return false
	}
	if gateway := stringStatus(status, "gateway"); gateway != "" && !doctorRouteFieldsContainPair(fields, "via", gateway) {
		return false
	}
	if device := stringStatus(status, "device"); routeType != "blackhole" && device != "" && !doctorRouteFieldsContainPair(fields, "dev", device) {
		return false
	}
	if preferredSource := doctorIPv4RoutePreferredSource(status); preferredSource != "" && !doctorRouteFieldsContainPair(fields, "src", preferredSource) {
		return false
	}
	if metric := statusInt(status["metric"]); metric > 0 && !doctorRouteFieldsContainPair(fields, "metric", fmt.Sprintf("%d", metric)) {
		return false
	}
	return true
}

func doctorIPv4RouteDestinationMatches(actual, expected string) bool {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	if expected == "0.0.0.0/0" && actual == "default" {
		return true
	}
	return normalizeDoctorIPv4RoutePrefix(actual) == normalizeDoctorIPv4RoutePrefix(expected)
}

func doctorRouteFieldsContainPair(fields []string, key, value string) bool {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == key && fields[i+1] == value {
			return true
		}
	}
	return false
}

func doctorIPv4RoutePreferredSource(status map[string]any) string {
	if boolStatus(status, "preferredSourceSkipped") {
		return ""
	}
	if _, ok := status["effectivePreferredSource"]; ok {
		return stringStatus(status, "effectivePreferredSource")
	}
	return stringStatus(status, "preferredSource")
}

func doctorIPv4RouteExpectedDetail(status map[string]any) string {
	parts := []string{
		"type=" + firstNonEmpty(stringStatus(status, "type"), "unicast"),
		"destination=" + firstNonEmpty(stringStatus(status, "destination"), "0.0.0.0/0"),
	}
	for _, key := range []string{"gateway", "device"} {
		if value := stringStatus(status, key); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	if preferredSource := doctorIPv4RoutePreferredSource(status); preferredSource != "" {
		parts = append(parts, "preferredSource="+preferredSource)
	}
	if boolStatus(status, "preferredSourceSkipped") {
		parts = append(parts, "preferredSourceSkipped=true")
	}
	if metric := statusInt(status["metric"]); metric > 0 {
		parts = append(parts, fmt.Sprintf("metric=%d", metric))
	}
	return strings.Join(parts, " ")
}

func (r doctorRunner) doctorDynamicOverridePolicyCheck(parts []dynamicconfig.DynamicConfigPart, policies []dynamicconfig.DynamicOverridePolicy) doctorCheck {
	activeMasks := map[string]bool{}
	for _, part := range parts {
		for _, directive := range part.Spec.Directives {
			if directive.Op == dynamicconfig.DirectiveOpMask {
				activeMasks[dynamicPolicyKey(part.Spec.Source, directive.Target)] = true
			}
		}
	}
	if len(policies) == 0 {
		if len(activeMasks) == 0 {
			return doctorCheck{Area: "dynamic", Name: "override policies present for masks", Status: doctorPass, Detail: "no active masks"}
		}
		return doctorCheck{Area: "dynamic", Name: "override policies present for masks", Status: doctorWarn, Detail: "active masks exist but no DynamicOverridePolicy resources are configured"}
	}
	deadRules := 0
	totalRules := 0
	for _, policy := range policies {
		for _, rule := range policy.Spec.Allow {
			for _, target := range rule.Targets {
				totalRules++
				if !doctorContainsString(rule.Operations, dynamicconfig.DirectiveOpMask) {
					deadRules++
					continue
				}
				if !activeMasks[dynamicPolicyKey(rule.Source, target)] {
					deadRules++
				}
			}
		}
	}
	if deadRules > 0 {
		return doctorCheck{Area: "dynamic", Name: "override policies present for masks", Status: doctorWarn, Detail: fmt.Sprintf("%d/%d DynamicOverridePolicy target rules match no current mask", deadRules, totalRules)}
	}
	return doctorCheck{Area: "dynamic", Name: "override policies present for masks", Status: doctorPass, Detail: fmt.Sprintf("%d active masks covered", len(activeMasks))}
}

func dynamicPolicyKey(source string, target dynamicconfig.DirectiveTarget) string {
	return source + "|" + target.APIVersion + "|" + target.Kind + "|" + target.Name
}

func doctorContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (r doctorRunner) doctorPlugin() []doctorCheck {
	if r.router == nil {
		return []doctorCheck{{Area: "plugin", Name: "startup config", Status: doctorSkip, Detail: "startup config unavailable"}}
	}
	plugins := selectResources(r.router.Spec.Resources, "Plugin", "")
	if len(plugins) == 0 {
		return []doctorCheck{{Area: "plugin", Name: "no plugins configured", Status: doctorSkip, Detail: "no Plugin resources configured"}}
	}
	runLister, runOK := r.store.(routerstate.PluginRunLister)
	partLister, partOK := r.store.(routerstate.DynamicConfigPartLister)
	var checks []doctorCheck
	for _, res := range plugins {
		name := res.Metadata.Name
		spec, err := res.PluginSpec()
		if err != nil {
			checks = append(checks, doctorCheck{Area: "plugin", Name: "Plugin/" + name, Status: doctorFail, Detail: err.Error(), Remedy: "fix Plugin resource spec"})
			continue
		}
		checks = append(checks, doctorPluginExecutableChecks(name, spec.Executable, r.opts.Host)...)
		if !runOK {
			checks = append(checks, doctorCheck{Area: "plugin", Name: "Plugin/" + name + " last run", Status: doctorSkip, Detail: "state store does not expose plugin runs"})
		} else {
			checks = append(checks, doctorPluginLastRunCheck(runLister, name))
		}
		if !partOK {
			checks = append(checks, doctorCheck{Area: "plugin", Name: "Plugin/" + name + " last result fresh", Status: doctorSkip, Detail: "state store does not expose dynamic config parts"})
		} else {
			checks = append(checks, doctorPluginLastResultCheck(partLister, name, time.Now().UTC()))
		}
	}
	return checks
}

func doctorPluginExecutableChecks(name, executable string, host bool) []doctorCheck {
	existsName := "Plugin/" + name + " executable exists"
	execName := "Plugin/" + name + " executable is executable"
	if !host {
		return []doctorCheck{
			{Area: "plugin", Name: existsName, Status: doctorSkip, Detail: "host commands disabled by --no-host"},
			{Area: "plugin", Name: execName, Status: doctorSkip, Detail: "host commands disabled by --no-host"},
		}
	}
	info, err := os.Stat(strings.TrimSpace(executable))
	if err != nil {
		return []doctorCheck{
			{Area: "plugin", Name: existsName, Status: doctorFail, Detail: err.Error(), Remedy: "install the plugin executable on this host"},
			{Area: "plugin", Name: execName, Status: doctorSkip, Detail: "skipped because executable was not found"},
		}
	}
	exists := doctorCheck{Area: "plugin", Name: existsName, Status: doctorPass, Detail: executable}
	if !info.Mode().IsRegular() {
		exists.Status = doctorFail
		exists.Detail = "not a regular file: " + executable
		exists.Remedy = "replace the plugin path with a regular executable file"
		return []doctorCheck{exists, doctorCheck{Area: "plugin", Name: execName, Status: doctorSkip, Detail: "skipped because executable is not a regular file"}}
	}
	if err := routerplugin.ValidateExecutable(executable); err != nil {
		return []doctorCheck{
			exists,
			{Area: "plugin", Name: execName, Status: doctorFail, Detail: err.Error(), Remedy: "set executable mode bits on the plugin file"},
		}
	}
	return []doctorCheck{
		exists,
		{Area: "plugin", Name: execName, Status: doctorPass, Detail: "executable bit set"},
	}
}

func doctorPluginLastRunCheck(lister routerstate.PluginRunLister, name string) doctorCheck {
	runs, err := lister.ListPluginRuns(name)
	if err != nil {
		return doctorCheck{Area: "plugin", Name: "Plugin/" + name + " last run", Status: doctorSkip, Detail: "plugin run history unavailable: " + err.Error()}
	}
	if len(runs) == 0 {
		return doctorCheck{Area: "plugin", Name: "Plugin/" + name + " last run", Status: doctorWarn, Detail: "never run", Remedy: "run routerctl plugin run " + name}
	}
	latest := runs[0]
	if latest.Status == "failed" {
		return doctorCheck{Area: "plugin", Name: "Plugin/" + name + " last run", Status: doctorFail, Detail: firstNonEmpty(latest.Error, latest.Stderr, "last run failed"), Remedy: "inspect plugin stderr and rerun the plugin"}
	}
	if latest.Status != "succeeded" {
		return doctorCheck{Area: "plugin", Name: "Plugin/" + name + " last run", Status: doctorWarn, Detail: "last run status " + latest.Status}
	}
	exit := "exit unknown"
	if latest.HasExitCode {
		exit = fmt.Sprintf("exit %d", latest.ExitCode)
	}
	return doctorCheck{Area: "plugin", Name: "Plugin/" + name + " last run", Status: doctorPass, Detail: fmt.Sprintf("last run %s, %s", formatDynamicTime(latest.CompletedAt), exit)}
}

func doctorPluginLastResultCheck(lister routerstate.DynamicConfigPartLister, name string, now time.Time) doctorCheck {
	source := "Plugin/" + name
	parts, err := lister.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return doctorCheck{Area: "plugin", Name: "Plugin/" + name + " last result fresh", Status: doctorSkip, Detail: "dynamic part unavailable: " + err.Error()}
	}
	if len(parts) == 0 {
		return doctorCheck{Area: "plugin", Name: "Plugin/" + name + " last result fresh", Status: doctorWarn, Detail: "no dynamic part", Remedy: "run routerctl plugin run " + name}
	}
	latest := parts[0]
	if latest.EffectiveStatus(now) == "expired" {
		return doctorCheck{Area: "plugin", Name: "Plugin/" + name + " last result fresh", Status: doctorWarn, Detail: "stale (expired " + formatDynamicTime(latest.ExpiresAt) + ")", Remedy: "rerun the plugin or adjust its result TTL"}
	}
	return doctorCheck{Area: "plugin", Name: "Plugin/" + name + " last result fresh", Status: doctorPass, Detail: fmt.Sprintf("generation %d active until %s", latest.Generation, formatDynamicTime(latest.ExpiresAt))}
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
	case currentlyFailing > 0:
		status = doctorFail
	case totalErrors >= doctorReconcileWarnThreshold:
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
	var checks []doctorCheck
	if len(firewallResources) == 0 {
		checks = append(checks, doctorCheck{Area: "firewall", Name: "FirewallZone/Policy", Status: doctorWarn, Detail: "no firewall zones or policy configured; router may be permissive", Remedy: "declare FirewallZone and FirewallPolicy resources"})
	}
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
		if len(firewallResources) > 0 {
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
		}
		checks = append(checks, r.doctorStaleNftTablesCheck(ctx))
	} else {
		checks = append(checks, doctorHostSkipped("firewall", "nft routerd_filter"))
	}
	return checks
}

func (r doctorRunner) doctorStaleNftTablesCheck(ctx context.Context) doctorCheck {
	name := "stale routerd nft tables"
	if doctorCurrentOS() != platform.OSLinux {
		return doctorCheck{Area: "firewall", Name: name, Status: doctorSkip, Detail: "nftables host table check is Linux-only"}
	}
	expected, err := expectedRouterdNftTables(r.router)
	if err != nil {
		return doctorCheck{Area: "firewall", Name: name, Status: doctorWarn, Detail: "could not render expected nft tables: " + err.Error(), Remedy: "fix config render errors, then rerun doctor firewall"}
	}
	command := runDiagnosticCommand(ctx, "nft list tables", "nft", "list", "tables")
	if !command.OK {
		return doctorCheck{Area: "firewall", Name: name, Status: doctorSkip, Detail: firstNonEmpty(command.Error, oneLine(command.Output), "nft list tables unavailable")}
	}
	present := parseNftTables(command.Stdout)
	var stale []string
	marked := 0
	unmarked := 0
	unverified := 0
	for _, table := range present {
		if !strings.HasPrefix(table.name, "routerd_") {
			continue
		}
		tableCommand := runDiagnosticCommand(ctx, "nft list table "+table.family+" "+table.name, "nft", "list", "table", table.family, table.name)
		if !tableCommand.OK {
			unverified++
			continue
		}
		if !strings.Contains(tableCommand.Stdout, render.NftablesRouterdOwnerMarker) {
			unmarked++
			continue
		}
		marked++
		if !expected[table.key()] {
			stale = append(stale, table.key())
		}
	}
	sort.Strings(stale)
	if len(stale) == 0 {
		detail := fmt.Sprintf("%d marked routerd-owned nft tables match current config", marked)
		if unmarked > 0 {
			detail = appendDoctorDetail(detail, fmt.Sprintf("%d unmarked routerd-prefixed tables ignored", unmarked))
		}
		if unverified > 0 {
			detail = appendDoctorDetail(detail, fmt.Sprintf("%d routerd-prefixed tables could not be inspected for ownership marker", unverified))
		}
		return doctorCheck{Area: "firewall", Name: name, Status: doctorPass, Detail: detail}
	}
	return doctorCheck{
		Area:   "firewall",
		Name:   name,
		Status: doctorWarn,
		Detail: "marked routerd-owned tables not expected by current config: " + strings.Join(stale, ","),
		Remedy: "inspect the listed nft tables and remove stale marked routerd-owned tables only after confirming they are not intentionally managed by this generation",
	}
}

type doctorNftTable struct {
	family string
	name   string
}

func (t doctorNftTable) key() string {
	return t.family + "/" + t.name
}

func parseNftTables(output string) []doctorNftTable {
	var tables []doctorNftTable
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 || fields[0] != "table" {
			continue
		}
		name := strings.TrimSuffix(fields[2], "{")
		tables = append(tables, doctorNftTable{family: fields[1], name: name})
	}
	return tables
}

func expectedRouterdNftTables(router *api.Router) (map[string]bool, error) {
	expected := map[string]bool{}
	if router == nil {
		return expected, nil
	}
	data, err := render.NftablesNAT44(router)
	if err != nil {
		return nil, err
	}
	for _, table := range parseNftTables(string(data)) {
		expected[table.key()] = true
	}
	for _, res := range selectResources(router.Spec.Resources, "EgressRoutePolicy", "") {
		spec, err := res.EgressRoutePolicySpec()
		if err != nil {
			return nil, err
		}
		switch strings.TrimSpace(spec.Mode) {
		case "priority":
			expected["ip/routerd_default_route"] = true
		case "mark", "hash":
			expected["ip/routerd_policy"] = true
		}
		for _, candidate := range spec.Candidates {
			if len(candidate.Targets) > 0 || candidate.Mark != 0 && strings.TrimSpace(spec.Mode) != "priority" {
				expected["ip/routerd_policy"] = true
			}
		}
	}
	return expected, nil
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
	checks := []doctorCheck{doctorTempDirPermissionsCheck(ctx)}
	command := runDiagnosticCommand(ctx, "df routerd runtime", "df", "-Pk", "/var/lib/routerd", "/run/routerd")
	if !command.OK {
		return append(checks, doctorCheck{Area: "disk", Name: command.Name, Status: doctorWarn, Detail: firstNonEmpty(command.Error, command.Output), Remedy: "check routerd runtime and state paths"})
	}
	return append(checks, doctorDFChecks(command.Output)...)
}

func doctorTempDirPermissionsCheck(ctx context.Context) doctorCheck {
	if doctorCurrentOS() != platform.OSLinux {
		return doctorCheck{Area: "disk", Name: "temporary directory permissions", Status: doctorSkip, Detail: "Linux host check not available on this OS"}
	}
	command := doctorRunDiagnosticCommand(ctx, "stat temporary directories", "env", "LC_ALL=C", "stat", "-c", "%a|%u|%g|%A|%F|%n", "/tmp", "/var/tmp")
	if !command.OK {
		return doctorCheck{Area: "disk", Name: command.Name, Status: doctorWarn, Detail: firstNonEmpty(command.Error, command.Output), Remedy: "inspect /tmp and /var/tmp permissions manually"}
	}
	type expectedTempDir struct {
		mode string
		uid  string
		gid  string
		typ  string
	}
	expected := map[string]expectedTempDir{
		"/tmp":     {mode: "1777", uid: "0", gid: "0", typ: "directory"},
		"/var/tmp": {mode: "1777", uid: "0", gid: "0", typ: "directory"},
	}
	seen := map[string]bool{}
	var problems []string
	for _, line := range strings.Split(strings.TrimSpace(command.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 6)
		if len(parts) != 6 {
			problems = append(problems, "unparseable stat output: "+line)
			continue
		}
		path := parts[5]
		want, ok := expected[path]
		if !ok {
			continue
		}
		seen[path] = true
		if parts[0] != want.mode || parts[1] != want.uid || parts[2] != want.gid || parts[4] != want.typ {
			problems = append(problems, fmt.Sprintf("%s mode=%s uid=%s gid=%s type=%s perms=%s want mode=%s uid=%s gid=%s type=%s", path, parts[0], parts[1], parts[2], parts[4], parts[3], want.mode, want.uid, want.gid, want.typ))
		}
	}
	for path := range expected {
		if !seen[path] {
			problems = append(problems, path+" missing from stat output")
		}
	}
	if len(problems) > 0 {
		return doctorCheck{
			Area:   "disk",
			Name:   "temporary directory permissions",
			Status: doctorFail,
			Detail: strings.Join(problems, "; "),
			Remedy: "identify the process that changed temporary directory ownership or mode before repairing it; /tmp and /var/tmp should be root:root 1777 sticky directories",
		}
	}
	return doctorCheck{Area: "disk", Name: "temporary directory permissions", Status: doctorPass, Detail: "/tmp and /var/tmp are root:root 1777 sticky directories"}
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
