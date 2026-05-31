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
)

// fakeAz is a FAKE az command runner: it records every argv and returns canned
// show/list JSON. It NEVER calls real Azure. Tests assert against recorded calls.
type fakeAz struct {
	calls   [][]string
	showOut []byte
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
	if verb == "show" {
		if f.showOut != nil {
			return f.showOut, nil
		}
		return cannedNICShow(false), nil
	}
	// Mutating verbs return a benign (ignored) JSON body.
	return []byte(`{}`), nil
}

func cannedNICShow(ipForwarding bool) []byte {
	return []byte(fmt.Sprintf(`{"enableIPForwarding":%t}`, ipForwarding))
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

func dispatchWith(spec executeActionRequestSpec, runner azRunner) executeActionResult {
	return dispatch(context.Background(), executeActionRequest{Spec: spec}, runner)
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
	if res.Status.Observed["assignedAddress"] != "10.88.60.9/32" {
		t.Errorf("want assignedAddress observed, got %+v", res.Status.Observed)
	}
	if res.Status.Observed["ipConfigName"] != "ipcfg-mobility" {
		t.Errorf("want ipConfigName observed, got %+v", res.Status.Observed)
	}
	if len(f.calls) != 1 {
		t.Fatalf("execute assign should issue exactly one call, got %v", f.calls)
	}
	got := strings.Join(f.calls[0], " ")
	want := "network nic ip-config create --resource-group rg1 --nic-name nic1 --name ipcfg-mobility --private-ip-address 10.88.60.9/32"
	if got != want {
		t.Fatalf("assign argv mismatch:\n got: %s\nwant: %s", got, want)
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
	want := "network nic ip-config delete --resource-group rg1 --nic-name nic1 --name ipcfg-mobility"
	if got != want {
		t.Fatalf("unassign argv mismatch:\n got: %s\nwant: %s", got, want)
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
