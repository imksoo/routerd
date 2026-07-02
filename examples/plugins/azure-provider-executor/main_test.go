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
		if strings.Join(toks, " ") == "network nic ip-config list" {
			return cannedIPConfigList("ipcfg-mobility", "10.88.60.9"), nil
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

type authFakeAz struct {
	calls             [][]string
	accountShowErr    error
	commandLoginErr   error
	loginErr          error
	loginErrs         []error
	commandLoginTries int
}

func (f *authFakeAz) run(ctx context.Context, argv ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), argv...))
	toks := strings.Join(leadingTokens(argv), " ")
	switch toks {
	case "account show":
		if f.accountShowErr != nil {
			return nil, f.accountShowErr
		}
		return []byte(`{"id":"s1"}`), nil
	case "login":
		if len(f.loginErrs) > 0 {
			err := f.loginErrs[0]
			f.loginErrs = f.loginErrs[1:]
			if err != nil {
				return nil, err
			}
		}
		if f.loginErr != nil {
			return nil, f.loginErr
		}
		return []byte(`[{"id":"s1"}]`), nil
	case "network nic show":
		return cannedNICShow(false), nil
	case "network nic ip-config create":
		if f.commandLoginErr != nil && f.commandLoginTries == 0 {
			f.commandLoginTries++
			return nil, f.commandLoginErr
		}
		return []byte(`{}`), nil
	default:
		return []byte(`{}`), nil
	}
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
		if f.verifyShowErr != nil && f.createCount > 0 {
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
	case "rest":
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
	return []byte(fmt.Sprintf(`{"id":"/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1","name":"nic1","resourceGroup":"rg1","location":"japaneast","enableIPForwarding":%t,"ipConfigurations":[{"name":"primary","properties":{"primary":true,"privateIPAddress":"10.88.60.4","privateIPAllocationMethod":"Static","subnet":{"id":"/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/default"}}}]}`, ipForwarding))
}

func cannedNICShowWithConfig(ipForwarding bool, name, address string) []byte {
	return []byte(fmt.Sprintf(`{"id":"/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1","name":"nic1","resourceGroup":"rg1","location":"japaneast","enableIPForwarding":%t,"ipConfigurations":[{"name":"primary","properties":{"primary":true,"privateIPAddress":"10.88.60.4","privateIPAllocationMethod":"Static","subnet":{"id":"/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/default"}}},{"name":%q,"properties":{"primary":false,"privateIPAddress":%q,"privateIPAllocationMethod":"Static","subnet":{"id":"/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/default"}}}]}`, ipForwarding, name, address))
}

func cannedNICShowFlattened(ipForwarding bool) []byte {
	return []byte(fmt.Sprintf(`{"id":"/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1","name":"nic1","resourceGroup":"rg1","location":"japaneast","enableIPForwarding":%t,"ipConfigurations":[{"id":"/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1/ipConfigurations/primary","name":"primary","primary":true,"privateIPAddress":"10.88.60.4","privateIPAddressVersion":"IPv4","privateIPAllocationMethod":"Static","provisioningState":"Succeeded","resourceGroup":"rg1","subnet":{"id":"/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/default","resourceGroup":"rg1"},"type":"Microsoft.Network/networkInterfaces/ipConfigurations"}]}`, ipForwarding))
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

func argAfter(argv []string, flag string) string {
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == flag {
			return argv[i+1]
		}
	}
	return ""
}

func bodyContainsIPConfig(t *testing.T, body, name, address string) bool {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	configs, ok := putBodyIPConfigurations(parsed)
	if !ok {
		t.Fatalf("body missing ipConfigurations: %s", body)
	}
	for _, raw := range configs {
		cfg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if cfg["name"] != name {
			continue
		}
		props, _ := cfg["properties"].(map[string]any)
		if props["privateIPAddress"] == address {
			return true
		}
	}
	return false
}

func putBodyIPConfigurations(parsed map[string]any) ([]any, bool) {
	if configs, ok := parsed["ipConfigurations"].([]any); ok {
		return configs, true
	}
	props, _ := parsed["properties"].(map[string]any)
	configs, ok := props["ipConfigurations"].([]any)
	return configs, ok
}

