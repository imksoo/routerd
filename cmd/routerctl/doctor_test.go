// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDoctorDNSPassNoHost(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver", map[string]any{"phase": "Applied", "health": "HealthOK"}); err != nil {
		t.Fatalf("save dns status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "dns", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor dns: %v", err)
	}
	got := out.String()
	for _, want := range []string{"DOCTOR", "PASS", "DNSResolver/lan-resolver"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
}

func TestDoctorDHCPv6PDWarnDoesNotFail(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd", map[string]any{"phase": "Pending", "reason": "NoLease"}); err != nil {
		t.Fatalf("save pd status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "dhcpv6-pd", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor dhcpv6-pd returned error for warning: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "WARN") || !strings.Contains(got, "DHCPv6PrefixDelegation/wan-pd") {
		t.Fatalf("doctor output = %s", got)
	}
}

func TestDoctorFailReturnsError(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver", map[string]any{"phase": "Error", "reason": "RenderFailed"}); err != nil {
		t.Fatalf("save dns status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "dns", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor dns succeeded with failing check:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "FAIL") {
		t.Fatalf("doctor output = %s", out.String())
	}
}

func TestDoctorJSONOutputIncludesSummaryAndChecks(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver", map[string]any{"phase": "Applied", "health": "HealthOK"}); err != nil {
		t.Fatalf("save dns status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "dns", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor dns json: %v", err)
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	if report.Summary.Overall != doctorPass || report.Summary.Pass == 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if len(report.Checks) == 0 || report.Checks[0].Area != "dns" {
		t.Fatalf("checks = %#v", report.Checks)
	}
}

func TestDoctorTempDirPermissionsFailsWhenTmpIs0755(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	oldOS := doctorCurrentOS
	defer func() {
		doctorRunDiagnosticCommand = oldRun
		doctorCurrentOS = oldOS
	}()
	doctorCurrentOS = func() platform.OS { return platform.OSLinux }
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		return diagnoseCommandCheck{
			Name:   label,
			OK:     true,
			Stdout: "755|0|0|drwxr-xr-x|directory|/tmp\n1777|0|0|drwxrwxrwt|directory|/var/tmp\n755|0|0|drwxr-xr-x|directory|/var/spool\n",
		}
	}
	check := doctorTempDirPermissionsCheck(context.Background())
	if check.Status != doctorFail || !strings.Contains(check.Detail, "/tmp mode=755") {
		t.Fatalf("check = %#v", check)
	}
	if !strings.Contains(check.Remedy, "identify the process") {
		t.Fatalf("remedy should preserve root-cause investigation context: %#v", check)
	}
}

func TestDoctorTempDirPermissionsIgnoresVarSpool(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	oldOS := doctorCurrentOS
	defer func() {
		doctorRunDiagnosticCommand = oldRun
		doctorCurrentOS = oldOS
	}()
	doctorCurrentOS = func() platform.OS { return platform.OSLinux }
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		return diagnoseCommandCheck{
			Name:   label,
			OK:     true,
			Stdout: "1777|0|0|drwxrwxrwt|directory|/tmp\n1777|0|0|drwxrwxrwt|directory|/var/tmp\n777|0|0|lrwxrwxrwx|symbolic link|/var/spool\n",
		}
	}
	check := doctorTempDirPermissionsCheck(context.Background())
	if check.Status != doctorPass || strings.Contains(check.Detail, "/var/spool") {
		t.Fatalf("check = %#v, want /var/spool ignored", check)
	}
}

func TestDoctorSAMOwnershipConflictFailsNoHost(t *testing.T) {
	configPath, statePath := writeDoctorSAMFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":                   "Degraded",
		"plannerReason":                  "remote home owner overlaps local provider inventory",
		"ownershipResolverPhase":         "Conflict",
		"ownershipResolverConflictCount": 1,
		"ownershipResolverConflicts": []map[string]any{{
			"address":        "10.77.60.7/32",
			"homeOwnerNode":  "oci-router",
			"localNodeRef":   "azure-router",
			"conflictReason": "remote-home-owner-overlaps-local-inventory",
		}},
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor sam succeeded with ownership conflict:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge ownership conflicts")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "10.77.60.7/32") {
		t.Fatalf("check = %#v, want failing ownership conflict detail", check)
	}
}

func TestDoctorSAMBGPDeliveryFailsWhenBGPRouterDown(t *testing.T) {
	configPath, statePath := writeDoctorSAMBGPDeliveryFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                          "Ready",
		"plannerPhase":                   "BGPPlanned",
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"phase":            "Down",
		"establishedPeers": 0,
		"fibMissingRoutes": 0,
	}); err != nil {
		t.Fatalf("save bgp status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor sam should fail when BGP-delivery BGPRouter is down:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge BGP delivery BGPRouter/mobility-bgp")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "phase=Down") {
		t.Fatalf("BGP delivery check = %#v", check)
	}
}

func TestDoctorSAMBGPDeliveryFailsWhenFIBMissing(t *testing.T) {
	configPath, statePath := writeDoctorSAMBGPDeliveryFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                          "Ready",
		"plannerPhase":                   "BGPPlanned",
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"phase":                "Established",
		"establishedPeers":     1,
		"fibMissingRoutes":     2,
		"fibUnsupportedRoutes": 0,
	}); err != nil {
		t.Fatalf("save bgp status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor sam should fail when BGP-delivery FIB routes are missing:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge BGP delivery BGPRouter/mobility-bgp")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "fibMissingRoutes=2") {
		t.Fatalf("BGP delivery check = %#v", check)
	}
}

func TestDoctorSAMBGPDeliveryWarnsWhenDegradedButFIBInstalled(t *testing.T) {
	configPath, statePath := writeDoctorSAMBGPDeliveryFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                          "Ready",
		"plannerPhase":                   "BGPPlanned",
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"phase":                "Degraded",
		"establishedPeers":     8,
		"fibMissingRoutes":     0,
		"fibUnsupportedRoutes": 0,
	}); err != nil {
		t.Fatalf("save bgp status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam should warn, not fail, when BGP delivery FIB is installed: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge BGP delivery BGPRouter/mobility-bgp")
	if check.Status != doctorWarn || !strings.Contains(check.Detail, "phase=Degraded") {
		t.Fatalf("BGP delivery check = %#v, want warn for degraded peer establishment with installed FIB", check)
	}
}

func TestDoctorSAMBGPDeliveryPassesWhenBGPRouterEstablished(t *testing.T) {
	configPath, statePath := writeDoctorSAMBGPDeliveryFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                          "Ready",
		"plannerPhase":                   "BGPPlanned",
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"phase":                "Established",
		"establishedPeers":     1,
		"fibMissingRoutes":     0,
		"fibUnsupportedRoutes": 0,
	}); err != nil {
		t.Fatalf("save bgp status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam should pass when BGP delivery is established: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge BGP delivery BGPRouter/mobility-bgp")
	if check.Status != doctorPass || !strings.Contains(check.Detail, "establishedPeers=1") {
		t.Fatalf("BGP delivery check = %#v", check)
	}
}

func TestDoctorSAMOwnerTableRouteDriftFails(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	defer func() { doctorRunDiagnosticCommand = oldRun }()
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		if label == "ip -4 route show table main" {
			return diagnoseCommandCheck{
				Name:   label,
				OK:     true,
				Stdout: "10.77.60.7 via 10.255.0.28 dev samtc72ffb1610c proto bgp src 10.77.60.4 metric 200\n",
				Output: "10.77.60.7 via 10.255.0.28 dev samtc72ffb1610c proto bgp src 10.77.60.4 metric 200\n",
			}
		}
		return diagnoseCommandCheck{
			Name:   label,
			OK:     true,
			Stdout: "10.77.60.7 dev samtc72ffb1610c src 10.77.60.4 uid 1000",
			Output: "10.77.60.7 dev samtc72ffb1610c src 10.77.60.4 uid 1000",
		}
	}

	configPath, statePath := writeDoctorSAMFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":                   "BGPPlanned",
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
		"ownershipResolverOwnerTable": []map[string]any{{
			"address":          "10.77.60.7/32",
			"state":            "OK",
			"class":            "LocalHomeOwned",
			"ownerNode":        "azure-router",
			"localNode":        "azure-router",
			"localProviderRef": "azure-provider",
		}},
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor sam succeeded with route drift:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge owner-table route drift 10.77.60.7/32")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "samtc72ffb1610c") || !strings.Contains(check.Detail, "expected endpoint-owned local route") {
		t.Fatalf("check = %#v, want failing route drift detail", check)
	}
	if strings.Contains(check.Detail, "local/provider-owned") || strings.Contains(check.Remedy, "local/provider-owned") {
		t.Fatalf("check = %#v, must not use stale local/provider-owned wording", check)
	}
}

func TestDoctorSAMOwnerTableRouteDriftAllowsUnselectedOverlayRoute(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	defer func() { doctorRunDiagnosticCommand = oldRun }()
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		if label == "ip -4 route show table main" {
			return diagnoseCommandCheck{
				Name: label,
				OK:   true,
				Stdout: strings.Join([]string{
					"10.77.60.7 dev ens5 scope link metric 1",
					"10.77.60.7 via 10.255.0.28 dev samtc72ffb1610c proto bgp src 10.77.60.4 metric 200",
				}, "\n") + "\n",
				Output: strings.Join([]string{
					"10.77.60.7 dev ens5 scope link metric 1",
					"10.77.60.7 via 10.255.0.28 dev samtc72ffb1610c proto bgp src 10.77.60.4 metric 200",
				}, "\n") + "\n",
			}
		}
		return diagnoseCommandCheck{
			Name:   label,
			OK:     true,
			Stdout: "10.77.60.7 dev ens5 src 10.77.60.4 uid 1000",
			Output: "10.77.60.7 dev ens5 src 10.77.60.4 uid 1000",
		}
	}

	configPath, statePath := writeDoctorSAMFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":                   "BGPPlanned",
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
		"ownershipResolverOwnerTable": []map[string]any{{
			"address":          "10.77.60.7/32",
			"state":            "OK",
			"class":            "LocalHomeOwned",
			"ownerNode":        "azure-router",
			"localNode":        "azure-router",
			"localProviderRef": "azure-provider",
		}},
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge owner-table route drift 10.77.60.7/32")
	if check.Status != doctorPass || !strings.Contains(check.Detail, "dev ens5") || !strings.Contains(check.Detail, "samtc72ffb1610c") {
		t.Fatalf("check = %#v, want pass with selected local route and snapshot context", check)
	}
}

