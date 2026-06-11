// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeAz is a FAKE az command runner: it records every argv and returns canned
// show/list JSON. It NEVER calls real Azure. Tests assert against recorded calls.
type fakeAz struct {
	calls   [][]string
	showOut []byte
	listOut []byte
	err     error
}

func (f *fakeAz) run(ctx context.Context, argv ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), argv...))
	if f.err != nil {
		return nil, f.err
	}
	toks := leadingTokens(argv)
	verb := ""
	if len(toks) > 0 {
		verb = toks[len(toks)-1]
	}
	switch verb {
	case "show":
		if f.showOut != nil {
			return f.showOut, nil
		}
		return cannedNICShow(false), nil
	case "list":
		if f.listOut != nil {
			return f.listOut, nil
		}
		return cannedNICList("rg1", "nic1", "ipcfg-mobility", "10.88.60.9"), nil
	}
	// Mutating verbs return a benign (ignored) JSON body.
	return []byte(`{}`), nil
}

type routeFakeAz struct {
	calls     [][]string
	showOut   []byte
	showErr   error
	createErr error
	updateErr error
	deleteErr error
}

func (f *routeFakeAz) run(ctx context.Context, argv ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), argv...))
	toks := strings.Join(leadingTokens(argv), " ")
	switch toks {
	case "network route-table route show":
		if f.showErr != nil {
			return nil, f.showErr
		}
		if f.showOut != nil {
			return f.showOut, nil
		}
		return cannedRouteShow("10.88.60.9/32", "10.88.60.254"), nil
	case "network route-table route create":
		if f.createErr != nil {
			return nil, f.createErr
		}
	case "network route-table route update":
		if f.updateErr != nil {
			return nil, f.updateErr
		}
	case "network route-table route delete":
		if f.deleteErr != nil {
			return nil, f.deleteErr
		}
	}
	return []byte(`{}`), nil
}

func cannedRouteShow(address, nextHop string) []byte {
	return []byte(fmt.Sprintf(`{"addressPrefix":%q,"nextHopIpAddress":%q}`, address, nextHop))
}

type seizeFakeAz struct {
	calls                    [][]string
	selfHolds                bool
	oldHolds                 bool
	createErr                error
	createErrOnce            bool
	conflictRevealsOld       bool
	alreadyExistsRevealsSelf bool
	deleteErr                error
	verifyShowErr            error
	showCount                int
	createCount              int
	deleteCount              int
	postCreateShowCount      int
	postDeleteListCount      int
	selfVerifyMisses         int
	oldReleaseMisses         int
	listResource             string
	oldResource              string
	oldNIC                   string
	oldIPConfig              string
	selfIPConfig             string
	address                  string
	nestedAltIPField         bool
}

func newSeizeFakeAz() *seizeFakeAz {
	return &seizeFakeAz{
		oldHolds:     true,
		listResource: "rg1",
		oldResource:  "rg1",
		oldNIC:       "nic-old",
		oldIPConfig:  "ipcfg-mobility",
		selfIPConfig: "ipcfg-mobility",
		address:      "10.88.60.9",
	}
}

func (f *seizeFakeAz) run(ctx context.Context, argv ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), argv...))
	toks := strings.Join(leadingTokens(argv), " ")
	switch toks {
	case "network nic show":
		f.showCount++
		if f.verifyShowErr != nil && f.showCount > 1 {
			return nil, f.verifyShowErr
		}
		if f.selfHolds {
			if f.createCount > 0 && f.postCreateShowCount < f.selfVerifyMisses {
				f.postCreateShowCount++
				return cannedNICShow(false), nil
			}
			return cannedNICShowWithConfig(false, f.selfIPConfig, f.address), nil
		}
		return cannedNICShow(false), nil
	case "network nic list":
		if f.oldHolds {
			return cannedNICList(f.oldResource, f.oldNIC, f.oldIPConfig, f.address), nil
		}
		return []byte(`[]`), nil
	case "network nic ip-config list":
		if f.oldHolds {
			if f.nestedAltIPField {
				return cannedIPConfigListNestedAlt(f.oldIPConfig, f.address), nil
			}
			return cannedIPConfigList(f.oldIPConfig, f.address), nil
		}
		if f.deleteCount > 0 && f.postDeleteListCount < f.oldReleaseMisses {
			f.postDeleteListCount++
			return cannedIPConfigList(f.oldIPConfig, f.address), nil
		}
		return []byte(`[]`), nil
	case "network nic ip-config delete":
		if f.deleteErr != nil {
			if isNotFoundError(f.deleteErr) {
				f.oldHolds = false
			}
			return nil, f.deleteErr
		}
		f.oldHolds = false
		f.deleteCount++
		return []byte(`{}`), nil
	case "network nic ip-config create":
		if f.createErr != nil {
			err := f.createErr
			if f.conflictRevealsOld && isAddressConflictError(err) {
				f.oldHolds = true
			}
			if f.alreadyExistsRevealsSelf && isAlreadyExistsError(err) {
				f.selfHolds = true
			}
			if f.createErrOnce {
				f.createErr = nil
			}
			return nil, err
		}
		f.selfHolds = true
		f.createCount++
		return []byte(`{}`), nil
	default:
		return []byte(`{}`), nil
	}
}