func countLeadingCalls(calls []string, leading string) int {
	count := 0
	for _, call := range calls {
		if strings.HasPrefix(call, leading) {
			count++
		}
	}
	return count
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

func TestAzureLoginEnsuringRunnerLogsInWithManagedIdentityWhenAccountMissing(t *testing.T) {
	f := &authFakeAz{accountShowErr: fmt.Errorf("ERROR: Please run 'az login' to setup account.")}
	runner := azureLoginEnsuringRunner(f.run)

	if _, err := runner(context.Background(), "network", "nic", "show", "--ids", "nic1"); err != nil {
		t.Fatalf("runner returned error: %v", err)
	}

	got := joinedCalls(f.calls)
	want := []string{
		"account show",
		"login --identity --allow-no-subscriptions",
		"network nic show --ids nic1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestAzureLoginEnsuringRunnerRefreshesWhenCommandNeedsLogin(t *testing.T) {
	f := &authFakeAz{commandLoginErr: fmt.Errorf("ERROR: Please run 'az login' to setup account.")}
	runner := azureLoginEnsuringRunner(f.run)

	if _, err := runner(context.Background(), "network", "nic", "ip-config", "create", "--resource-group", "rg1", "--nic-name", "nic1"); err != nil {
		t.Fatalf("runner returned error: %v", err)
	}

	got := joinedCalls(f.calls)
	want := []string{
		"account show",
		"network nic ip-config create --resource-group rg1 --nic-name nic1",
		"login --identity --allow-no-subscriptions",
		"network nic ip-config create --resource-group rg1 --nic-name nic1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestAzureLoginEnsuringRunnerDoesNotPinInitialLoginFailure(t *testing.T) {
	f := &authFakeAz{
		accountShowErr: fmt.Errorf("ERROR: Please run 'az login' to setup account."),
		loginErrs:      []error{fmt.Errorf("managed identity endpoint is not ready"), nil},
	}
	runner := azureLoginEnsuringRunner(f.run)

	if _, err := runner(context.Background(), "network", "nic", "show", "--ids", "nic1"); err == nil {
		t.Fatal("first runner call unexpectedly succeeded")
	}
	if _, err := runner(context.Background(), "network", "nic", "show", "--ids", "nic1"); err != nil {
		t.Fatalf("second runner call returned error: %v", err)
	}

	got := joinedCalls(f.calls)
	want := []string{
		"account show",
		"login --identity --allow-no-subscriptions",
		"account show",
		"login --identity --allow-no-subscriptions",
		"network nic show --ids nic1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
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

func stripQueryFromCall(call string) string {
	if i := strings.Index(call, " --query "); i != -1 {
		return call[:i]
	}
	return call
}

func joinedCallsWithoutQuery(calls [][]string) []string {
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		out = append(out, stripQueryFromCall(strings.Join(call, " ")))
	}
	return out
}

func containsCallWithoutQuery(calls []string, want string) bool {
	for _, call := range calls {
		if stripQueryFromCall(call) == want {
			return true
		}
	}
	return false
}

func countCallWithoutQuery(calls []string, want string) int {
	count := 0
	for _, call := range calls {
		if stripQueryFromCall(call) == want {
			count++
		}
	}
	return count
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
	if len(f.calls) != 2 {
		t.Fatalf("execute assign should show then PUT once, got %v", f.calls)
	}
	if got := strings.Join(f.calls[0], " "); got != "network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1" {
		t.Fatalf("assign show argv mismatch: %s", got)
	}
	if got := strings.Join(leadingTokens(f.calls[1]), " "); got != "rest" {
		t.Fatalf("assign should use az rest for single NIC PUT, got %v", f.calls[1])
	}
	body := argAfter(f.calls[1], "--body")
	if body == "" {
		t.Fatalf("assign PUT missing body: %v", f.calls[1])
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("assign PUT body is not JSON: %v", err)
	}
	if parsed["enableIPForwarding"] != true {
		props, _ := parsed["properties"].(map[string]any)
		if props["enableIPForwarding"] != true {
			t.Fatalf("assign PUT body must enable forwarding: %s", body)
		}
	}
	if !bodyContainsIPConfig(t, body, "ipcfg-mobility", "10.88.60.9") {
		t.Fatalf("assign PUT body missing target ip-config: %s", body)
	}
}

func TestAssignExecuteNormalizesFlattenedNICShowForSinglePUT(t *testing.T) {
	f := &fakeAz{showOut: cannedNICShowFlattened(false)}
	res := dispatchWith(reqSpec(actionAssignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	body := argAfter(f.calls[1], "--body")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("assign PUT body is not JSON: %v", err)
	}
	props, _ := parsed["properties"].(map[string]any)
	if props["enableIPForwarding"] != true {
		t.Fatalf("assign PUT body must put enableIPForwarding under properties: %s", body)
	}
	configs, ok := props["ipConfigurations"].([]any)
	if !ok || len(configs) != 2 {
		t.Fatalf("assign PUT body must put normalized ipConfigurations under properties: %s", body)
	}
	primary, _ := configs[0].(map[string]any)
	if _, leaked := primary["subnet"]; leaked {
		t.Fatalf("flattened subnet must move under properties in PUT body: %s", body)
	}
	primaryProps, _ := primary["properties"].(map[string]any)
	if primaryProps["privateIPAddress"] != "10.88.60.4" {
		t.Fatalf("primary private IP not preserved: %s", body)
	}
	if _, ok := primaryProps["subnet"].(map[string]any); !ok {
		t.Fatalf("primary subnet not preserved under properties: %s", body)
	}
	if !bodyContainsIPConfig(t, body, "ipcfg-mobility", "10.88.60.9") {
		t.Fatalf("assign PUT body missing target ip-config: %s", body)
	}
}

func TestAssignExecuteFailsAzureAddressConflictWithoutSeizeParameter(t *testing.T) {
	f := newSeizeFakeAz()
	f.oldHolds = false
	f.createErr = fmt.Errorf("PrivateIPAddressIsAllocated: private IP address 10.88.60.9 is already allocated to nic-old/ipcfg-mobility")
	f.createErrOnce = true
	f.conflictRevealsOld = true

	res := dispatchWith(reqSpec(actionAssignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("normal assign should not seize stale Azure holder, got status=%q message=%q err=%q", res.Status.Status, res.Status.Message, res.Status.Error)
	}
	if res.Status.Observed["failureClass"] != "addressHeldByAnotherTarget" {
		t.Fatalf("observed = %+v, want addressHeldByAnotherTarget failure", res.Status.Observed)
	}
	if res.Status.Observed["observedHolderNIC"] != "nic-old" || res.Status.Observed["observedHolderIPConfig"] != "ipcfg-mobility" {
		t.Fatalf("observed = %+v, want stale holder identity", res.Status.Observed)
	}
	got := joinedCalls(f.calls)
	if !containsCallWithoutQuery(got, "network nic list --resource-group rg1") {
		t.Fatalf("calls = %v, want holder rediscovery by NIC list", got)
	}
	if containsCallWithoutQuery(got, "network nic ip-config delete --resource-group rg1 --nic-name nic-old --name ipcfg-mobility") {
		t.Fatalf("calls = %v, normal assign must not delete stale holder", got)
	}
	createCount := 0
	for _, call := range got {
		if strings.HasPrefix(call, "rest --method put ") {
			createCount++
		}
	}
	if createCount != 1 {
		t.Fatalf("calls = %v, want create attempted once without seize", got)
	}
}

func TestAuthorizationFailureIsClassified(t *testing.T) {
	f := &fakeAz{err: fmt.Errorf("AuthorizationFailed: The client does not have authorization to perform action")}
	res := dispatchWith(reqSpec(actionAssignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("want failed, got %q", res.Status.Status)
	}
	if res.Status.Observed["failureClass"] != "authorization" {
		t.Fatalf("want authorization failure class, got %+v", res.Status.Observed)
	}
	if res.Status.Observed["permissionHint"] != "Microsoft.Network/networkInterfaces/write" {
		t.Fatalf("permissionHint = %q", res.Status.Observed["permissionHint"])
	}
}

func TestToolchainPermissionFailureIsNotAuthorization(t *testing.T) {
	f := &fakeAz{err: fmt.Errorf("fork/exec /usr/local/bin/az: permission denied")}
	res := dispatchWith(reqSpec(actionAssignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("want failed, got %q", res.Status.Status)
	}
	if res.Status.Observed["failureClass"] == "authorization" {
		t.Fatalf("toolchain permission failure must not look like cloud authorization, got %+v", res.Status.Observed)
	}
}

func TestAssignRouteTableExecuteCreatesRoute(t *testing.T) {
	f := &routeFakeAz{}
	res := dispatchWith(routeReqSpec(actionAssignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	got := joinedCalls(f.calls)
	want := "network route-table route create --resource-group rg1 --route-table-name rt-cloudedge --name cloudedge-10-88-60-9-32 --address-prefix 10.88.60.9/32 --next-hop-type VirtualAppliance --next-hop-ip-address 10.88.60.254"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("calls = %v, want create route", got)
	}
}

func TestAssignRouteTableCreateRetriesTransientFailure(t *testing.T) {
	withNoRetrySleep(t)
	callCount := 0
	runner := func(ctx context.Context, argv ...string) ([]byte, error) {
		switch strings.Join(leadingTokens(argv), " ") {
		case "network route-table route create":
			callCount++
			if callCount < 3 {
				return nil, fmt.Errorf("azure CLI failed: signal: killed")
			}
			return []byte(`{}`), nil
		default:
			return nil, fmt.Errorf("unexpected call: %v", argv)
		}
	}
	res := dispatchWith(routeReqSpec(actionAssignRouteTableRoute, modeExecute), runner)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded after retry, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if callCount != 3 {
		t.Fatalf("route create should retry twice before success, got %d calls", callCount)
	}
}

func TestAssignSecondaryIPRouteTableStrategyCreatesRoute(t *testing.T) {
	f := &routeFakeAz{}
	res := dispatchWith(routeReqSpec(actionAssignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	want := "network route-table route create --resource-group rg1 --route-table-name rt-cloudedge --name cloudedge-10-88-60-9-32 --address-prefix 10.88.60.9/32 --next-hop-type VirtualAppliance --next-hop-ip-address 10.88.60.254"
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
	want := "network route-table route update --resource-group rg1 --route-table-name rt-cloudedge --name cloudedge-10-88-60-9-32 --set addressPrefix=10.88.60.9/32 nextHopType=VirtualAppliance nextHopIpAddress=10.88.60.254"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("calls = %v, want update route for seize", got)
	}
}

func TestAssignRouteTableUpdateRetriesTransientFailure(t *testing.T) {
	withNoRetrySleep(t)
	spec := routeReqSpec(actionAssignRouteTableRoute, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	callCount := 0
	runner := func(ctx context.Context, argv ...string) ([]byte, error) {
		switch strings.Join(leadingTokens(argv), " ") {
		case "network route-table route update":
			callCount++
			if callCount < 3 {
				return nil, fmt.Errorf("azure CLI failed: temporary failure")
			}
			return []byte(`{}`), nil
		default:
			return nil, fmt.Errorf("unexpected call: %v", argv)
		}
	}
	res := dispatchWith(spec, runner)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded after retry, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if callCount != 3 {
		t.Fatalf("route update should retry twice before success, got %d calls", callCount)
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

func TestUnassignRouteTableDeleteRetriesTransientFailure(t *testing.T) {
	withNoRetrySleep(t)
	deleteCount := 0
	runner := func(ctx context.Context, argv ...string) ([]byte, error) {
		switch strings.Join(leadingTokens(argv), " ") {
		case "network route-table route show":
			return cannedRouteShow("10.88.60.9/32", "10.88.60.254"), nil
		case "network route-table route delete":
			deleteCount++
			if deleteCount < 3 {
				return nil, fmt.Errorf("azure CLI failed: 429 too many requests")
			}
			return []byte(`{}`), nil
		default:
			return nil, fmt.Errorf("unexpected call: %v", argv)
		}
	}
	res := dispatchWith(routeReqSpec(actionUnassignRouteTableRoute, modeExecute), runner)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded after retry, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if deleteCount != 3 {
		t.Fatalf("route delete should retry twice before success, got %d calls", deleteCount)
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
	gotNoQuery := joinedCallsWithoutQuery(f.calls)
	want := []string{
		"network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
		"network nic ip-config list --resource-group rg1 --nic-name nic-old",
		"network nic ip-config delete --resource-group rg1 --nic-name nic-old --name ipcfg-mobility",
		"network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
		"rest --method put --uri /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1?api-version=2023-09-01",
		"network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
		"network nic ip-config list --resource-group rg1 --nic-name nic-old",
	}
	if len(gotNoQuery) != len(want) {
		t.Fatalf("seize calls mismatch:\n got: %v\nwant: %v", got, want)
	}
	for i := range want {
		if !strings.HasPrefix(gotNoQuery[i], want[i]) {
			t.Fatalf("seize calls mismatch at %d:\n got: %v\nwant: %v", i, got, want)
		}
	}
	if countLeadingCalls(got, "rest --method put ") != 1 {
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
	if len(got) < 3 || got[1] != "network nic list --resource-group rg1" {
		t.Fatalf("calls = %v, want inventory discovery via nic list", got)
	}
	if !containsCallWithoutQuery(got, "network nic ip-config delete --resource-group rg1 --nic-name nic-old --name ipcfg-mobility") {
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
	if !containsCallWithoutQuery(joinedCalls(f.calls), "network nic ip-config delete --resource-group rg1 --nic-name nic-old --name ipcfg-mobility") {
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
	if countLeadingCalls(joinedCalls(retry.calls), "rest --method put ") != 1 {
		t.Fatalf("retry calls = %v, want single NIC PUT self", joinedCalls(retry.calls))
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
	oldList := "network nic ip-config list --resource-group rg1 --nic-name nic-old"
	if countCallWithoutQuery(got, oldList) < 3 {
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
	if countLeadingCalls(joinedCalls(f.calls), "rest --method put ") != 0 {
		t.Fatalf("calls = %v, must not PUT self after old delete failed", joinedCalls(f.calls))
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
	if !containsCallWithoutQuery(got, "network nic ip-config delete --resource-group rg1 --nic-name nic-old --name ipcfg-mobility") {
		t.Fatalf("calls = %v, want delete after rediscovery", got)
	}
	createCount := 0
	for _, call := range got {
		if strings.HasPrefix(call, "rest --method put ") {
			createCount++
		}
	}
	if createCount != 2 {
		t.Fatalf("calls = %v, want create attempted twice around conflict", got)
	}
	if !containsCallWithoutQuery(got, "network nic ip-config list --resource-group rg1 --nic-name nic-old") {
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
	if strings.Join(leadingTokens(f.calls[1]), " ") != "rest" {
		t.Fatalf("second call must be az rest NIC PUT, got %v", f.calls[1])
	}
	body := argAfter(f.calls[1], "--body")
	if body == "" {
		t.Fatalf("forwarding PUT missing body: %v", f.calls[1])
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("forwarding PUT body is not JSON: %v", err)
	}
	props, _ := parsed["properties"].(map[string]any)
	if props["enableIPForwarding"] != true {
		t.Fatalf("forwarding PUT body must enable forwarding: %s", body)
	}
}

func TestEnsureForwardingEnabledExecuteNoOpWhenAlreadyTrue(t *testing.T) {
	f := &fakeAz{showOut: cannedNICShow(true)}
	res := dispatchWith(reqSpec(actionEnsureFwdEnabled, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Observed["priorIpForwarding"] != "true" {
		t.Fatalf("want prior captured =true, got %+v", res.Status.Observed)
	}
	if len(f.calls) != 1 {
		t.Fatalf("already-forwarding case should only show, got %v", f.calls)
	}
	if got := strings.Join(f.calls[0], " "); got != "network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1" {
		t.Fatalf("show argv mismatch: %s", got)
	}
}

func TestBuildNICAssignForwardingBodyAddsIPAndForwarding(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal(cannedNICShow(false), &raw); err != nil {
		t.Fatal(err)
	}
	nic, err := showNIC(context.Background(), func(ctx context.Context, argv ...string) ([]byte, error) {
		return cannedNICShow(false), nil
	}, "nic1")
	if err != nil {
		t.Fatal(err)
	}
	nic.Raw = raw
	body, changed, err := buildNICAssignForwardingBody(nic, nicTarget{ipConfigName: "ipcfg-mobility", address: "10.88.60.9"})
	if err != nil {
		t.Fatalf("build body: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed body")
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body json: %v", err)
	}
	props, _ := parsed["properties"].(map[string]any)
	if props["enableIPForwarding"] != true {
		t.Fatalf("body must enable forwarding: %s", body)
	}
	if !bodyContainsIPConfig(t, string(body), "ipcfg-mobility", "10.88.60.9") {
		t.Fatalf("body missing secondary ip-config: %s", body)
	}
}

func TestAssignExecuteNoOpWhenTargetStateMatches(t *testing.T) {
	f := &fakeAz{showOut: cannedNICShowWithConfig(true, "ipcfg-mobility", "10.88.60.9")}
	res := dispatchWith(reqSpec(actionAssignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if len(f.calls) != 1 {
		t.Fatalf("target-state match should only show, got %v", f.calls)
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
	if len(f.calls) != 2 {
		t.Fatalf("execute unassign should delete and verify absence, got %v", f.calls)
	}
	gotNoQuery := joinedCallsWithoutQuery(f.calls)
	want := []string{
		"network nic ip-config delete --resource-group rg1 --nic-name nic1 --name ipcfg-mobility",
		"network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
	}
	if gotNoQuery[0] != want[0] {
		t.Fatalf("unassign argv mismatch:\n got: %s\nwant: %s", gotNoQuery[0], want[0])
	}
	if gotNoQuery[1] != want[1] {
		t.Fatalf("unassign verify argv mismatch:\n got: %s\nwant: %s", gotNoQuery[1], want[1])
	}
	if strings.Contains(res.Status.Message, "/32") {
		t.Fatalf("unassign message should not leak CIDR-form address, got %q", res.Status.Message)
	}
}

func TestUnassignExecuteSucceedsWhenIPConfigAlreadyAbsent(t *testing.T) {
	callCount := 0
	runner := func(ctx context.Context, argv ...string) ([]byte, error) {
		callCount++
		joined := strings.Join(leadingTokens(argv), " ")
		switch joined {
		case "network nic ip-config delete":
			return nil, fmt.Errorf("azure CLI failed: ResourceNotFound")
		case "network nic show":
			return cannedNICShow(false), nil
		default:
			return nil, fmt.Errorf("unexpected call: %v", argv)
		}
	}
	res := dispatchWith(reqSpec(actionUnassignSecondaryIP, modeExecute), runner)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if !strings.Contains(res.Status.Message, "already absent") {
		t.Fatalf("message = %q, want already absent", res.Status.Message)
	}
	if callCount != 2 {
		t.Fatalf("expected delete+show fallback calls, got %d", callCount)
	}
}

func TestUnassignExecuteDeletesWhenDeleteReturnsNotFound(t *testing.T) {
	callCount := 0
	runner := func(ctx context.Context, argv ...string) ([]byte, error) {
		callCount++
		joined := strings.Join(leadingTokens(argv), " ")
		switch joined {
		case "network nic ip-config delete":
			return nil, fmt.Errorf("azure CLI failed: ResourceNotFound")
		case "network nic show":
			if callCount > 2 {
				return cannedNICShow(false), nil
			}
			return cannedNICShowWithConfig(false, "ipcfg-renamed", "10.88.60.9"), nil
		default:
			return nil, fmt.Errorf("unexpected call: %v", argv)
		}
	}
	res := dispatchWith(reqSpec(actionUnassignSecondaryIP, modeExecute), runner)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if callCount != 4 {
		t.Fatalf("expected delete+show+delete+verify calls, got %d", callCount)
	}
	if !strings.Contains(res.Status.Message, "unassigned") {
		t.Fatalf("message = %q, want unassigned", res.Status.Message)
	}
}

func TestUnassignExecuteFallsBackToDeleteWhenShowContainsAddress(t *testing.T) {
	withNoRetrySleep(t)
	callCount := 0
	var calls []string
	runner := func(ctx context.Context, argv ...string) ([]byte, error) {
		callCount++
		calls = append(calls, strings.Join(argv, " "))
		joined := strings.Join(leadingTokens(argv), " ")
		switch joined {
		case "network nic ip-config delete":
			if callCount == 1 {
				return nil, fmt.Errorf("azure CLI failed: ResourceNotFound")
			}
			return []byte(`{}`), nil
		case "network nic show":
			if callCount > 3 {
				return cannedNICShow(false), nil
			}
			return cannedNICShowWithConfig(false, "ipcfg-renamed", "10.88.60.9"), nil
		default:
			return nil, fmt.Errorf("unexpected call: %v", argv)
		}
	}
	res := dispatchWith(reqSpec(actionUnassignSecondaryIP, modeExecute), runner)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if callCount != 4 {
		t.Fatalf("expected delete + show + delete by discovered ip-config name + verify, got=%d", callCount)
	}
	want := []string{
		"network nic ip-config delete --resource-group rg1 --nic-name nic1 --name ipcfg-mobility",
		"network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
		"network nic ip-config delete --resource-group rg1 --nic-name nic1 --name ipcfg-renamed",
		"network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want length %d", calls, len(want))
	}
	for i := range want {
		if stripQueryFromCall(calls[i]) != want[i] {
			t.Fatalf("unassign calls mismatch at %d:\n got=%q\nwant=%q", i, calls[i], want[i])
		}
	}
}

func TestUnassignExecuteRequiresTargetAddress(t *testing.T) {
	f := &fakeAz{}
	spec := reqSpec(actionUnassignSecondaryIP, modeExecute)
	delete(spec.Target, "address")
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("missing target.address should fail, got %q", res.Status.Status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("missing target.address should not invoke az, got %v", f.calls)
	}
}

func TestUnassignExecuteFailsWhenAddressStillPresentAfterDelete(t *testing.T) {
	withNoRetrySleep(t)
	runner := func(ctx context.Context, argv ...string) ([]byte, error) {
		switch strings.Join(leadingTokens(argv), " ") {
		case "network nic ip-config delete":
			return []byte(`{}`), nil
		case "network nic show":
			return cannedNICShowWithConfig(false, "ipcfg-mobility", "10.88.60.9"), nil
		default:
			return nil, fmt.Errorf("unexpected call: %v", argv)
		}
	}
	res := dispatchWith(reqSpec(actionUnassignSecondaryIP, modeExecute), runner)
	if res.Status.Status != statusFailed {
		t.Fatalf("want failed while address remains present, got %q", res.Status.Status)
	}
	if !strings.Contains(res.Status.Error, "still holds address") {
		t.Fatalf("error = %q, want still holds address", res.Status.Error)
	}
}

func TestUnassignExecuteRetriesDeleteWithRetryableError(t *testing.T) {
	withNoRetrySleep(t)
	callCount := 0
	var calls []string
	runner := func(ctx context.Context, argv ...string) ([]byte, error) {
		callCount++
		calls = append(calls, strings.Join(argv, " "))
		joined := strings.Join(leadingTokens(argv), " ")
		switch joined {
		case "network nic ip-config delete":
			if callCount < 3 {
				return nil, fmt.Errorf("azure CLI failed: signal: killed")
			}
			return []byte(`{}`), nil
		case "network nic show":
			return cannedNICShow(false), nil
		default:
			return nil, fmt.Errorf("unexpected call: %v", argv)
		}
	}
	res := dispatchWith(reqSpec(actionUnassignSecondaryIP, modeExecute), runner)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if callCount != 4 {
		t.Fatalf("delete retryable should attempt 3 deletes and one verify, got=%d", callCount)
	}
	want := []string{
		"network nic ip-config delete --resource-group rg1 --nic-name nic1 --name ipcfg-mobility",
		"network nic ip-config delete --resource-group rg1 --nic-name nic1 --name ipcfg-mobility",
		"network nic ip-config delete --resource-group rg1 --nic-name nic1 --name ipcfg-mobility",
		"network nic show --ids /subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want length %d", calls, len(want))
	}
	for i := range want {
		if stripQueryFromCall(calls[i]) != want[i] {
			t.Fatalf("unassign calls mismatch at %d:\n got=%q\nwant=%q", i, calls[i], want[i])
		}
	}
}

func TestListIPConfigsUsesQueryAndRetriesTransientFailure(t *testing.T) {
	withNoRetrySleep(t)
	callCount := 0
	var lastCall []string
	runner := func(ctx context.Context, argv ...string) ([]byte, error) {
		callCount++
		lastCall = append([]string(nil), argv...)
		if callCount == 1 {
			return nil, fmt.Errorf("azure CLI failed: signal: killed")
		}
		if strings.Join(leadingTokens(argv), " ") == "network nic ip-config list" {
			out := cannedIPConfigList("ipcfg-mobility", "10.88.60.9")
			return out, nil
		}
		return []byte(`[]`), nil
	}
	configs, err := listIPConfigs(context.Background(), runner, "rg1", "nic1")
	if err != nil {
		t.Fatalf("listIPConfigs should retry and succeed, got err=%v", err)
	}
	if callCount != 2 {
		t.Fatalf("listIPConfigs should retry once on transient failure, got calls=%d", callCount)
	}
	if len(configs) != 1 || configs[0].Name != "ipcfg-mobility" || configs[0].PrivateIPAddress != "10.88.60.9" {
		t.Fatalf("unexpected ip-configs: %+v", configs)
	}
	if lastCall == nil || !strings.Contains(strings.Join(lastCall, " "), "--query") {
		t.Fatalf("list call should include --query, got %v", lastCall)
	}
}

func TestCreateIPConfigRetriesTransientFailure(t *testing.T) {
	withNoRetrySleep(t)
	restCount := 0
	var calls []string
	runner := func(ctx context.Context, argv ...string) ([]byte, error) {
		calls = append(calls, strings.Join(argv, " "))
		joined := strings.Join(leadingTokens(argv), " ")
		switch joined {
		case "network nic show":
			return cannedNICShow(false), nil
		case "rest":
			restCount++
			if restCount < 3 {
				return nil, fmt.Errorf("azure CLI failed: 429 too many requests")
			}
			return []byte(`{}`), nil
		default:
			return nil, fmt.Errorf("unexpected call: %v", argv)
		}
	}

	config := nicTarget{
		resourceGroup: "rg1",
		nicName:       "nic1",
		nicID:         "/subscriptions/s1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
		ipConfigName:  "ipcfg-mobility",
		address:       "10.88.60.9",
	}
	err := createIPConfig(context.Background(), runner, config)
	if err != nil {
		t.Fatalf("createIPConfig should succeed after retries, got err=%v calls=%v", err, calls)
	}

	if restCount != 3 {
		t.Fatalf("nic PUT should retry twice before success, got %d rest calls (%v)", restCount, calls)
	}
}

func TestDeleteIPConfigRetriesTransientFailure(t *testing.T) {
	withNoRetrySleep(t)
	callCount := 0
	var calls []string
	runner := func(ctx context.Context, argv ...string) ([]byte, error) {
		callCount++
		calls = append(calls, strings.Join(argv, " "))
		joined := strings.Join(leadingTokens(argv), " ")
		switch joined {
		case "network nic ip-config delete":
			if callCount < 3 {
				return nil, fmt.Errorf("azure CLI failed: temporary failure")
			}
			return []byte(`{}`), nil
		default:
			return nil, fmt.Errorf("unexpected call: %v", argv)
		}
	}

	err := deleteIPConfig(context.Background(), runner, ipConfigHolder{resourceGroup: "rg1", nicName: "nic1", ipConfigName: "ipcfg-mobility"})
	if err != nil {
		t.Fatalf("deleteIPConfig should succeed after retries, got err=%v calls=%v", err, calls)
	}

	if callCount != 3 {
		t.Fatalf("delete should retry twice before success, got calls=%d (%v)", callCount, calls)
	}
}

func TestCommandTimeoutUsesDurationEnv(t *testing.T) {
	t.Setenv(azCommandTimeoutEnv, "75s")
	t.Setenv(legacyAzCommandTimeoutMsEnv, "")

	if got := commandTimeout(); got != 75*time.Second {
		t.Fatalf("commandTimeout() = %s, want 75s", got)
	}
}

func TestCommandTimeoutUsesLegacyMillisecondsEnv(t *testing.T) {
	t.Setenv(azCommandTimeoutEnv, "")
	t.Setenv(legacyAzCommandTimeoutMsEnv, "45000")

	if got := commandTimeout(); got != 45*time.Second {
		t.Fatalf("commandTimeout() = %s, want 45s", got)
	}
}

func TestCommandTimeoutIgnoresInvalidEnv(t *testing.T) {
	t.Setenv(azCommandTimeoutEnv, "0")
	t.Setenv(legacyAzCommandTimeoutMsEnv, "-1")

	if got := commandTimeout(); got != defaultAzCommandTimeout {
		t.Fatalf("commandTimeout() = %s, want default %s", got, defaultAzCommandTimeout)
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
// imports NO cloud SDK. It LEGITIMATELY uses os/exec (it runs the `az` CLI), so
// os/exec is allowed here — but pulling in an Azure/AWS/OCI/GCP SDK is forbidden:
// the executor's only external dependency is exec of the `az` binary. (The
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
					t.Errorf("%s imports forbidden cloud SDK %q (executor may exec `az`, not link an SDK)", name, p)
				}
			}
		}
	}
	if !usesExec {
		t.Error("expected the azure executor to use os/exec to run the `az` CLI")
	}
}