func TestDoctorSAMOwnerTableRouteDriftSkipsRemoteCaptureHolder(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	defer func() { doctorRunDiagnosticCommand = oldRun }()
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		if label == "ip -4 route show table main" {
			return diagnoseCommandCheck{
				Name:   label,
				OK:     true,
				Stdout: "10.77.60.7 via 10.255.0.28 dev samtc72ffb1610c proto bgp src 10.77.60.4 metric 200\n",
				Output: "10.77.60.7 via 10.255.0.28 dev samtc72ffb1610c proto bgp src 10.77.60.4 metric 200\n",
			}
		}
		t.Fatalf("unexpected diagnostic command %q %s", label, strings.Join(args, " "))
		return diagnoseCommandCheck{}
	}

	configPath, statePath := writeDoctorSAMFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":                   "BGPPlanned",
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
		"ownershipResolverOwnerTable": []map[string]any{{
			"address":          "10.77.60.7/32",
			"state":            "OK",
			"class":            "RemoteHomeOwned",
			"ownerNode":        "onprem-router",
			"localNode":        "azure-router",
			"localProviderRef": "azure-provider",
			"localNICRef":      "azure-nic",
		}},
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	assertDoctorCheckAbsent(t, report, "MobilityPool/cloudedge owner-table route drift 10.77.60.7/32")
}

func TestDoctorSAMOwnerTableReportsUnexpectedRouteResidue(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	defer func() { doctorRunDiagnosticCommand = oldRun }()
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		if label == "ip -4 route show table main" {
			return diagnoseCommandCheck{
				Name: label,
				OK:   true,
				Stdout: strings.Join([]string{
					"10.77.60.7 dev ens5 scope link metric 1",
					"10.77.60.1 dev ens5 proto dhcp scope link src 10.77.60.4 metric 100",
					"10.77.60.2 dev ens5 proto dhcp scope link src 10.77.60.4 metric 100",
					"10.77.60.3 dev ens5 proto dhcp scope link src 10.77.60.4 metric 100",
					"10.77.60.3 dev ens5 scope link metric 1",
					"10.77.60.4 dev ens5 proto dhcp scope global src 10.77.60.4 metric 100",
					"10.77.60.8 dev ens5 scope link metric 1",
					"10.88.60.9 dev ens5 scope link metric 1",
				}, "\n") + "\n",
				Output: strings.Join([]string{
					"10.77.60.7 dev ens5 scope link metric 1",
					"10.77.60.1 dev ens5 proto dhcp scope link src 10.77.60.4 metric 100",
					"10.77.60.2 dev ens5 proto dhcp scope link src 10.77.60.4 metric 100",
					"10.77.60.3 dev ens5 proto dhcp scope link src 10.77.60.4 metric 100",
					"10.77.60.3 dev ens5 scope link metric 1",
					"10.77.60.4 dev ens5 proto dhcp scope global src 10.77.60.4 metric 100",
					"10.77.60.8 dev ens5 scope link metric 1",
					"10.88.60.9 dev ens5 scope link metric 1",
				}, "\n") + "\n",
			}
		}
		return diagnoseCommandCheck{
			Name:   label,
			OK:     true,
			Stdout: "10.77.60.7 dev ens5 src 10.77.60.4 uid 1000",
			Output: "10.77.60.7 dev ens5 src 10.77.60.4 uid 1000",
		}
	}

	configPath, statePath := writeDoctorSAMFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":                   "BGPPlanned",
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
		"ownershipResolverOwnerTable": []map[string]any{{
			"address":          "10.77.60.7/32",
			"state":            "OK",
			"class":            "LocalHomeOwned",
			"ownerNode":        "azure-router",
			"localNode":        "azure-router",
			"localProviderRef": "azure-provider",
		}},
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor sam succeeded with unexpected route residue:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge owner-table unexpected route residue 10.77.60.8/32")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "absent from ownershipResolverOwnerTable") || !strings.Contains(check.Detail, "10.77.60.8 dev ens5") {
		t.Fatalf("check = %#v, want failing unexpected residue detail", check)
	}
	mixed := findDoctorCheck(t, report, "MobilityPool/cloudedge owner-table unexpected route residue 10.77.60.3/32")
	if mixed.Status != doctorFail || !strings.Contains(mixed.Detail, "proto dhcp scope link") || !strings.Contains(mixed.Detail, "scope link metric 1") {
		t.Fatalf("mixed check = %#v, want mixed DHCP/stale route to fail", mixed)
	}
	global := findDoctorCheck(t, report, "MobilityPool/cloudedge owner-table unexpected route residue 10.77.60.4/32")
	if global.Status != doctorFail || !strings.Contains(global.Detail, "scope global") {
		t.Fatalf("global check = %#v, want non-link DHCP route to fail", global)
	}
	assertDoctorCheckAbsent(t, report, "MobilityPool/cloudedge owner-table unexpected route residue 10.77.60.1/32")
	assertDoctorCheckAbsent(t, report, "MobilityPool/cloudedge owner-table unexpected route residue 10.77.60.2/32")
	assertDoctorCheckAbsent(t, report, "MobilityPool/cloudedge owner-table unexpected route residue 10.88.60.9/32")
}

func TestDoctorSAMOwnerTableIgnoresObservedBGPReturnRouteResidue(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	defer func() { doctorRunDiagnosticCommand = oldRun }()
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		if label == "ip -4 route show table main" {
			return diagnoseCommandCheck{
				Name:   label,
				OK:     true,
				Stdout: "10.77.60.7 dev ens5 scope link metric 1\n10.77.60.8 via 10.255.0.28 dev samtc72ffb1610c proto bgp src 10.77.60.4 metric 200\n",
				Output: "10.77.60.7 dev ens5 scope link metric 1\n10.77.60.8 via 10.255.0.28 dev samtc72ffb1610c proto bgp src 10.77.60.4 metric 200\n",
			}
		}
		return diagnoseCommandCheck{
			Name:   label,
			OK:     true,
			Stdout: "10.77.60.7 dev ens5 src 10.77.60.4 uid 1000",
			Output: "10.77.60.7 dev ens5 src 10.77.60.4 uid 1000",
		}
	}

	configPath, statePath := writeDoctorSAMFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":                   "BGPPlanned",
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
		"observedBGPReturnRoutes":        []string{"10.77.60.8/32"},
		"ownershipResolverOwnerTable": []map[string]any{{
			"address":          "10.77.60.7/32",
			"state":            "OK",
			"class":            "LocalHomeOwned",
			"ownerNode":        "azure-router",
			"localNode":        "azure-router",
			"localProviderRef": "azure-provider",
		}},
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	assertDoctorCheckAbsent(t, report, "MobilityPool/cloudedge owner-table unexpected route residue 10.77.60.8/32")
}

func TestDoctorSAMFederationDiscoveryWarnsWhenNoPeerDiscoveryEvents(t *testing.T) {
	configPath, statePath := writeDoctorSAMFederationFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":  "BGPPlanned",
		"plannerReason": "",
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	oldNow := doctorNow
	doctorNow = func() time.Time { return time.Date(2026, 6, 10, 15, 30, 0, 0, time.UTC) }
	defer func() { doctorNow = oldNow }()

	var out bytes.Buffer
	err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("doctor sam: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge federation discovery")
	if check.Status != doctorWarn {
		t.Fatalf("check = %#v, want warn", check)
	}
	if !strings.Contains(check.Detail, "aws-router-a") || !strings.Contains(check.Detail, "oci-router") {
		t.Fatalf("check detail should include peers, got: %#v", check.Detail)
	}
}

func TestDoctorSAMFederationDiscoveryPassesWithResolvedOwnerTable(t *testing.T) {
	configPath, statePath := writeDoctorSAMFederationFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                          "Ready",
		"plannerPhase":                   "BGPPlanned",
		"plannerReason":                  "deliveryPolicy.mode=bgp",
		"bgpRIBObserved":                 true,
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
		"ownershipResolverOwnerTable": []map[string]any{{
			"address":   "10.77.60.11/32",
			"state":     "OK",
			"ownerNode": "aws-router-a",
		}, {
			"address":   "10.77.60.12/32",
			"state":     "OK",
			"ownerNode": "oci-router",
		}},
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	oldNow := doctorNow
	doctorNow = func() time.Time { return time.Date(2026, 6, 10, 15, 30, 0, 0, time.UTC) }
	defer func() { doctorNow = oldNow }()

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	resourceCheck := findDoctorCheck(t, report, "MobilityPool/cloudedge")
	if resourceCheck.Status != doctorPass {
		t.Fatalf("resource check = %#v, want pass", resourceCheck)
	}
	discoveryCheck := findDoctorCheck(t, report, "MobilityPool/cloudedge federation discovery")
	if discoveryCheck.Status != doctorPass || !strings.Contains(discoveryCheck.Detail, "owner table") {
		t.Fatalf("federation check = %#v, want owner-table pass", discoveryCheck)
	}
}

func TestDoctorSAMFederationDiscoveryWarnsWithOnlyStaleOrUnknownOwnerTableRows(t *testing.T) {
	configPath, statePath := writeDoctorSAMFederationFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                          "Ready",
		"plannerPhase":                   "BGPPlanned",
		"plannerReason":                  "deliveryPolicy.mode=bgp",
		"bgpRIBObserved":                 true,
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
		"ownershipResolverOwnerTable": []map[string]any{{
			"address":           "10.77.60.11/32",
			"state":             "Stale",
			"class":             "StaleCapture",
			"suppressionReason": "capture-not-desired",
		}, {
			"address": "10.77.60.12/32",
			"state":   "Unknown",
			"class":   "Unknown",
			"source":  "bgp-rib",
		}},
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	oldNow := doctorNow
	doctorNow = func() time.Time { return time.Date(2026, 6, 10, 15, 30, 0, 0, time.UTC) }
	defer func() { doctorNow = oldNow }()

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge federation discovery")
	if check.Status != doctorWarn || strings.Contains(check.Detail, "owner table") {
		t.Fatalf("federation check = %#v, want stale/unknown owner table rows to keep freshness warning", check)
	}
}