func cannedNICShow(ipForwarding bool) []byte {
	return []byte(fmt.Sprintf(`{"id":"/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1","name":"nic1","resourceGroup":"rg1","enableIPForwarding":%t,"ipConfigurations":[]}`, ipForwarding))
}

func cannedNICShowWithConfig(ipForwarding bool, name, address string) []byte {
	return []byte(fmt.Sprintf(`{"id":"/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1","name":"nic1","resourceGroup":"rg1","enableIPForwarding":%t,"ipConfigurations":[{"name":%q,"privateIPAddress":%q}]}`, ipForwarding, name, address))
}

func cannedNICList(resourceGroup, nicName, ipConfigName, address string) []byte {
	return []byte(fmt.Sprintf(`[{"id":"/subscriptions/s1/resourceGroups/%s/providers/Microsoft.Network/networkInterfaces/%s","name":%q,"resourceGroup":%q,"ipConfigurations":[{"name":%q,"privateIPAddress":%q}]}]`, resourceGroup, nicName, nicName, resourceGroup, ipConfigName, address))
}

func cannedIPConfigList(ipConfigName, address string) []byte {
	return []byte(fmt.Sprintf(`[{"name":%q,"privateIPAddress":%q}]`, ipConfigName, address))
}

func cannedIPConfigListNestedAlt(ipConfigName, address string) []byte {
	return []byte(fmt.Sprintf(`[{"name":%q,"properties":{"privateIpAddress":%q}}]`, ipConfigName, address))
}

func reqSpec(action, mode string) executeActionRequestSpec {
	return executeActionRequestSpec{
		Action:         action,
		Provider:       "azure",
		Mode:           mode,
		IdempotencyKey: "k1",
		Target: map[string]string{
			"nicRef":         "/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
			"resourceGroup":  "rg1",
			"nicName":        "nic1",
			"ipConfigName":   "ipcfg-mobility",
			"address":        "10.88.60.9/32",
			"region":         "japaneast",
			"subscriptionId": "s1",
		},
	}
}

func routeReqSpec(action, mode string) executeActionRequestSpec {
	spec := reqSpec(action, mode)
	spec.Target["routeTableRef"] = "rt-cloudedge"
	spec.Target["routeTableName"] = "rt-cloudedge"
	spec.Target["routeName"] = "cloudedge-10-88-60-9-32"
	spec.Target["nextHopIPAddress"] = "10.88.60.254"
	spec.Target["captureStrategy"] = captureStrategyRouteTable
	return spec
}

func dispatchWith(spec executeActionRequestSpec, runner azRunner) executeActionResult {
	return dispatch(context.Background(), executeActionRequest{Spec: spec}, runner)
}

func TestPreflightMissingAzureHelperFails(t *testing.T) {
	t.Setenv(azureHelperEnv, filepath.Join(t.TempDir(), "missing-helper"))
	res := dispatchWith(reqSpec(actionPreflight, modeDryRun), func(ctx context.Context, argv ...string) ([]byte, error) {
		t.Fatalf("preflight must not invoke azure helper runner")
		return nil, nil
	})
	if res.Status.Status != statusFailed {
		t.Fatalf("want failed, got %#v", res.Status)
	}
	if !strings.Contains(res.Status.Error, azureHelperEnv) || !strings.Contains(res.Status.Message, "preflight") {
		t.Fatalf("preflight error not actionable: %#v", res.Status)
	}
}

func TestPreflightAzureHelperOverridePasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "azure-routerd-helper")
	if err := os.WriteFile(path, []byte(`#!/bin/sh
if [ "$1" = "version" ]; then echo '{"version":"azure-routerd-helper/test"}'; exit 0; fi
if [ "$1" = "preflight" ]; then echo '{"managedIdentity":"ok","subscriptionProbe":"not-requested"}'; exit 0; fi
exit 2
`), 0755); err != nil {
		t.Fatalf("write fake helper: %v", err)
	}
	t.Setenv(azureHelperEnv, path)
	res := dispatchWith(reqSpec(actionPreflight, modeDryRun), func(ctx context.Context, argv ...string) ([]byte, error) {
		t.Fatalf("preflight must not invoke azure helper runner")
		return nil, nil
	})
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %#v", res.Status)
	}
	if res.Status.Observed["dependency"] != "azure-routerd-helper" || res.Status.Observed["path"] != path || res.Status.Observed["version"] != "azure-routerd-helper/test" {
		t.Fatalf("observed = %#v", res.Status.Observed)
	}
}

func TestPreflightLegacyAzCLIPathFallbackPasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "az")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake az: %v", err)
	}
	t.Setenv(azureHelperEnv, "")
	t.Setenv(azCLIPathEnv, path)
	res := dispatchWith(reqSpec(actionPreflight, modeDryRun), func(ctx context.Context, argv ...string) ([]byte, error) {
		t.Fatalf("preflight must not invoke azure helper runner")
		return nil, nil
	})
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %#v", res.Status)
	}
	if res.Status.Observed["legacyAzCLI"] != "true" || res.Status.Observed["path"] != path {
		t.Fatalf("observed = %#v", res.Status.Observed)
	}
}

func TestPreflightLegacyAzCLIPathDoesNotUsePATHLookup(t *testing.T) {
	binDir := t.TempDir()
	path := filepath.Join(binDir, "az")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake az: %v", err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv(azureHelperEnv, "")
	t.Setenv(azCLIPathEnv, "az")
	res := dispatchWith(reqSpec(actionPreflight, modeDryRun), func(ctx context.Context, argv ...string) ([]byte, error) {
		t.Fatalf("preflight must not invoke azure helper runner")
		return nil, nil
	})
	if res.Status.Status != statusFailed {
		t.Fatalf("want failed, got %#v", res.Status)
	}
	if !strings.Contains(res.Status.Error, "PATH lookup is not used") {
		t.Fatalf("preflight error = %#v", res.Status)
	}
}

func verbsOf(calls [][]string) []string {
	var out []string
	for _, c := range calls {
		toks := leadingTokens(c)
		if len(toks) > 0 {
			out = append(out, toks[len(toks)-1])
		}
	}
	return out
}

func joinedCalls(calls [][]string) []string {
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		out = append(out, strings.Join(call, " "))
	}
	return out
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func countCall(calls []string, want string) int {
	count := 0
	for _, call := range calls {
		if call == want {
			count++
		}
	}
	return count
}

func withNoRetrySleep(t *testing.T) {
	t.Helper()
	orig := sleep
	sleep = func(time.Duration) {}
	t.Cleanup(func() { sleep = orig })
}

