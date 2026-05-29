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
	"github.com/imksoo/routerd/pkg/sam"
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

var doctorAreas = []string{"wan", "dns", "dslite", "dhcpv6-pd", "nat", "firewall", "rollback", "disk", "mgmt", "reconcile", "runtime", "dynamic", "plugin", "hybrid"}

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
	if opts.Target != "" && !validDoctorArea(opts.Target) {
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
	case "dynamic":
		return r.doctorDynamic()
	case "plugin":
		return r.doctorPlugin()
	case "hybrid":
		return r.doctorHybrid()
	default:
		return []doctorCheck{{Area: area, Name: "area", Status: doctorSkip, Detail: "unknown area"}}
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.Timeout)
	defer cancel()
	address := strings.TrimSpace(spec.Address)
	routeName := sam.DeliveryRouteName(name)
	tunnel := strings.TrimSpace(spec.Delivery.TunnelInterface)
	if tunnel == "" {
		tunnel = "delivery tunnel interface unresolved from OverlayPeer in doctor"
	}
	checks := []doctorCheck{
		doctorSAMIPForwardCheck(ctx, name),
		doctorSAMDeliveryRouteCheck(ctx, name, routeName, address, tunnel),
	}
	if strings.TrimSpace(spec.Capture.Type) == "proxy-arp" {
		checks = append(checks, doctorSAMProxyNeighborCheck(ctx, name, address, strings.TrimSpace(spec.Capture.Interface)))
		checks = append(checks, doctorSAMRPFilterCheck(ctx, name, strings.TrimSpace(spec.Capture.Interface)))
	}
	if iface := strings.TrimSpace(spec.Delivery.TunnelInterface); iface != "" {
		checks = append(checks, doctorSAMRPFilterCheck(ctx, name, iface))
	}
	if strings.TrimSpace(spec.Capture.Type) == "provider-secondary-ip" {
		checks = append(checks, doctorCheck{Area: "hybrid", Name: "RemoteAddressClaim/" + name + " provider capture", Status: doctorSkip, Detail: "cloud fabric secondary-IP assignment is external to routerd; checking only local forwarding and delivery route"})
	}
	return checks
}

func doctorSAMIPForwardCheck(ctx context.Context, name string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " ip_forward"
	command := doctorRunDiagnosticCommand(ctx, "sysctl net.ipv4.ip_forward", "sysctl", "-n", "net.ipv4.ip_forward")
	if command.OK && strings.TrimSpace(command.Stdout) == "1" {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: "net.ipv4.ip_forward=1"}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: firstNonEmpty(command.Error, oneLine(command.Output), "net.ipv4.ip_forward is not 1"), Remedy: "wait for routerd sysctl reconciliation or set net.ipv4.ip_forward=1"}
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

func doctorSAMProxyNeighborCheck(ctx context.Context, name, address, iface string) doctorCheck {
	label := "RemoteAddressClaim/" + name + " proxy neighbor"
	command := doctorRunDiagnosticCommand(ctx, "ip neigh show proxy "+address+" dev "+iface, "ip", "neigh", "show", "proxy", address, "dev", iface)
	if command.OK && strings.Contains(command.Stdout, strings.TrimSuffix(address, "/32")) {
		return doctorCheck{Area: "hybrid", Name: label, Status: doctorPass, Detail: oneLine(command.Stdout)}
	}
	return doctorCheck{Area: "hybrid", Name: label, Status: doctorWarn, Detail: firstNonEmpty(command.Error, oneLine(command.Output), "proxy neighbor not found"), Remedy: "wait for routerd SAM capture reconciliation or inspect proxy_arp and netlink neighbor state"}
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