func TestDoctorSAMStaleCaptureEvidenceWarns(t *testing.T) {
	configPath, statePath := writeDoctorSAMFederationFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":                          "Ready",
		"plannerPhase":                   "BGPPlanned",
		"plannerReason":                  "deliveryPolicy.mode=bgp",
		"bgpRIBObserved":                 true,
		"ownershipResolverPhase":         "Resolved",
		"ownershipResolverConflictCount": 0,
		"ownershipResolverOwnerTable": []map[string]any{{
			"address":           "10.77.60.11/32",
			"state":             "Stale",
			"class":             "StaleCapture",
			"suppressionReason": "capture-not-desired",
		}},
		"ownershipResolverStaleCount": 1,
		"ownershipResolverStaleClaims": []map[string]any{{
			"address":           "10.77.60.11/32",
			"state":             "Stale",
			"class":             "StaleCapture",
			"captureState":      "Stale",
			"captureHolderNode": "aws-router-a",
			"suppressionReason": "capture-not-desired",
		}},
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge stale capture evidence")
	if check.Status != doctorWarn || !strings.Contains(check.Detail, "10.77.60.11/32") || !strings.Contains(check.Remedy, "dataplane") {
		t.Fatalf("stale capture evidence check = %#v, want diagnostic warning with dataplane remedy", check)
	}
}

func TestDoctorSAMFederationDiscoveryPassesWhenPeerDiscoveryEventsPresent(t *testing.T) {
	configPath, statePath := writeDoctorSAMFederationFixture(t)
	store := openDoctorState(t, statePath)
	now := time.Date(2026, 6, 10, 15, 30, 0, 0, time.UTC)
	recordFederationEvent(t, store, routerstate.EventRecord{
		ID:         "e-aws-a",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       "routerd.client.ipv4.observed",
		Subject:    "10.77.60.11/32",
		Payload: map[string]string{
			"source":  "provider-discovery",
			"pool":    "cloudedge",
			"address": "10.77.60.11",
		},
		ObservedAt: now,
	})
	recordFederationEvent(t, store, routerstate.EventRecord{
		ID:         "e-oci-1",
		Group:      "cloudedge",
		SourceNode: "oci-router",
		Type:       "routerd.client.ipv4.observed",
		Subject:    "10.77.60.12/32",
		Payload: map[string]string{
			"source":  "provider-discovery",
			"pool":    "cloudedge",
			"address": "10.77.60.12",
		},
		ObservedAt: now,
	})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":  "BGPPlanned",
		"plannerReason": "",
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	oldNow := doctorNow
	doctorNow = func() time.Time { return now.Add(2 * time.Minute) }
	defer func() { doctorNow = oldNow }()

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge federation discovery")
	if check.Status != doctorPass {
		t.Fatalf("check = %#v, want pass", check)
	}
}

func TestDoctorSAMFederationDiscoveryWarnsWhenEventsAreStale(t *testing.T) {
	configPath, statePath := writeDoctorSAMFederationFixture(t)
	store := openDoctorState(t, statePath)
	now := time.Date(2026, 6, 10, 15, 30, 0, 0, time.UTC)
	recordFederationEvent(t, store, routerstate.EventRecord{
		ID:         "e-aws-stale",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       "routerd.client.ipv4.observed",
		Subject:    "10.77.60.11/32",
		Payload: map[string]string{
			"source":  "provider-discovery",
			"pool":    "cloudedge",
			"address": "10.77.60.11",
		},
		ObservedAt: now.Add(-20 * time.Minute),
	})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":  "BGPPlanned",
		"plannerReason": "",
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	oldNow := doctorNow
	doctorNow = func() time.Time { return now }
	defer func() { doctorNow = oldNow }()

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge federation discovery")
	if check.Status != doctorWarn {
		t.Fatalf("check = %#v, want warn", check)
	}
}