func TestAssignDryRunReadsOnly(t *testing.T) {
	f := &fakeAz{}
	res := dispatchWith(reqSpec(actionAssignSecondaryIP, modeDryRun), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if !strings.HasPrefix(res.Status.Message, "would assign ") {
		t.Errorf("dry-run message: %q", res.Status.Message)
	}
	if !res.Status.UndoAvailable {
		t.Errorf("assign must report UndoAvailable")
	}
	for _, c := range f.calls {
		if !isReadOnlyVerb(c) {
			t.Fatalf("dry-run issued a non-read-only command (must NOT mutate); call=%v", c)
		}
	}
}

func TestAssignExecuteIssuesIPConfigCreate(t *testing.T) {
	f := &fakeAz{}
	res := dispatchWith(reqSpec(actionAssignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Observed["assignedAddress"] != "10.88.60.9" {
		t.Errorf("want assignedAddress observed, got %+v", res.Status.Observed)
	}
	if res.Status.Observed["ipConfigName"] != "ipcfg-mobility" {
		t.Errorf("want ipConfigName observed, got %+v", res.Status.Observed)
	}
	if len(f.calls) != 1 {
		t.Fatalf("execute assign should issue exactly one call, got %v", f.calls)
	}
	got := strings.Join(f.calls[0], " ")
	want := "network nic ip-config create --resource-group rg1 --nic-name nic1 --name ipcfg-mobility --private-ip-address 10.88.60.9 --subscription s1"
	if got != want {
		t.Fatalf("assign argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestAssignRouteTableExecuteCreatesRoute(t *testing.T) {
	f := &routeFakeAz{}
	res := dispatchWith(routeReqSpec(actionAssignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	got := joinedCalls(f.calls)
	want := "network route-table route create --resource-group rg1 --route-table-name rt-cloudedge --name cloudedge-10-88-60-9-32 --address-prefix 10.88.60.9/32 --next-hop-type VirtualAppliance --next-hop-ip-address 10.88.60.254 --subscription s1"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("calls = %v, want create route", got)
	}
}

func TestAssignSecondaryIPRouteTableStrategyCreatesRoute(t *testing.T) {
	f := &routeFakeAz{}
	res := dispatchWith(routeReqSpec(actionAssignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	want := "network route-table route create --resource-group rg1 --route-table-name rt-cloudedge --name cloudedge-10-88-60-9-32 --address-prefix 10.88.60.9/32 --next-hop-type VirtualAppliance --next-hop-ip-address 10.88.60.254 --subscription s1"
	if got := strings.Join(f.calls[0], " "); got != want {
		t.Fatalf("create route argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestAssignRouteTableSeizeUpdatesExistingRoute(t *testing.T) {
	f := &routeFakeAz{}
	spec := routeReqSpec(actionAssignRouteTableRoute, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	got := joinedCalls(f.calls)
	want := "network route-table route update --resource-group rg1 --route-table-name rt-cloudedge --name cloudedge-10-88-60-9-32 --set addressPrefix=10.88.60.9/32 nextHopType=VirtualAppliance nextHopIpAddress=10.88.60.254 --subscription s1"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("calls = %v, want update route for seize", got)
	}
}

func TestAssignRouteTableCreateExistingSameNextHopIsIdempotent(t *testing.T) {
	f := &routeFakeAz{createErr: fmt.Errorf("route already exists")}
	res := dispatchWith(routeReqSpec(actionAssignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	got := joinedCalls(f.calls)
	if len(got) != 2 || !strings.Contains(got[0], " route create ") || !strings.Contains(got[1], " route show ") {
		t.Fatalf("calls = %v, want create then show", got)
	}
}

func TestAssignRouteTableCreateExistingForeignNextHopFails(t *testing.T) {
	f := &routeFakeAz{
		createErr: fmt.Errorf("route already exists"),
		showOut:   cannedRouteShow("10.88.60.9/32", "10.88.60.253"),
	}
	res := dispatchWith(routeReqSpec(actionAssignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("want failed, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	got := joinedCalls(f.calls)
	if len(got) != 2 || !strings.Contains(got[0], " route create ") || !strings.Contains(got[1], " route show ") {
		t.Fatalf("calls = %v, want create then show only", got)
	}
}

func TestUnassignRouteTableMissingRouteSkips(t *testing.T) {
	f := &routeFakeAz{showErr: fmt.Errorf("route not found")}
	res := dispatchWith(routeReqSpec(actionUnassignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSkipped {
		t.Fatalf("want skipped, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	got := joinedCalls(f.calls)
	if len(got) != 1 || !strings.Contains(got[0], " route show ") {
		t.Fatalf("calls = %v, want show only", got)
	}
}

func TestUnassignRouteTableSkipsForeignNextHop(t *testing.T) {
	f := &routeFakeAz{showOut: cannedRouteShow("10.88.60.9/32", "10.88.60.253")}
	res := dispatchWith(routeReqSpec(actionUnassignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSkipped {
		t.Fatalf("want skipped, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	got := joinedCalls(f.calls)
	if len(got) != 1 || !strings.Contains(got[0], " route show ") {
		t.Fatalf("calls = %v, want show only", got)
	}
}

func TestUnassignRouteTableDeletesOnlyMatchingNextHop(t *testing.T) {
	f := &routeFakeAz{}
	res := dispatchWith(routeReqSpec(actionUnassignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	got := joinedCalls(f.calls)
	if len(got) != 2 || !strings.Contains(got[0], " route show ") || !strings.Contains(got[1], " route delete ") {
		t.Fatalf("calls = %v, want show then delete", got)
	}
}

func TestAssignDryRunAllowReassignmentReadsOnly(t *testing.T) {
	f := &fakeAz{}
	spec := reqSpec(actionAssignSecondaryIP, modeDryRun)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if !strings.Contains(res.Status.Message, "would seize/reassign") {
		t.Fatalf("dry-run message = %q, want seize/reassign", res.Status.Message)
	}
	for _, c := range f.calls {
		if !isReadOnlyVerb(c) {
			t.Fatalf("dry-run issued a non-read-only command (must NOT mutate); call=%v", c)
		}
	}
}

func TestAssignExecuteAllowReassignmentDeletesOldThenCreatesSelf(t *testing.T) {
	f := newSeizeFakeAz()
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	spec.Target["displacedResourceGroup"] = "rg1"
	spec.Target["displacedNicName"] = "nic-old"
	spec.Target["displacedIpConfigName"] = "ipcfg-mobility"
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q message=%q", res.Status.Status, res.Status.Error, res.Status.Message)
	}
	got := joinedCalls(f.calls)
	want := []string{
		"network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
		"network nic ip-config list --resource-group rg1 --nic-name nic-old --subscription s1",
		"network nic ip-config delete --resource-group rg1 --nic-name nic-old --name ipcfg-mobility --subscription s1",
		"network nic ip-config create --resource-group rg1 --nic-name nic1 --name ipcfg-mobility --private-ip-address 10.88.60.9 --subscription s1",
		"network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
		"network nic ip-config list --resource-group rg1 --nic-name nic-old --subscription s1",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("seize calls mismatch:\n got: %v\nwant: %v", got, want)
	}
	if res.Status.Observed["assignedAddress"] != "10.88.60.9" || res.Status.Observed["ipConfigName"] != "ipcfg-mobility" {
		t.Fatalf("observed = %+v", res.Status.Observed)
	}
	if !strings.Contains(res.Status.Message, "seized/reassigned") {
		t.Fatalf("message = %q, want seize/reassign", res.Status.Message)
	}
}

func TestAssignExecuteAllowReassignmentFallsBackToInventoryDiscovery(t *testing.T) {
	f := newSeizeFakeAz()
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q message=%q", res.Status.Status, res.Status.Error, res.Status.Message)
	}
	got := joinedCalls(f.calls)
	if len(got) < 3 || got[1] != "network nic list --resource-group rg1 --subscription s1" {
		t.Fatalf("calls = %v, want inventory discovery via nic list", got)
	}
	if !containsCall(got, "network nic ip-config delete --resource-group rg1 --nic-name nic-old --name ipcfg-mobility --subscription s1") {
		t.Fatalf("calls = %v, want delete of discovered old holder", got)
	}
}

func TestAssignExecuteAllowReassignmentParsesNestedPrivateIPAddress(t *testing.T) {
	f := newSeizeFakeAz()
	f.nestedAltIPField = true
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	spec.Target["displacedResourceGroup"] = "rg1"
	spec.Target["displacedNicName"] = "nic-old"
	spec.Target["displacedIpConfigName"] = "ipcfg-mobility"

	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q message=%q", res.Status.Status, res.Status.Error, res.Status.Message)
	}
	if !containsCall(joinedCalls(f.calls), "network nic ip-config delete --resource-group rg1 --nic-name nic-old --name ipcfg-mobility --subscription s1") {
		t.Fatalf("calls = %v, want old holder discovered from nested privateIpAddress", joinedCalls(f.calls))
	}
}

func TestAssignExecuteAllowReassignmentIdempotentWhenSelfAlreadyHolds(t *testing.T) {
	f := newSeizeFakeAz()
	f.oldHolds = false
	f.selfHolds = true
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if len(f.calls) != 1 || strings.Join(f.calls[0], " ") != "network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1" {
		t.Fatalf("calls = %v, want only self show", f.calls)
	}
	if res.Status.Observed["seizeAlreadyPresent"] != "true" {
		t.Fatalf("observed = %+v, want already-present convergence", res.Status.Observed)
	}
}

func TestAssignExecuteAllowReassignmentRetriesAfterRemoveSucceededAddFailed(t *testing.T) {
	first := newSeizeFakeAz()
	first.createErr = fmt.Errorf("injected create failure")
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, first.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("first attempt should fail at create, got %q", res.Status.Status)
	}
	if first.oldHolds {
		t.Fatal("old holder should have been removed before create failure")
	}

	retry := newSeizeFakeAz()
	retry.oldHolds = false
	res = dispatchWith(spec, retry.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("retry should add self after old removal, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if !containsCall(joinedCalls(retry.calls), "network nic ip-config create --resource-group rg1 --nic-name nic1 --name ipcfg-mobility --private-ip-address 10.88.60.9 --subscription s1") {
		t.Fatalf("retry calls = %v, want create self", joinedCalls(retry.calls))
	}
}

func TestAssignExecuteAllowReassignmentVerifyFailureRetriesToSelfPresent(t *testing.T) {
	withNoRetrySleep(t)
	first := newSeizeFakeAz()
	first.verifyShowErr = fmt.Errorf("injected verify failure")
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, first.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("first attempt should fail at verify, got %q", res.Status.Status)
	}
	if !first.selfHolds {
		t.Fatal("self should hold IP after create even if verify failed")
	}

	retry := newSeizeFakeAz()
	retry.oldHolds = false
	retry.selfHolds = true
	res = dispatchWith(spec, retry.run)
	if res.Status.Status != statusSucceeded || res.Status.Observed["seizeAlreadyPresent"] != "true" {
		t.Fatalf("retry should converge from self-present state, status=%q observed=%+v err=%q", res.Status.Status, res.Status.Observed, res.Status.Error)
	}
}

func TestAssignExecuteAllowReassignmentWaitsForEventualSelfAndOldVisibility(t *testing.T) {
	withNoRetrySleep(t)
	f := newSeizeFakeAz()
	f.selfVerifyMisses = 2
	f.oldReleaseMisses = 1
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	spec.Target["displacedResourceGroup"] = "rg1"
	spec.Target["displacedNicName"] = "nic-old"
	spec.Target["displacedIpConfigName"] = "ipcfg-mobility"
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("eventual visibility should converge, got status=%q message=%q err=%q", res.Status.Status, res.Status.Message, res.Status.Error)
	}
	got := joinedCalls(f.calls)
	showSelf := "network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1"
	if countCall(got, showSelf) < 4 {
		t.Fatalf("calls = %v, want initial show plus repeated self verify", got)
	}
	oldList := "network nic ip-config list --resource-group rg1 --nic-name nic-old --subscription s1"
	if countCall(got, oldList) < 3 {
		t.Fatalf("calls = %v, want holder discovery plus repeated old release verify", got)
	}
}

func TestAssignExecuteAllowReassignmentDeleteNotFoundIsIdempotent(t *testing.T) {
	f := newSeizeFakeAz()
	f.deleteErr = fmt.Errorf("ResourceNotFound: ip configuration could not be found")
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	spec.Target["displacedResourceGroup"] = "rg1"
	spec.Target["displacedNicName"] = "nic-old"
	spec.Target["displacedIpConfigName"] = "ipcfg-mobility"
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("delete not-found should be idempotent, got %q err=%q", res.Status.Status, res.Status.Error)
	}
}

func TestAssignExecuteAllowReassignmentDeleteFailureIsRetriedLater(t *testing.T) {
	f := newSeizeFakeAz()
	f.deleteErr = fmt.Errorf("AuthorizationFailed: missing write permission on old NIC")
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	spec.Target["displacedResourceGroup"] = "rg1"
	spec.Target["displacedNicName"] = "nic-old"
	spec.Target["displacedIpConfigName"] = "ipcfg-mobility"
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("delete permission failure should fail hard, got %q", res.Status.Status)
	}
	if !f.oldHolds {
		t.Fatal("old holder should remain when delete fails before removal")
	}
	if containsCall(joinedCalls(f.calls), "network nic ip-config create --resource-group rg1 --nic-name nic1 --name ipcfg-mobility --private-ip-address 10.88.60.9 --subscription s1") {
		t.Fatalf("calls = %v, must not create self after old delete failed", joinedCalls(f.calls))
	}
}

func TestAssignExecuteAllowReassignmentCreateAlreadyExistsVerifiesSelf(t *testing.T) {
	f := newSeizeFakeAz()
	f.oldHolds = false
	f.createErr = fmt.Errorf("AlreadyExists: IP configuration already exists")
	f.alreadyExistsRevealsSelf = true
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("already-exists self verify should converge, got %q err=%q message=%q", res.Status.Status, res.Status.Error, res.Status.Message)
	}
	if res.Status.Observed["seizeAlreadyPresent"] != "true" {
		t.Fatalf("observed = %+v, want already-present convergence", res.Status.Observed)
	}
}

func TestAssignExecuteAllowReassignmentConflictRediscovery(t *testing.T) {
	f := newSeizeFakeAz()
	f.oldHolds = false
	f.createErr = fmt.Errorf("PrivateIPAddressIsInUse: private IP address is in use")
	f.createErrOnce = true
	f.conflictRevealsOld = true
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("conflict rediscovery should delete holder and retry create, got status=%q message=%q err=%q", res.Status.Status, res.Status.Message, res.Status.Error)
	}
	got := joinedCalls(f.calls)
	if !containsCall(got, "network nic ip-config delete --resource-group rg1 --nic-name nic-old --name ipcfg-mobility --subscription s1") {
		t.Fatalf("calls = %v, want delete after rediscovery", got)
	}
	createCount := 0
	for _, call := range got {
		if call == "network nic ip-config create --resource-group rg1 --nic-name nic1 --name ipcfg-mobility --private-ip-address 10.88.60.9 --subscription s1" {
			createCount++
		}
	}
	if createCount != 2 {
		t.Fatalf("calls = %v, want create attempted twice around conflict", got)
	}
	if !containsCall(got, "network nic ip-config list --resource-group rg1 --nic-name nic-old --subscription s1") {
		t.Fatalf("calls = %v, want displaced verify after conflict retry", got)
	}
}

func TestEnsureForwardingEnabledDryRunCapturesPriorNoMutation(t *testing.T) {
	f := &fakeAz{showOut: cannedNICShow(false)}
	res := dispatchWith(reqSpec(actionEnsureFwdEnabled, modeDryRun), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q", res.Status.Status)
	}
	if res.Status.Message != "would set ipForwarding=true" {
		t.Errorf("message: %q", res.Status.Message)
	}
	if res.Status.Observed["priorIpForwarding"] != "false" {
		t.Errorf("want priorIpForwarding=false, got %+v", res.Status.Observed)
	}
	for _, c := range f.calls {
		if !isReadOnlyVerb(c) {
			t.Fatalf("dry-run mutated via %v", c)
		}
	}
}

func TestEnsureForwardingEnabledExecuteShowsThenUpdates(t *testing.T) {
	f := &fakeAz{showOut: cannedNICShow(false)}
	res := dispatchWith(reqSpec(actionEnsureFwdEnabled, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Observed["priorIpForwarding"] != "false" {
		t.Fatalf("want prior captured =false, got %+v", res.Status.Observed)
	}
	verbs := verbsOf(f.calls)
	if len(verbs) != 2 {
		t.Fatalf("expected show THEN update (2 calls), got %v", f.calls)
	}
	if verbs[0] != "show" {
		t.Fatalf("first call must be nic show (capture prior), got %q", verbs[0])
	}
	if verbs[1] != "update" {
		t.Fatalf("second call must be nic update, got %q", verbs[1])
	}
	got := strings.Join(f.calls[1], " ")
	want := "network nic update --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1 --ip-forwarding true"
	if got != want {
		t.Fatalf("update argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestEnsureForwardingDisabledRestoresWhenPriorFalse(t *testing.T) {
	f := &fakeAz{}
	spec := reqSpec(actionEnsureFwdDisabled, modeExecute)
	spec.Parameters = map[string]string{"priorIpForwarding": "false"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if len(f.calls) != 1 {
		t.Fatalf("expected one update call, got %v", f.calls)
	}
	got := strings.Join(f.calls[0], " ")
	want := "network nic update --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1 --ip-forwarding false"
	if got != want {
		t.Fatalf("restore argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestEnsureForwardingDisabledNoOpWhenPriorTrue(t *testing.T) {
	f := &fakeAz{}
	spec := reqSpec(actionEnsureFwdDisabled, modeExecute)
	spec.Parameters = map[string]string{"priorIpForwarding": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSkipped {
		t.Fatalf("prior=true must be a NO-OP skipped, got %q", res.Status.Status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("NO-OP must issue ZERO az calls, got %v", f.calls)
	}
	if !strings.Contains(res.Status.Message, "already true") {
		t.Errorf("message should explain the no-op, got %q", res.Status.Message)
	}
}

func TestEnsureForwardingDisabledMissingPriorErrors(t *testing.T) {
	f := &fakeAz{}
	spec := reqSpec(actionEnsureFwdDisabled, modeExecute) // no priorIpForwarding
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("missing priorIpForwarding must fail (never blind), got %q", res.Status.Status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("must not call az without a prior fact, got %v", f.calls)
	}
}

func TestUnassignExecuteDeletesIPConfig(t *testing.T) {
	f := &fakeAz{}
	res := dispatchWith(reqSpec(actionUnassignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if len(f.calls) != 1 {
		t.Fatalf("execute unassign should issue exactly one call, got %v", f.calls)
	}
	got := strings.Join(f.calls[0], " ")
	want := "network nic ip-config delete --resource-group rg1 --nic-name nic1 --name ipcfg-mobility --subscription s1"
	if got != want {
		t.Fatalf("unassign argv mismatch:\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(res.Status.Message, "/32") {
		t.Fatalf("unassign message should not leak CIDR-form address, got %q", res.Status.Message)
	}
}

func TestMissingTargetFieldsError(t *testing.T) {
	cases := []struct {
		name  string
		mutTk func(m map[string]string)
	}{
		{"no nicRef", func(m map[string]string) { delete(m, "nicRef") }},
		{"no resourceGroup", func(m map[string]string) { delete(m, "resourceGroup") }},
		{"no nicName", func(m map[string]string) { delete(m, "nicName") }},
		{"no ipConfigName", func(m map[string]string) { delete(m, "ipConfigName") }},
		{"no address", func(m map[string]string) { delete(m, "address") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAz{}
			spec := reqSpec(actionAssignSecondaryIP, modeExecute)
			tc.mutTk(spec.Target)
			res := dispatchWith(spec, f.run)
			if res.Status.Status != statusFailed {
				t.Fatalf("%s should fail clearly, got %q", tc.name, res.Status.Status)
			}
			if len(f.calls) != 0 {
				t.Fatalf("%s must not invoke az, got %v", tc.name, f.calls)
			}
		})
	}
}

func TestEnsureForwardingMissingNICRefError(t *testing.T) {
	// The forwarding actions only need nicRef (NIC resource id), not the
	// ip-config fields. Missing nicRef must still fail clearly.
	f := &fakeAz{}
	spec := reqSpec(actionEnsureFwdEnabled, modeExecute)
	delete(spec.Target, "nicRef")
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("missing nicRef must fail, got %q", res.Status.Status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("must not invoke az, got %v", f.calls)
	}
}

func TestInvalidModeFails(t *testing.T) {
	f := &fakeAz{}
	res := dispatchWith(reqSpec(actionAssignSecondaryIP, "apply"), f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("invalid mode must fail, got %q", res.Status.Status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("invalid mode must not invoke az, got %v", f.calls)
	}
}

func TestGuardedRunnerRejectsNonReadOnly(t *testing.T) {
	f := &fakeAz{}
	guarded := guardedRunner(f.run)
	if _, err := guarded(context.Background(), "network", "nic", "update", "--ids", "n"); err == nil {
		t.Fatal("guarded runner must reject a mutating verb")
	}
	if len(f.calls) != 0 {
		t.Fatalf("guarded runner must not invoke the inner runner for a mutating verb, got %v", f.calls)
	}
	if _, err := guarded(context.Background(), "network", "nic", "show", "--ids", "n"); err != nil {
		t.Fatalf("guarded runner must allow show: %v", err)
	}
	if _, err := guarded(context.Background(), "network", "nic", "list"); err != nil {
		t.Fatalf("guarded runner must allow list: %v", err)
	}
}

func TestAzCommandArgsForcesJSONAndQuietErrors(t *testing.T) {
	got := strings.Join(azCommandArgs("network", "nic", "ip-config", "delete",
		"--resource-group", "rg1",
		"--nic-name", "nic1",
		"--name", "ipcfg-mobility"), " ")
	want := "network nic ip-config delete --resource-group rg1 --nic-name nic1 --name ipcfg-mobility --only-show-errors --output json"
	if got != want {
		t.Fatalf("az argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestRunEndToEndStdInOut(t *testing.T) {
	f := &fakeAz{}
	req := executeActionRequest{Spec: reqSpec(actionAssignSecondaryIP, modeExecute)}
	in, _ := json.Marshal(req)
	var out bytes.Buffer
	if err := run(context.Background(), bytes.NewReader(in), &out, f.run); err != nil {
		t.Fatalf("run: %v", err)
	}
	var res executeActionResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q", res.Status.Status)
	}
}

// TestExecutorImportsNoCloudSDK asserts the azure-provider-executor shipped code
// imports NO cloud SDK. It LEGITIMATELY uses os/exec (it runs azure-routerd-helper), so
// os/exec is allowed here — but pulling in an Azure/AWS/OCI/GCP SDK is forbidden:
// the executor's only external dependency is exec of the helper binary. (The
// fleet-wide examples/plugins no-exec invariant in internal/addressclaim
// excludes THIS directory for exactly this reason.)
func TestExecutorImportsNoCloudSDK(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	dir := filepath.Dir(thisFile)
	forbidden := []string{
		"github.com/Azure/",
		"github.com/aws/",
		"github.com/oracle/oci-go-sdk",
		"cloud.google.com/go",
	}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	usesExec := false
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatal(perr)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "os/exec" {
				usesExec = true
			}
			for _, bad := range forbidden {
				if p == bad || strings.HasPrefix(p, bad) {
					t.Errorf("%s imports forbidden cloud SDK %q (executor may exec azure-routerd-helper, not link an SDK)", name, p)
				}
			}
		}
	}
	if !usesExec {
		t.Error("expected the azure executor to use os/exec to run azure-routerd-helper")
	}
}