func TestDoctorReconcileHistoricalErrorsWarnButDoNotFail(t *testing.T) {
	oldFetcher := reconcileStatusFetcher
	defer func() { reconcileStatusFetcher = oldFetcher }()
	reconcileStatusFetcher = func(string, time.Duration) ([]controlapi.ControllerStatus, error) {
		return []controlapi.ControllerStatus{{
			Name: "dhcpv4-lease",
			ReconcileErrorHistory: []controlapi.ReconcileErrorEntry{
				{CompletedAt: time.Now().UTC(), Trigger: "periodic", Error: "socket unavailable"},
			},
		}}, nil
	}

	configPath, statePath := writeDoctorFixture(t)
	var out bytes.Buffer
	if err := run([]string{"doctor", "reconcile", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("historical reconcile errors should warn, not fail: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	if report.Summary.Overall != doctorWarn || report.Summary.Fail != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func TestDoctorReconcileCurrentErrorsFail(t *testing.T) {
	oldFetcher := reconcileStatusFetcher
	defer func() { reconcileStatusFetcher = oldFetcher }()
	reconcileStatusFetcher = func(string, time.Duration) ([]controlapi.ControllerStatus, error) {
		return []controlapi.ControllerStatus{{
			Name:         "dhcpv4-lease",
			CurrentError: true,
			ReconcileErrorHistory: []controlapi.ReconcileErrorEntry{
				{CompletedAt: time.Now().UTC(), Trigger: "periodic", Error: "socket unavailable"},
			},
		}}, nil
	}

	configPath, statePath := writeDoctorFixture(t)
	var out bytes.Buffer
	err := run([]string{"doctor", "reconcile", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("current reconcile errors should fail:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	if report.Summary.Overall != doctorFail || report.Summary.Fail != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
}

func TestDoctorDynamicHealthyMaskPolicyPasses(t *testing.T) {
	configPath, statePath := writeDoctorDynamicFixture(t, true)
	now := time.Now().UTC()
	store := openDoctorState(t, statePath)
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:         "Plugin/cloud",
		Generation:     1,
		ObservedAt:     now,
		ExpiresAt:      now.Add(time.Hour),
		Digest:         "sha256:dynamic",
		ResourcesJSON:  `[{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4Route","metadata":{"name":"cloud-route"},"spec":{"destination":"10.20.0.0/24","gateway":"192.0.2.1"}}]`,
		DirectivesJSON: `[{"op":"mask","target":{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4Route","name":"static-cloud-route"},"reason":"cloud route observed"}]`,
		Status:         "active",
	}); err != nil {
		t.Fatalf("upsert dynamic part: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "dynamic", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor dynamic: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	for _, name := range []string{"dynamic parts decode", "expired parts ignored", "effective config builds", "override policies present for masks"} {
		check := findDoctorCheck(t, report, name)
		if check.Status != doctorPass {
			t.Fatalf("%s check = %#v", name, check)
		}
	}
	if check := findDoctorCheck(t, report, "effective config builds"); !strings.Contains(check.Detail, "1 suppressed, 1 dynamic resources added") {
		t.Fatalf("effective detail = %q", check.Detail)
	}
}

func TestDoctorDynamicMaskWithoutPolicyFailsEffectiveBuild(t *testing.T) {
	configPath, statePath := writeDoctorDynamicFixture(t, false)
	now := time.Now().UTC()
	store := openDoctorState(t, statePath)
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:         "Plugin/cloud",
		Generation:     1,
		ObservedAt:     now,
		ExpiresAt:      now.Add(time.Hour),
		Digest:         "sha256:dynamic",
		ResourcesJSON:  `[]`,
		DirectivesJSON: `[{"op":"mask","target":{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4Route","name":"static-cloud-route"},"reason":"cloud route observed"}]`,
		Status:         "active",
	}); err != nil {
		t.Fatalf("upsert dynamic part: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "dynamic", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor dynamic succeeded with disallowed mask:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "effective config builds")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "dynamic mask not allowed") {
		t.Fatalf("effective check = %#v", check)
	}
}

func TestDoctorRoutesDetectsIPv4RouteDeviceMetricDrift(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	defer func() { doctorRunDiagnosticCommand = oldRun }()
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		if label == "ip -4 route show 10.44.0.0/24" {
			return diagnoseCommandCheck{
				Name:   label,
				OK:     true,
				Stdout: "10.44.0.0/24 via 192.0.2.1 dev eth1 metric 300\n",
			}
		}
		return diagnoseCommandCheck{Name: label, OK: true}
	}
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", "cloud-route", map[string]any{
		"phase":       "Installed",
		"type":        "unicast",
		"destination": "10.44.0.0/24",
		"gateway":     "192.0.2.1",
		"device":      "eth0",
		"metric":      200,
	}); err != nil {
		t.Fatalf("save route status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "routes", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor routes succeeded with route drift:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "IPv4Route/cloud-route host route")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "device=eth0") || !strings.Contains(check.Detail, "metric=200") || !strings.Contains(check.Detail, "dev eth1 metric 300") {
		t.Fatalf("check = %#v, want device/metric drift detail", check)
	}
}

func TestDoctorRoutesPassesMatchingIPv4Route(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	defer func() { doctorRunDiagnosticCommand = oldRun }()
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		if label == "ip -4 route show 10.44.0.0/24" {
			return diagnoseCommandCheck{
				Name:   label,
				OK:     true,
				Stdout: "10.44.0.0/24 via 192.0.2.1 dev eth0 src 10.44.0.1 metric 200\n",
			}
		}
		return diagnoseCommandCheck{Name: label, OK: true}
	}
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", "cloud-route", map[string]any{
		"phase":                    "Installed",
		"type":                     "unicast",
		"destination":              "10.44.0.0/24",
		"gateway":                  "192.0.2.1",
		"device":                   "eth0",
		"effectivePreferredSource": "10.44.0.1",
		"metric":                   200,
	}); err != nil {
		t.Fatalf("save route status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "routes", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor routes: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "IPv4Route/cloud-route host route")
	if check.Status != doctorPass {
		t.Fatalf("check = %#v, want pass", check)
	}
}

func TestDoctorRoutesHonorsPreferredSourceSkipped(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	defer func() { doctorRunDiagnosticCommand = oldRun }()
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		if label == "ip -4 route show 10.44.0.0/24" {
			return diagnoseCommandCheck{
				Name:   label,
				OK:     true,
				Stdout: "10.44.0.0/24 via 192.0.2.1 dev eth0 metric 200\n",
			}
		}
		return diagnoseCommandCheck{Name: label, OK: true}
	}
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", "cloud-route", map[string]any{
		"phase":                  "Installed",
		"type":                   "unicast",
		"destination":            "10.44.0.0/24",
		"gateway":                "192.0.2.1",
		"device":                 "eth0",
		"preferredSource":        "10.44.0.1",
		"preferredSourceSkipped": true,
		"metric":                 200,
	}); err != nil {
		t.Fatalf("save route status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "routes", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor routes: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "IPv4Route/cloud-route host route")
	if check.Status != doctorPass {
		t.Fatalf("check = %#v, want pass", check)
	}
	if strings.Contains(check.Detail, "preferredSource=10.44.0.1") {
		t.Fatalf("check detail = %q, should not require skipped preferred source", check.Detail)
	}
}

func TestDoctorRoutesSkipsHostChecksWithNoHost(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", "cloud-route", map[string]any{
		"phase":       "Installed",
		"destination": "10.44.0.0/24",
		"device":      "eth0",
		"metric":      200,
	}); err != nil {
		t.Fatalf("save route status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "routes", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor routes --no-host: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "IPv4Route/cloud-route host route")
	if check.Status != doctorSkip {
		t.Fatalf("check = %#v, want skip", check)
	}
}

func TestDoctorPluginExecutableRunAndFreshnessChecks(t *testing.T) {
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "cloud-plugin.sh")
	writeTestCommand(t, pluginPath, "#!/bin/sh\nexit 0\n")
	missingPath := filepath.Join(dir, "missing-plugin.sh")
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: plugin.routerd.net/v1alpha1
      kind: Plugin
      metadata:
        name: cloud
      spec:
        executable: `+pluginPath+`
        timeout: 2s
        capabilities: [observe.cloud, propose.dynamicConfig]
    - apiVersion: plugin.routerd.net/v1alpha1
      kind: Plugin
      metadata:
        name: missing
      spec:
        executable: `+missingPath+`
        timeout: 2s
        capabilities: [observe.cloud, propose.dynamicConfig]
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	statePath := filepath.Join(dir, "routerd.db")
	now := time.Now().UTC()
	store := openDoctorState(t, statePath)
	runID, err := store.RecordPluginRun(routerstate.PluginRunRecord{
		Plugin:      "cloud",
		TriggerType: "manual",
		StartedAt:   now.Add(-time.Minute),
		Status:      "running",
	})
	if err != nil {
		t.Fatalf("record plugin run: %v", err)
	}
	exitCode := 0
	if err := store.CompletePluginRun(runID, now, &exitCode, "succeeded", "sha256:stdout", "", ""); err != nil {
		t.Fatalf("complete plugin run: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:         "Plugin/cloud",
		Generation:     1,
		ObservedAt:     now,
		ExpiresAt:      now.Add(time.Hour),
		Digest:         "sha256:dynamic",
		ResourcesJSON:  `[]`,
		DirectivesJSON: `[]`,
		Status:         "active",
	}); err != nil {
		t.Fatalf("upsert dynamic part: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err = run([]string{"doctor", "plugin", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor plugin succeeded despite missing executable:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	for _, name := range []string{"Plugin/cloud executable exists", "Plugin/cloud executable is executable", "Plugin/cloud last run", "Plugin/cloud last result fresh"} {
		if check := findDoctorCheck(t, report, name); check.Status != doctorPass {
			t.Fatalf("%s check = %#v", name, check)
		}
	}
	if check := findDoctorCheck(t, report, "Plugin/missing executable exists"); check.Status != doctorFail {
		t.Fatalf("missing executable check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "Plugin/missing last run"); check.Status != doctorWarn || !strings.Contains(check.Detail, "never run") {
		t.Fatalf("missing last run check = %#v", check)
	}
}

func TestDoctorDSLiteSelectedHealthyPolicyIgnoresAFTRProbeFailure(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Applied", "health": "HealthOK"}, "ds-lite", map[string]any{"phase": "Applied", "selectedCandidate": "ds-lite", "selectedDevice": "ds-routerd"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	if report.Summary.Warn != 0 || report.Summary.Fail != 0 {
		t.Fatalf("summary = %#v checks = %#v", report.Summary, report.Checks)
	}
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorPass || !strings.Contains(check.Detail, "selected via EgressRoutePolicy/ipv4-default") {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteUpSelectedSourceAggregateCandidatePasses(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Up"}, "dslite-pd-balanced", map[string]any{"phase": "Applied", "selectedSource": "DSLiteTunnel/ds-lite", "selectedTargets": 3}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	if report.Summary.Overall != doctorPass || report.Summary.Warn != 0 || report.Summary.Fail != 0 {
		t.Fatalf("summary = %#v checks = %#v", report.Summary, report.Checks)
	}
	resourceCheck := findDoctorCheck(t, report, "DSLiteTunnel/ds-lite")
	if resourceCheck.Status != doctorPass {
		t.Fatalf("DSLiteTunnel check = %#v", resourceCheck)
	}
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorPass || !strings.Contains(check.Detail, "selected via EgressRoutePolicy/ipv4-default, gatewayHealth-aligned") {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteSelectedHealthyPolicyWithAFTRProbeSuccessPasses(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Applied", "health": "HealthOK"}, "ds-lite", map[string]any{"phase": "Applied", "selectedCandidate": "ds-lite", "selectedDevice": "ds-routerd"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, true, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	if report.Summary.Warn != 0 || report.Summary.Fail != 0 {
		t.Fatalf("summary = %#v checks = %#v", report.Summary, report.Checks)
	}
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorPass {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteNotSelectedWarnsOnAFTRProbeFailure(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Applied", "health": "HealthOK"}, "pppoe", map[string]any{"phase": "Applied", "selectedCandidate": "pppoe", "selectedDevice": "ppp0"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorWarn || strings.Contains(check.Detail, "selected via") {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteUpSelectedSourceMismatchWarnsOnAFTRProbeFailure(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Up"}, "dslite-pd-balanced", map[string]any{"phase": "Applied", "selectedSource": "DSLiteTunnel/other", "selectedDevice": "other-device"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorWarn || strings.Contains(check.Detail, "selected via") {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteUpSelectedSourceUnhealthyPolicyWarnsOnAFTRProbeFailure(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Up"}, "dslite-pd-balanced", map[string]any{"phase": "Pending", "selectedSource": "DSLiteTunnel/ds-lite"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorWarn || strings.Contains(check.Detail, "selected via") {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteDownStatusIsNotOverriddenBySelectedPolicy(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Error", "reason": "TunnelApplyFailed"}, "ds-lite", map[string]any{"phase": "Applied", "selectedCandidate": "ds-lite", "selectedDevice": "ds-routerd"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	var out bytes.Buffer
	err := run([]string{"doctor", "dslite", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor dslite succeeded with down tunnel:\n%s", out.String())
	}
	var report doctorReport
	if unmarshalErr := json.Unmarshal(out.Bytes(), &report); unmarshalErr != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", unmarshalErr, out.String())
	}
	check := findDoctorCheck(t, report, "DSLiteTunnel/ds-lite")
	if check.Status != doctorFail {
		t.Fatalf("DSLiteTunnel check = %#v", check)
	}
	aftrCheck := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if aftrCheck.Status != doctorWarn || strings.Contains(aftrCheck.Detail, "selected via") {
		t.Fatalf("AFTR check = %#v", aftrCheck)
	}
}

func TestDoctorHybridHealthyNoHost(t *testing.T) {
	configPath, statePath := writeDoctorHybridFixture(t, false)
	var out bytes.Buffer
	if err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor hybrid: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	if report.Summary.Fail != 0 || report.Summary.Pass == 0 {
		t.Fatalf("summary = %#v checks=%#v", report.Summary, report.Checks)
	}
	if check := findDoctorCheck(t, report, "HybridRoute/cloud-private peerRef"); check.Status != doctorPass {
		t.Fatalf("peerRef check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "HybridRoute/cloud-private default route untouched"); check.Status != doctorPass {
		t.Fatalf("default route check = %#v", check)
	}
}

func TestDoctorHybridAddressMobilityNoHost(t *testing.T) {
	configPath, statePath := writeDoctorAddressMobilityFixture(t)
	var out bytes.Buffer
	if err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor hybrid: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm domainRef"); check.Status != doctorPass {
		t.Fatalf("domainRef check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm delivery.peerRef"); check.Status != doctorPass {
		t.Fatalf("delivery.peerRef check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm SAM dataplane"); check.Status != doctorSkip || !strings.Contains(check.Detail, "--no-host") {
		t.Fatalf("dataplane check = %#v", check)
	}
}

func TestDoctorHybridSAMLiveChecksStubbed(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	oldOS := doctorCurrentOS
	defer func() {
		doctorRunDiagnosticCommand = oldRun
		doctorCurrentOS = oldOS
	}()
	doctorCurrentOS = func() platform.OS { return platform.OSLinux }
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		switch {
		case label == "sysctl net.ipv4.ip_forward":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "1", Output: "1"}
		case strings.HasPrefix(label, "ip route show"):
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "10.0.0.9 dev wg-hybrid", Output: "10.0.0.9 dev wg-hybrid"}
		case strings.HasPrefix(label, "ip route get"):
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "10.0.0.9 dev wg-hybrid src 10.0.0.4", Output: "10.0.0.9 dev wg-hybrid src 10.0.0.4"}
		case label == "ip link show wg-hybrid":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "3: wg-hybrid: <POINTOPOINT,UP> mtu 1420", Output: "3: wg-hybrid: <POINTOPOINT,UP> mtu 1420"}
		case strings.HasPrefix(label, "ip addr show"):
			return diagnoseCommandCheck{Name: label, OK: true}
		case label == "nft list table inet routerd_mss":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: `table inet routerd_mss {
 chain forward {
  iifname "eth0" oifname "wg-hybrid" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1380 tcp option maxseg size set 1380
 }
}`, Output: "table inet routerd_mss"}
		case label == "nft list table inet routerd_filter":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "table inet routerd_filter {\n chain forward { type filter hook forward priority 0; policy accept; }\n}", Output: "table inet routerd_filter"}
		case label == "iptables -S INPUT":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "-P INPUT ACCEPT\n-A INPUT -p udp --dport 51820 -j ACCEPT", Output: "-P INPUT ACCEPT"}
		case label == "iptables -S FORWARD":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "-P FORWARD ACCEPT\n-A FORWARD -i eth0 -o wg-hybrid -j ACCEPT\n-A FORWARD -i wg-hybrid -o eth0 -j ACCEPT", Output: "-P FORWARD ACCEPT"}
		case strings.Contains(label, "rp_filter"):
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "1", Output: "1"}
		default:
			return diagnoseCommandCheck{Name: label, OK: false, Error: "unexpected command"}
		}
	}
	configPath, statePath := writeDoctorAddressMobilityFixture(t)
	var out bytes.Buffer
	if err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor hybrid: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm ip_forward"); check.Status != doctorPass {
		t.Fatalf("ip_forward check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm delivery route"); check.Status != doctorPass {
		t.Fatalf("delivery route check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm rp_filter wg-hybrid"); check.Status != doctorWarn || !strings.Contains(check.Remedy, "loose") {
		t.Fatalf("rp_filter check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm local OS address"); check.Status != doctorPass {
		t.Fatalf("local OS address check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm route get"); check.Status != doctorPass {
		t.Fatalf("route get check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm MSS clamp"); check.Status != doctorPass {
		t.Fatalf("MSS clamp check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm host firewall"); check.Status != doctorPass {
		t.Fatalf("host firewall check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm FORWARD policy"); check.Status != doctorPass {
		t.Fatalf("FORWARD policy check = %#v", check)
	}
}

func TestDoctorHybridSAMProxyARPInterfaceLiveChecksStubbed(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	oldOS := doctorCurrentOS
	defer func() {
		doctorRunDiagnosticCommand = oldRun
		doctorCurrentOS = oldOS
	}()
	doctorCurrentOS = func() platform.OS { return platform.OSLinux }

	for _, tc := range []struct {
		name       string
		linkExists bool
		wantErr    bool
		wantStatus string
	}{
		{name: "present", linkExists: true, wantStatus: doctorPass},
		{name: "missing", linkExists: false, wantErr: true, wantStatus: doctorFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
				switch {
				case label == "sysctl net.ipv4.ip_forward":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "1", Output: "1"}
				case strings.HasPrefix(label, "ip route show"):
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "10.0.0.7 dev wg-hybrid", Output: "10.0.0.7 dev wg-hybrid"}
				case strings.HasPrefix(label, "ip route get"):
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "10.0.0.7 dev wg-hybrid src 10.0.0.4", Output: "10.0.0.7 dev wg-hybrid src 10.0.0.4"}
				case label == "ip link show wg-hybrid":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "3: wg-hybrid: <POINTOPOINT,UP> mtu 1420", Output: "3: wg-hybrid: <POINTOPOINT,UP> mtu 1420"}
				case label == "ip link show br-lan" && tc.linkExists:
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "2: br-lan: <BROADCAST,MULTICAST,UP>", Output: "2: br-lan: <BROADCAST,MULTICAST,UP>"}
				case label == "ip link show br-lan":
					return diagnoseCommandCheck{Name: label, OK: false, Error: "Device \"br-lan\" does not exist.", Output: "Device \"br-lan\" does not exist."}
				case label == "nft list table inet routerd_mss":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: `table inet routerd_mss {
 chain forward {
  iifname "br-lan" oifname "wg-hybrid" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1380 tcp option maxseg size set 1380
 }
}`, Output: "table inet routerd_mss"}
				case label == "sysctl net.ipv4.conf.br-lan.proxy_arp":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "1", Output: "1"}
				case strings.HasPrefix(label, "ip neigh show proxy"):
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "10.0.0.7 dev br-lan proxy", Output: "10.0.0.7 dev br-lan proxy"}
				case label == "nft list table inet routerd_filter":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "table inet routerd_filter {\n chain forward { type filter hook forward priority 0; policy accept; }\n}", Output: "table inet routerd_filter"}
				case label == "iptables -S INPUT":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "-P INPUT ACCEPT\n-A INPUT -p udp --dport 51820 -j ACCEPT", Output: "-P INPUT ACCEPT"}
				case label == "iptables -S FORWARD":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "-P FORWARD ACCEPT\n-A FORWARD -i br-lan -o wg-hybrid -j ACCEPT\n-A FORWARD -i wg-hybrid -o br-lan -j ACCEPT", Output: "-P FORWARD ACCEPT"}
				case strings.Contains(label, "rp_filter"):
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "0", Output: "0"}
				default:
					return diagnoseCommandCheck{Name: label, OK: false, Error: "unexpected command"}
				}
			}
			configPath, statePath := writeDoctorProxyARPAddressMobilityFixture(t)
			var out bytes.Buffer
			err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{})
			if tc.wantErr && err == nil {
				t.Fatalf("doctor hybrid succeeded with missing interface:\n%s", out.String())
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("doctor hybrid: %v\n%s", err, out.String())
			}
			var report doctorReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
			}
			check := findDoctorCheck(t, report, "RemoteAddressClaim/cloud-vm proxy-arp interface")
			if check.Status != tc.wantStatus {
				t.Fatalf("capture interface check = %#v", check)
			}
			if tc.wantStatus == doctorFail && (!strings.Contains(check.Detail, "br-lan not found") || !strings.Contains(check.Remedy, "proxy_arp")) {
				t.Fatalf("capture interface detail/remedy = %#v", check)
			}
		})
	}
}

func TestDoctorHybridSAMProviderLocalAddressWarnsWhenPresent(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	oldOS := doctorCurrentOS
	defer func() {
		doctorRunDiagnosticCommand = oldRun
		doctorCurrentOS = oldOS
	}()
	doctorCurrentOS = func() platform.OS { return platform.OSLinux }
	doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
		switch {
		case label == "sysctl net.ipv4.ip_forward":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "1", Output: "1"}
		case strings.HasPrefix(label, "ip route show"):
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "10.0.0.9 dev wg-hybrid", Output: "10.0.0.9 dev wg-hybrid"}
		case strings.HasPrefix(label, "ip route get"):
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "local 10.0.0.9 dev lo src 10.0.0.9", Output: "local 10.0.0.9 dev lo src 10.0.0.9"}
		case label == "ip link show wg-hybrid":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "3: wg-hybrid: <POINTOPOINT,UP> mtu 1420", Output: "3: wg-hybrid: <POINTOPOINT,UP> mtu 1420"}
		case strings.HasPrefix(label, "ip addr show"):
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "2: eth0    inet 10.0.0.9/32 scope global eth0", Output: "2: eth0    inet 10.0.0.9/32 scope global eth0"}
		case label == "nft list table inet routerd_mss":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: `table inet routerd_mss {
 chain forward {
  iifname "eth0" oifname "wg-hybrid" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1380 tcp option maxseg size set 1380
 }
}`, Output: "table inet routerd_mss"}
		case label == "nft list table inet routerd_filter":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "table inet routerd_filter {\n chain forward { type filter hook forward priority 0; policy drop; }\n}", Output: "table inet routerd_filter"}
		case label == "iptables -S INPUT":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "-P INPUT ACCEPT\n-A INPUT -p udp --dport 51820 -j ACCEPT", Output: "-P INPUT ACCEPT"}
		case label == "iptables -S FORWARD":
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "-P FORWARD ACCEPT\n-A FORWARD -i eth0 -o wg-hybrid -j ACCEPT\n-A FORWARD -i wg-hybrid -o eth0 -j ACCEPT", Output: "-P FORWARD ACCEPT"}
		case strings.Contains(label, "rp_filter"):
			return diagnoseCommandCheck{Name: label, OK: true, Stdout: "0", Output: "0"}
		default:
			return diagnoseCommandCheck{Name: label, OK: false, Error: "unexpected command"}
		}
	}
	configPath, statePath := writeDoctorAddressMobilityFixture(t)
	var out bytes.Buffer
	if err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor hybrid: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm local OS address"); check.Status != doctorWarn || !strings.Contains(check.Remedy, "cloud-init/netplan") {
		t.Fatalf("local OS address check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm FORWARD policy"); check.Status != doctorWarn || !strings.Contains(check.Remedy, "permits SAM forwarding") {
		t.Fatalf("FORWARD policy check = %#v", check)
	}
}

func TestDoctorHybridSAMProxyARPInterfaceHostSkips(t *testing.T) {
	t.Run("no-host", func(t *testing.T) {
		configPath, statePath := writeDoctorProxyARPAddressMobilityFixture(t)
		var out bytes.Buffer
		if err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("doctor hybrid: %v\n%s", err, out.String())
		}
		var report doctorReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
		}
		if check := findDoctorCheck(t, report, "RemoteAddressClaim/cloud-vm SAM dataplane"); check.Status != doctorSkip || !strings.Contains(check.Detail, "--no-host") {
			t.Fatalf("dataplane check = %#v", check)
		}
		assertDoctorCheckAbsent(t, report, "RemoteAddressClaim/cloud-vm proxy-arp interface")
	})

	t.Run("non-linux", func(t *testing.T) {
		oldOS := doctorCurrentOS
		defer func() { doctorCurrentOS = oldOS }()
		doctorCurrentOS = func() platform.OS { return platform.OSFreeBSD }
		configPath, statePath := writeDoctorProxyARPAddressMobilityFixture(t)
		var out bytes.Buffer
		if err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("doctor hybrid: %v\n%s", err, out.String())
		}
		var report doctorReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
		}
		if check := findDoctorCheck(t, report, "RemoteAddressClaim/cloud-vm SAM dataplane"); check.Status != doctorSkip || !strings.Contains(check.Detail, "not implemented") {
			t.Fatalf("dataplane check = %#v", check)
		}
		assertDoctorCheckAbsent(t, report, "RemoteAddressClaim/cloud-vm proxy-arp interface")
	})
}

func TestDoctorHybridSAMVRRPGatedProxyARPDetectsInactiveArtifacts(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	oldOS := doctorCurrentOS
	defer func() {
		doctorRunDiagnosticCommand = oldRun
		doctorCurrentOS = oldOS
	}()
	doctorCurrentOS = func() platform.OS { return platform.OSLinux }

	for _, tc := range []struct {
		name               string
		proxyExists        bool
		proxyARPValue      string
		wantErr            bool
		wantProxyStatus    string
		wantProxyARPStatus string
	}{
		{name: "standby-clean", proxyARPValue: "0", wantProxyStatus: doctorSkip, wantProxyARPStatus: doctorPass},
		{name: "standby-stale-proxy", proxyExists: true, proxyARPValue: "0", wantErr: true, wantProxyStatus: doctorFail, wantProxyARPStatus: doctorPass},
		{name: "standby-route-based-proxy-arp", proxyARPValue: "1", wantErr: true, wantProxyStatus: doctorSkip, wantProxyARPStatus: doctorFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorRunDiagnosticCommand = func(_ context.Context, label, name string, args ...string) diagnoseCommandCheck {
				switch {
				case strings.HasPrefix(label, "ip neigh show proxy"):
					if tc.proxyExists {
						return diagnoseCommandCheck{Name: label, OK: true, Stdout: "10.0.0.7 dev br-lan proxy", Output: "10.0.0.7 dev br-lan proxy"}
					}
					return diagnoseCommandCheck{Name: label, OK: true}
				case label == "sysctl net.ipv4.conf.br-lan.proxy_arp":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: tc.proxyARPValue, Output: tc.proxyARPValue}
				}
				return diagnoseCommandCheck{Name: label, OK: false, Error: "unexpected command"}
			}
			configPath, statePath := writeDoctorVRRPGatedProxyARPAddressMobilityFixture(t)
			store := openDoctorState(t, statePath)
			if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "onprem-vip", map[string]any{"role": "backup"}); err != nil {
				t.Fatalf("save VirtualAddress status: %v", err)
			}
			closeDoctorState(t, store)

			var out bytes.Buffer
			err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{})
			if tc.wantErr && err == nil {
				t.Fatalf("doctor hybrid succeeded with inactive proxy-ARP artifact:\n%s", out.String())
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("doctor hybrid: %v\n%s", err, out.String())
			}
			var report doctorReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
			}
			if check := findDoctorCheck(t, report, "RemoteAddressClaim/cloud-vm activeWhen vrrp-master"); check.Status != doctorSkip {
				t.Fatalf("activeWhen check = %#v", check)
			}
			check := findDoctorCheck(t, report, "RemoteAddressClaim/cloud-vm proxy neighbor absent")
			if check.Status != tc.wantProxyStatus {
				t.Fatalf("proxy absent check = %#v", check)
			}
			disabled := findDoctorCheck(t, report, "RemoteAddressClaim/cloud-vm proxy_arp disabled")
			if disabled.Status != tc.wantProxyARPStatus {
				t.Fatalf("proxy_arp disabled check = %#v", disabled)
			}
		})
	}
}

func TestDoctorHybridSAMVRRPGatedProxyARPDetectsDuplicateResponders(t *testing.T) {
	oldRun := doctorRunDiagnosticCommand
	oldOS := doctorCurrentOS
	defer func() {
		doctorRunDiagnosticCommand = oldRun
		doctorCurrentOS = oldOS
	}()
	doctorCurrentOS = func() platform.OS { return platform.OSLinux }

	for _, tc := range []struct {
		name       string
		arpingOut  string
		wantErr    bool
		wantStatus string
		localMAC   string
	}{
		{
			name:       "no-responder-output",
			wantStatus: doctorPass,
		},
		{
			name:       "single-responder",
			arpingOut:  "Unicast reply from 10.0.0.7 [aa:bb:cc:dd:ee:01]  0.812ms",
			wantStatus: doctorPass,
		},
		{
			name:       "local-master-peer-responder",
			arpingOut:  "Unicast reply from 10.0.0.7 [aa:bb:cc:dd:ee:02]  0.812ms",
			wantErr:    true,
			wantStatus: doctorFail,
			localMAC:   "aa:bb:cc:dd:ee:01",
		},
		{
			name: "duplicate-responders",
			arpingOut: strings.Join([]string{
				"Unicast reply from 10.0.0.7 [aa:bb:cc:dd:ee:01]  0.812ms",
				"Unicast reply from 10.0.0.7 [aa:bb:cc:dd:ee:02]  0.953ms",
			}, "\n"),
			wantErr:    true,
			wantStatus: doctorFail,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorRunDiagnosticCommand = func(_ context.Context, label, cmdName string, args ...string) diagnoseCommandCheck {
				switch {
				case label == "sysctl net.ipv4.ip_forward":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "1", Output: "1"}
				case strings.HasPrefix(label, "ip route show"):
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "10.0.0.7 dev wg-hybrid", Output: "10.0.0.7 dev wg-hybrid"}
				case strings.HasPrefix(label, "ip route get"):
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "10.0.0.7 dev wg-hybrid src 10.0.0.4", Output: "10.0.0.7 dev wg-hybrid src 10.0.0.4"}
				case label == "ip link show wg-hybrid":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "3: wg-hybrid: <POINTOPOINT,UP> mtu 1420", Output: "3: wg-hybrid: <POINTOPOINT,UP> mtu 1420"}
				case label == "ip link show br-lan":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "2: br-lan: <BROADCAST,MULTICAST,UP>", Output: "2: br-lan: <BROADCAST,MULTICAST,UP>"}
				case label == "nft list table inet routerd_mss":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: `table inet routerd_mss {
 chain forward {
  iifname "br-lan" oifname "wg-hybrid" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1380 tcp option maxseg size set 1380
 }
}`, Output: "table inet routerd_mss"}
				case label == "sysctl net.ipv4.conf.br-lan.proxy_arp":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "1", Output: "1"}
				case strings.HasPrefix(label, "ip neigh show proxy"):
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "10.0.0.7 dev br-lan proxy", Output: "10.0.0.7 dev br-lan proxy"}
				case label == "arping 10.0.0.7 dev br-lan":
					if cmdName != "arping" || strings.Join(args, " ") != "-c 3 -w 2 -I br-lan 10.0.0.7" {
						t.Fatalf("unexpected arping command: %s %v", cmdName, args)
					}
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: tc.arpingOut, Output: tc.arpingOut}
				case label == "cat /sys/class/net/br-lan/address":
					if tc.localMAC != "" {
						return diagnoseCommandCheck{Name: label, OK: true, Stdout: tc.localMAC + "\n", Output: tc.localMAC + "\n"}
					}
					return diagnoseCommandCheck{Name: label, OK: false, Error: "not checked"}
				case label == "nft list table inet routerd_filter":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "table inet routerd_filter {\n chain forward { type filter hook forward priority 0; policy accept; }\n}", Output: "table inet routerd_filter"}
				case label == "iptables -S INPUT":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "-P INPUT ACCEPT\n-A INPUT -p udp --dport 51820 -j ACCEPT", Output: "-P INPUT ACCEPT"}
				case label == "iptables -S FORWARD":
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "-P FORWARD ACCEPT\n-A FORWARD -i br-lan -o wg-hybrid -j ACCEPT\n-A FORWARD -i wg-hybrid -o br-lan -j ACCEPT", Output: "-P FORWARD ACCEPT"}
				case strings.Contains(label, "rp_filter"):
					return diagnoseCommandCheck{Name: label, OK: true, Stdout: "0", Output: "0"}
				default:
					return diagnoseCommandCheck{Name: label, OK: false, Error: "unexpected command: " + label}
				}
			}
			configPath, statePath := writeDoctorVRRPGatedProxyARPAddressMobilityFixture(t)
			store := openDoctorState(t, statePath)
			if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "onprem-vip", map[string]any{"role": "master"}); err != nil {
				t.Fatalf("save VirtualAddress status: %v", err)
			}
			closeDoctorState(t, store)

			var out bytes.Buffer
			err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{})
			if tc.wantErr && err == nil {
				t.Fatalf("doctor hybrid succeeded with duplicate ARP responders:\n%s", out.String())
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("doctor hybrid: %v\n%s", err, out.String())
			}
			var report doctorReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
			}
			if check := findDoctorCheck(t, report, "RemoteAddressClaim/cloud-vm activeWhen vrrp-master"); check.Status != doctorPass {
				t.Fatalf("activeWhen check = %#v", check)
			}
			check := findDoctorCheck(t, report, "RemoteAddressClaim/cloud-vm proxy-arp duplicate responders")
			if check.Status != tc.wantStatus {
				t.Fatalf("duplicate responder check = %#v", check)
			}
			if tc.wantStatus == doctorFail && (!strings.Contains(check.Remedy, "split-brain") || (!strings.Contains(check.Detail, "aa:bb:cc:dd:ee:02") && !strings.Contains(check.Detail, "aa:bb:cc:dd:ee:01"))) {
				t.Fatalf("duplicate responder detail/remedy = %#v", check)
			}
		})
	}
}

func TestDoctorHybridSAMNonLinuxSkip(t *testing.T) {
	oldOS := doctorCurrentOS
	defer func() { doctorCurrentOS = oldOS }()
	doctorCurrentOS = func() platform.OS { return platform.OSFreeBSD }
	configPath, statePath := writeDoctorAddressMobilityFixture(t)
	var out bytes.Buffer
	if err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor hybrid: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	if check := findDoctorCheck(t, report, "RemoteAddressClaim/azure-vm SAM dataplane"); check.Status != doctorSkip || !strings.Contains(check.Detail, "not implemented") {
		t.Fatalf("dataplane check = %#v", check)
	}
}

func TestDoctorSAMForwardPolicyUnavailableDetailClassifiesFailures(t *testing.T) {
	tests := []struct {
		name    string
		command diagnoseCommandCheck
		want    string
	}{
		{
			name:    "nft unavailable",
			command: diagnoseCommandCheck{Error: `exec: "nft": executable file not found in $PATH`, ExitCode: -1},
			want:    "nft unavailable",
		},
		{
			name:    "permission denied",
			command: diagnoseCommandCheck{Stderr: "Error: Could not process rule: Operation not permitted", ExitCode: 1, Output: "Error: Could not process rule: Operation not permitted"},
			want:    "permission denied running nft",
		},
		{
			name:    "table absent",
			command: diagnoseCommandCheck{Stderr: "Error: No such file or directory", ExitCode: 1, Output: "Error: No such file or directory"},
			want:    "routerd_filter table absent; no routerd firewall policy observed",
		},
		{
			name:    "other failure",
			command: diagnoseCommandCheck{Error: "exit status 2", Stderr: "syntax error", ExitCode: 2, Output: "syntax error"},
			want:    "nft list table inet routerd_filter failed: exit status 2",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := doctorSAMForwardPolicyUnavailableDetail(tc.command); got != tc.want {
				t.Fatalf("detail = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDoctorHybridFailsUnresolvedPeerRef(t *testing.T) {
	configPath, statePath := writeDoctorHybridFixture(t, true)
	var out bytes.Buffer
	err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor hybrid succeeded with unresolved peer:\n%s", out.String())
	}
	var report doctorReport
	if unmarshalErr := json.Unmarshal(out.Bytes(), &report); unmarshalErr != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", unmarshalErr, out.String())
	}
	check := findDoctorCheck(t, report, "HybridRoute/cloud-private peerRef")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "missing OverlayPeer") {
		t.Fatalf("peerRef check = %#v", check)
	}
}

func TestDoctorFirewallWarnsAboutStaleRouterdNftTables(t *testing.T) {
	configPath, statePath := writeDoctorFirewallFixture(t)
	installDoctorFirewallNftTablesCommand(t, map[string]bool{
		"inet/routerd_filter":       true,
		"inet/routerd_stale_filter": true,
	})

	var out bytes.Buffer
	if err := run([]string{"doctor", "firewall", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor firewall: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "stale routerd nft tables")
	if check.Status != doctorWarn || !strings.Contains(check.Detail, "inet/routerd_stale_filter") {
		t.Fatalf("stale nft check = %#v", check)
	}
	if !strings.Contains(check.Detail, "marked routerd-owned") {
		t.Fatalf("stale nft check should be marker-based: %#v", check)
	}
}

func TestDoctorFirewallPassesWhenRouterdNftTablesMatchExpected(t *testing.T) {
	configPath, statePath := writeDoctorFirewallFixture(t)
	installDoctorFirewallNftTablesCommand(t, map[string]bool{
		"inet/routerd_filter": true,
	})

	var out bytes.Buffer
	if err := run([]string{"doctor", "firewall", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor firewall: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "stale routerd nft tables")
	if check.Status != doctorPass {
		t.Fatalf("stale nft check = %#v", check)
	}
}

func TestDoctorFirewallIgnoresUnmarkedRouterdPrefixedNftTables(t *testing.T) {
	configPath, statePath := writeDoctorFirewallFixture(t)
	installDoctorFirewallNftTablesCommand(t, map[string]bool{
		"inet/routerd_filter":       true,
		"inet/routerd_stale_filter": false,
	})

	var out bytes.Buffer
	if err := run([]string{"doctor", "firewall", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor firewall: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "stale routerd nft tables")
	if check.Status != doctorPass || !strings.Contains(check.Detail, "unmarked routerd-prefixed tables ignored") {
		t.Fatalf("stale nft check = %#v", check)
	}
}

func writeDoctorAddressMobilityFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: eth0
      spec:
        ifname: eth0
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata:
        name: wg-hybrid
      spec:
        listenPort: 51820
        mtu: 1420
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: OverlayPeer
      metadata:
        name: cloud-main
      spec:
        role: cloud
        nodeID: cloud-main
        underlay:
          type: wireguard
          interface: wg-hybrid
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: AddressMobilityDomain
      metadata:
        name: same-subnet
      spec:
        prefix: 10.0.0.0/24
        mode: selective-address
        peerRef: cloud-main
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: CloudProviderProfile
      metadata:
        name: azure-lab
      spec:
        provider: azure
        capabilities: [nic-secondary-ip, ip-forwarding]
        auth:
          mode: external-command
          command: /usr/local/libexec/routerd/plugins/azure-auth
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: RemoteAddressClaim
      metadata:
        name: azure-vm
      spec:
        domainRef: same-subnet
        address: 10.0.0.9/32
        ownerSide: cloud
        capture:
          type: provider-secondary-ip
          providerRef: azure-lab
          providerMode: nic-secondary-ip
          nicRef: azure-nic
          interface: eth0
        delivery:
          peerRef: cloud-main
          mode: route
          tunnelInterface: wg-hybrid
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorFirewallFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: eth0
        managed: false
        owner: external
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallZone
      metadata:
        name: lan
      spec:
        role: trust
        interfaces: [lan]
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallPolicy
      metadata:
        name: default
      spec:
        logDeny: true
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	statePath := filepath.Join(dir, "routerd.db")
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "FirewallZone", "lan", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save firewall zone status: %v", err)
	}
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "FirewallPolicy", "default", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save firewall policy status: %v", err)
	}
	closeDoctorState(t, store)
	return configPath, statePath
}

func installDoctorFirewallNftTablesCommand(t *testing.T, tables map[string]bool) {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for key := range tables {
		parts := strings.Split(key, "/")
		lines = append(lines, "table "+parts[0]+" "+parts[1])
	}
	sort.Strings(lines)
	listTables := strings.Join(lines, "\n")
	var cases strings.Builder
	for key, marked := range tables {
		parts := strings.Split(key, "/")
		marker := ""
		if marked {
			marker = "  comment \"" + render.NftablesRouterdOwnerMarker + "\"\n"
		}
		cases.WriteString(`if [ "$1" = "list" ] && [ "$2" = "table" ] && [ "$3" = "` + parts[0] + `" ] && [ "$4" = "` + parts[1] + `" ]; then
cat <<'EOF'
table ` + parts[0] + ` ` + parts[1] + ` {
` + marker + ` chain input { type filter hook input priority 0; policy drop; }
}
EOF
exit 0
fi
`)
	}
	writeTestCommand(t, filepath.Join(binDir, "nft"), `#!/bin/sh
`+cases.String()+`
if [ "$1" = "list" ] && [ "$2" = "tables" ]; then
cat <<'EOF'
`+listTables+`
EOF
exit 0
fi
echo "unexpected nft $*" >&2
exit 1
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeDoctorProxyARPAddressMobilityFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata:
        name: wg-hybrid
      spec:
        listenPort: 51820
        mtu: 1420
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: OverlayPeer
      metadata:
        name: cloud-main
      spec:
        role: cloud
        nodeID: cloud-main
        underlay:
          type: wireguard
          interface: wg-hybrid
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: AddressMobilityDomain
      metadata:
        name: same-subnet
      spec:
        prefix: 10.0.0.0/24
        mode: selective-address
        peerRef: cloud-main
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: RemoteAddressClaim
      metadata:
        name: cloud-vm
      spec:
        domainRef: same-subnet
        address: 10.0.0.7/32
        ownerSide: cloud
        capture:
          type: proxy-arp
          interface: br-lan
        delivery:
          peerRef: cloud-main
          mode: route
          tunnelInterface: wg-hybrid
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorVRRPGatedProxyARPAddressMobilityFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: VirtualAddress
      metadata:
        name: onprem-vip
      spec:
        family: ipv4
        interface: br-lan
        address: 10.0.0.1/32
        mode: vrrp
        vrrp:
          virtualRouterID: 60
          peers: ["10.0.0.2"]
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata:
        name: wg-hybrid
      spec:
        listenPort: 51820
        mtu: 1420
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: OverlayPeer
      metadata:
        name: cloud-main
      spec:
        role: cloud
        nodeID: cloud-main
        underlay:
          type: wireguard
          interface: wg-hybrid
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: AddressMobilityDomain
      metadata:
        name: same-subnet
      spec:
        prefix: 10.0.0.0/24
        mode: selective-address
        peerRef: cloud-main
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: RemoteAddressClaim
      metadata:
        name: cloud-vm
      spec:
        domainRef: same-subnet
        address: 10.0.0.7/32
        ownerSide: cloud
        capture:
          type: proxy-arp
          interface: br-lan
          activeWhen:
            type: vrrp-master
            virtualAddressRef: onprem-vip
        delivery:
          peerRef: cloud-main
          mode: route
          tunnelInterface: wg-hybrid
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: eth0
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSResolver
      metadata:
        name: lan-resolver
      spec:
        listen:
          - addresses: ["127.0.0.1"]
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6PrefixDelegation
      metadata:
        name: wan-pd
      spec:
        interface: wan
        prefixLength: 60
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorSAMFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata:
        name: cloudedge
      spec:
        prefix: 10.77.60.0/24
        groupRef: cloudedge
        deliveryPolicy:
          mode: bgp
        members:
          - nodeRef: azure-router
            site: azure
            role: cloud
            capture:
              type: provider-secondary-ip
              interface: eth0
          - nodeRef: oci-router
            site: oci
            role: cloud
            capture:
              type: provider-secondary-ip
              interface: ens3
          - nodeRef: onprem-router
            site: onprem
            role: onprem
            capture:
              type: proxy-arp
              interface: lan0
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorSAMBGPDeliveryFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventGroup
      metadata:
        name: cloudedge
      spec:
        nodeName: node-a
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata:
        name: mobility-bgp
      spec:
        asn: 64577
        routerID: 10.99.0.1
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMTransportProfile
      metadata:
        name: cloudedge-transport
      spec:
        selfNodeRef: node-a
        mode: ipip
        innerPrefix: 10.255.0.0/20
        underlayInterface: wg-hybrid
        localEndpoint: 10.99.0.1
        bgp:
          routerRef: BGPRouter/mobility-bgp
          peerASN: 64577
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata:
        name: cloudedge
      spec:
        prefix: 10.77.60.0/24
        groupRef: cloudedge
        deliveryPolicy:
          mode: bgp
        members:
          - nodeRef: node-a
            site: aws
            role: cloud
          - nodeRef: node-b
            site: azure
            role: cloud
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorSAMFederationFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := fmt.Sprintf(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventGroup
      metadata: { name: cloudedge }
      spec:
        nodeName: azure-router
        retention:
          maxEvents: 1000
          maxAge: 24h
        listen:
          address: 10.99.0.3
          port: 9443
        replayWindow: 5m
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata:
        name: cloudedge
      spec:
        prefix: 10.77.60.0/24
        groupRef: cloudedge
        deliveryPolicy: { mode: bgp }
        members:
          - nodeRef: azure-router
          - nodeRef: aws-router-a
          - nodeRef: oci-router
`)
	if err := os.WriteFile(configPath, []byte(data), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func recordFederationEvent(t *testing.T, store *routerstate.SQLiteStore, rec routerstate.EventRecord) {
	t.Helper()
	if err := store.RecordFederationEvent(rec); err != nil {
		t.Fatalf("record federation event: %v", err)
	}
}

func writeDoctorHybridFixture(t *testing.T, missingPeer bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	peerRef := "cloud-main"
	if missingPeer {
		peerRef = "missing"
	}
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata:
        name: wg-hybrid
      spec:
        listenPort: 51820
        mtu: 1420
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: OverlayPeer
      metadata:
        name: cloud-main
      spec:
        role: cloud
        nodeID: cloud-main
        underlay:
          type: wireguard
          interface: wg-hybrid
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: HybridRoute
      metadata:
        name: cloud-private
      spec:
        destinationCIDRs: [10.20.0.0/16]
        peerRef: ` + peerRef + `
        install:
          table: main
          metric: 120
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorDynamicFixture(t *testing.T, includePolicy bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	policy := ""
	if includePolicy {
		policy = `
    - apiVersion: config.routerd.net/v1alpha1
      kind: DynamicOverridePolicy
      metadata:
        name: cloud-masks
      spec:
        allow:
          - source: Plugin/cloud
            operations: [mask]
            targets:
              - apiVersion: net.routerd.net/v1alpha1
                kind: IPv4Route
                name: static-cloud-route
`
	}
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4Route
      metadata:
        name: static-cloud-route
      spec:
        destination: 10.10.0.0/24
        gateway: 192.0.2.1
` + policy)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorDSLiteFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: DSLiteTunnel
      metadata:
        name: ds-lite
      spec:
        interface: wan
        tunnelName: ds-routerd
        aftrFQDN: aftr.example.net
    - apiVersion: net.routerd.net/v1alpha1
      kind: EgressRoutePolicy
      metadata:
        name: ipv4-default
      spec:
        family: ipv4
        selection: highest-weight-ready
        candidates:
          - name: ds-lite
            source: DSLiteTunnel/ds-lite
            device: ds-routerd
            gatewaySource: none
            weight: 80
            healthCheck: internet
          - name: pppoe
            interface: ppp0
            gatewaySource: none
            weight: 20
            healthCheck: internet
    - apiVersion: net.routerd.net/v1alpha1
      kind: HealthCheck
      metadata:
        name: internet
      spec:
        target: 1.1.1.1
        protocol: icmp
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func saveDoctorDSLiteState(t *testing.T, statePath string, tunnelStatus map[string]any, selectedCandidate string, policyStatus map[string]any, healthStatus map[string]any) {
	t.Helper()
	store := openDoctorState(t, statePath)
	tunnelStatus["device"] = "ds-routerd"
	tunnelStatus["aftrFQDN"] = "aftr.example.net"
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "ds-lite", tunnelStatus); err != nil {
		t.Fatalf("save dslite status: %v", err)
	}
	if selectedCandidate != "" {
		policyStatus["selectedCandidate"] = selectedCandidate
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default", policyStatus); err != nil {
		t.Fatalf("save egress status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "internet", healthStatus); err != nil {
		t.Fatalf("save health status: %v", err)
	}
	closeDoctorState(t, store)
}

func installDoctorDSLiteHostCommands(t *testing.T, digOK, ipOK bool) {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	digScript := "#!/bin/sh\necho 'dig failed' >&2\nexit 1\n"
	if digOK {
		digScript = "#!/bin/sh\necho '2001:db8::1'\n"
	}
	ipScript := "#!/bin/sh\necho 'ip link failed' >&2\nexit 1\n"
	if ipOK {
		ipScript = "#!/bin/sh\necho '7: ds-routerd: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1460'\n"
	}
	writeTestCommand(t, filepath.Join(binDir, "dig"), digScript)
	writeTestCommand(t, filepath.Join(binDir, "ip"), ipScript)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func runDoctorDSLiteJSON(t *testing.T, configPath, statePath string) doctorReport {
	t.Helper()
	var out bytes.Buffer
	if err := run([]string{"doctor", "dslite", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor dslite: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	return report
}

func findDoctorCheck(t *testing.T, report doctorReport, name string) doctorCheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing check %q in %#v", name, report.Checks)
	return doctorCheck{}
}

func assertDoctorCheckAbsent(t *testing.T, report doctorReport, name string) {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			t.Fatalf("unexpected check %q in %#v", name, report.Checks)
		}
	}
}

func openDoctorState(t *testing.T, path string) *routerstate.SQLiteStore {
	t.Helper()
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	return store
}

func closeDoctorState(t *testing.T, store *routerstate.SQLiteStore) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite state: %v", err)
	}
}

func TestRunDiagnosticCommandSeparatesStreamsAndExitCode(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho 'this is stdout'\necho 'this is stderr' >&2\nexit 7\n"
	writeTestCommand(t, filepath.Join(binDir, "fakecmd"), script)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	check := runDiagnosticCommand(ctx, "fakecmd run", filepath.Join(binDir, "fakecmd"), "alpha", "beta")
	if check.OK {
		t.Fatalf("expected non-OK, got %#v", check)
	}
	if check.ExitCode != 7 {
		t.Fatalf("expected exit 7, got %d", check.ExitCode)
	}
	if check.Stdout != "this is stdout" {
		t.Fatalf("stdout = %q", check.Stdout)
	}
	if check.Stderr != "this is stderr" {
		t.Fatalf("stderr = %q", check.Stderr)
	}
	if !strings.HasSuffix(check.Command, "fakecmd alpha beta") {
		t.Fatalf("command = %q", check.Command)
	}
	if !strings.Contains(check.Output, "this is stdout") || !strings.Contains(check.Output, "this is stderr") {
		t.Fatalf("output = %q (must retain both streams)", check.Output)
	}
}

func TestDoctorNftCheckStatusWarnsWhenStdoutHasTableButExitNonZero(t *testing.T) {
	command := diagnoseCommandCheck{
		Name:     "nft list table ip routerd_nat",
		OK:       false,
		Command:  "nft list table ip routerd_nat",
		Stdout:   "table ip routerd_nat {\n\tchain postrouting {\n\t}\n}",
		Stderr:   "warning: ignoring unknown attribute",
		ExitCode: 1,
		Output:   "table ip routerd_nat {...}\n--- stderr ---\nwarning: ignoring unknown attribute",
	}
	check := doctorNftCheckStatus("nat", command, "ip", "routerd_nat", doctorFail, "remedy here", "NAT44Rule active=1 pending=0")
	if check.Status != doctorWarn {
		t.Fatalf("expected warn, got %#v", check)
	}
	if !strings.Contains(check.Detail, "table=ip/routerd_nat") {
		t.Fatalf("detail missing table label: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "exit=1") {
		t.Fatalf("detail missing exit code: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "stderr=warning: ignoring unknown attribute") {
		t.Fatalf("detail missing stderr excerpt: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "NAT44Rule active=1 pending=0") {
		t.Fatalf("detail missing resource count: %q", check.Detail)
	}
}

func TestDoctorNftCheckStatusFailsWhenStdoutEmpty(t *testing.T) {
	command := diagnoseCommandCheck{
		Name:     "nft list table ip routerd_nat",
		OK:       false,
		Command:  "nft list table ip routerd_nat",
		Stdout:   "",
		Stderr:   "Error: No such file or directory; did you mean table 'routerd' in family ip?",
		ExitCode: 1,
		Output:   "Error: No such file or directory",
	}
	check := doctorNftCheckStatus("nat", command, "ip", "routerd_nat", doctorFail, "apply NAT44Rule resources", "NAT44Rule active=0 pending=2")
	if check.Status != doctorFail {
		t.Fatalf("expected fail, got %#v", check)
	}
	if !strings.Contains(check.Detail, "table=ip/routerd_nat") {
		t.Fatalf("detail missing table label: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "cmd=nft list table ip routerd_nat") {
		t.Fatalf("detail missing cmd: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "exit=1") {
		t.Fatalf("detail missing exit code: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "stderr=Error: No such file or directory") {
		t.Fatalf("detail missing stderr excerpt: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "NAT44Rule active=0 pending=2") {
		t.Fatalf("detail missing resource counts: %q", check.Detail)
	}
	if check.Remedy != "apply NAT44Rule resources" {
		t.Fatalf("remedy = %q", check.Remedy)
	}
}

func TestDoctorNftCheckStatusPassesWithStdout(t *testing.T) {
	command := diagnoseCommandCheck{
		Name:     "nft list table ip routerd_nat",
		OK:       true,
		Command:  "nft list table ip routerd_nat",
		Stdout:   "table ip routerd_nat {\n}",
		ExitCode: 0,
		Output:   "table ip routerd_nat {\n}",
	}
	check := doctorNftCheckStatus("nat", command, "ip", "routerd_nat", doctorFail, "remedy", "NAT44Rule active=2 pending=0")
	if check.Status != doctorPass {
		t.Fatalf("expected pass, got %#v", check)
	}
	if !strings.Contains(check.Detail, "table=ip/routerd_nat") {
		t.Fatalf("detail missing table label: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "NAT44Rule active=2 pending=0") {
		t.Fatalf("detail missing resource counts: %q", check.Detail)
	}
}

func TestDoctorNATEmitsExitAndStderrOnNftFailure(t *testing.T) {
	configPath, statePath := writeDoctorNATFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "NAT44Rule", "wan-masq", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save nat status: %v", err)
	}
	closeDoctorState(t, store)

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestCommand(t, filepath.Join(binDir, "nft"), "#!/bin/sh\necho 'Error: No such file or directory' >&2\nexit 1\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	err := run([]string{"doctor", "nat", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor nat expected to fail when nft listing is empty:\n%s", out.String())
	}
	var report doctorReport
	if unmarshalErr := json.Unmarshal(out.Bytes(), &report); unmarshalErr != nil {
		t.Fatalf("unmarshal: %v\n%s", unmarshalErr, out.String())
	}
	check := findDoctorCheck(t, report, "nft list table ip routerd_nat")
	if check.Status != doctorFail {
		t.Fatalf("nft check = %#v", check)
	}
	for _, want := range []string{"table=ip/routerd_nat", "exit=1", "stderr=Error: No such file or directory", "NAT44Rule active=1 pending=0"} {
		if !strings.Contains(check.Detail, want) {
			t.Fatalf("nft check detail missing %q: %q", want, check.Detail)
		}
	}
}

func TestDoctorNATWarnsWhenNftListingPresentDespiteExit(t *testing.T) {
	configPath, statePath := writeDoctorNATFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "NAT44Rule", "wan-masq", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save nat status: %v", err)
	}
	closeDoctorState(t, store)

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestCommand(t, filepath.Join(binDir, "nft"), "#!/bin/sh\ncat <<'EOF'\ntable ip routerd_nat {\n\tchain postrouting {\n\t}\n}\nEOF\necho 'warning: noisy attribute' >&2\nexit 1\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	if err := run([]string{"doctor", "nat", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor nat unexpectedly failed for warn case: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "nft list table ip routerd_nat")
	if check.Status != doctorWarn {
		t.Fatalf("expected warn (table present in stdout, exit !=0); got %#v", check)
	}
	for _, want := range []string{"table=ip/routerd_nat", "exit=1", "stderr=warning: noisy attribute"} {
		if !strings.Contains(check.Detail, want) {
			t.Fatalf("nft check detail missing %q: %q", want, check.Detail)
		}
	}
}

func TestDoctorNATPassesWhenNftSucceeds(t *testing.T) {
	configPath, statePath := writeDoctorNATFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "NAT44Rule", "wan-masq", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save nat status: %v", err)
	}
	closeDoctorState(t, store)

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestCommand(t, filepath.Join(binDir, "nft"), "#!/bin/sh\ncat <<'EOF'\ntable ip routerd_nat {\n}\nEOF\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	if err := run([]string{"doctor", "nat", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor nat: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "nft list table ip routerd_nat")
	if check.Status != doctorPass {
		t.Fatalf("expected pass; got %#v", check)
	}
	if !strings.Contains(check.Detail, "table=ip/routerd_nat") {
		t.Fatalf("detail missing table label: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "NAT44Rule active=1 pending=0") {
		t.Fatalf("detail missing resource counts: %q", check.Detail)
	}
}

func writeDoctorNATFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: NAT44Rule
      metadata:
        name: wan-masq
      spec:
        action: masquerade
        outboundInterface: wan
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}
