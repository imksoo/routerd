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

// fakeOCI is a FAKE oci command runner: it records every argv and returns canned
// get/list JSON. It NEVER calls real OCI. Tests assert against recorded calls.
type fakeOCI struct {
	calls      [][]string
	vnicGetOut []byte
	listOut    []byte
	err        error
}

func (f *fakeOCI) run(ctx context.Context, argv ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), argv...))
	if f.err != nil {
		return nil, f.err
	}
	toks := leadingTokens(argv)
	verb := ""
	if len(toks) > 0 {
		verb = toks[len(toks)-1]
	}
	switch {
	case verb == "get": // network vnic get
		if f.vnicGetOut != nil {
			return f.vnicGetOut, nil
		}
		return cannedVNICGet(false), nil
	case verb == "list": // network private-ip list
		if f.listOut != nil {
			return f.listOut, nil
		}
		return cannedPrivateIPList("10.88.60.9", "ocid1.privateip.oc1..pip9"), nil
	default:
		// Mutating verbs return a benign (ignored) JSON body.
		return []byte(`{}`), nil
	}
}

func cannedVNICGet(skip bool) []byte {
	return []byte(fmt.Sprintf(`{"data":{"skip-source-dest-check":%t}}`, skip))
}

func cannedPrivateIPList(ip, ocid string) []byte {
	return []byte(fmt.Sprintf(`{"data":[{"id":"ocid1.privateip.oc1..primary","ip-address":"10.88.60.1"},{"id":%q,"ip-address":%q}]}`, ocid, ip))
}

func reqSpec(action, mode string) executeActionRequestSpec {
	return executeActionRequestSpec{
		Action:         action,
		Provider:       "oci",
		Mode:           mode,
		IdempotencyKey: "k1",
		Target:         map[string]string{"nicRef": "ocid1.vnic.oc1..vnic1", "address": "10.88.60.9/32", "region": "ap-tokyo-1", "compartmentId": "ocid1.compartment.oc1..c1"},
	}
}

func dispatchWith(spec executeActionRequestSpec, runner ociRunner) executeActionResult {
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
	f := &fakeOCI{}
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

func TestAssignDryRunAllowReassignmentReadsOnly(t *testing.T) {
	f := &fakeOCI{}
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

func TestAssignExecuteIssuesCreate(t *testing.T) {
	f := &fakeOCI{}
	res := dispatchWith(reqSpec(actionAssignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Observed["assignedAddress"] != "10.88.60.9" {
		t.Errorf("want assignedAddress observed, got %+v", res.Status.Observed)
	}
	if len(f.calls) != 1 {
		t.Fatalf("execute assign should issue exactly one call, got %v", f.calls)
	}
	got := strings.Join(f.calls[0], " ")
	want := "network private-ip create --vnic-id ocid1.vnic.oc1..vnic1 --ip-address 10.88.60.9"
	if got != want {
		t.Fatalf("assign argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestAssignExecuteAllowReassignment(t *testing.T) {
	f := &fakeOCI{}
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Observed["assignedAddress"] != "10.88.60.9" {
		t.Errorf("want assignedAddress observed, got %+v", res.Status.Observed)
	}
	if len(f.calls) != 1 {
		t.Fatalf("execute assign should issue exactly one call, got %v", f.calls)
	}
	got := strings.Join(f.calls[0], " ")
	want := "network vnic assign-private-ip --vnic-id ocid1.vnic.oc1..vnic1 --ip-address 10.88.60.9 --unassign-if-already-assigned"
	if got != want {
		t.Fatalf("assign argv mismatch:\n got: %s\nwant: %s", got, want)
	}
	if !strings.Contains(res.Status.Message, "seized/reassigned") {
		t.Fatalf("message = %q, want seize/reassign", res.Status.Message)
	}
}

func TestEnsureForwardingEnabledDryRunCapturesPriorNoMutation(t *testing.T) {
	f := &fakeOCI{vnicGetOut: cannedVNICGet(false)}
	res := dispatchWith(reqSpec(actionEnsureFwdEnabled, modeDryRun), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q", res.Status.Status)
	}
	if res.Status.Message != "would set skipSourceDestCheck=true" {
		t.Errorf("message: %q", res.Status.Message)
	}
	if res.Status.Observed["priorSkipSourceDestCheck"] != "false" {
		t.Errorf("want priorSkipSourceDestCheck=false, got %+v", res.Status.Observed)
	}
	for _, c := range f.calls {
		if !isReadOnlyVerb(c) {
			t.Fatalf("dry-run mutated via %v", c)
		}
	}
}

func TestEnsureForwardingEnabledExecuteGetsThenUpdates(t *testing.T) {
	f := &fakeOCI{vnicGetOut: cannedVNICGet(false)}
	res := dispatchWith(reqSpec(actionEnsureFwdEnabled, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Observed["priorSkipSourceDestCheck"] != "false" {
		t.Fatalf("want prior captured =false, got %+v", res.Status.Observed)
	}
	verbs := verbsOf(f.calls)
	if len(verbs) != 2 {
		t.Fatalf("expected get THEN update (2 calls), got %v", f.calls)
	}
	if verbs[0] != "get" {
		t.Fatalf("first call must be vnic get (capture prior), got %q", verbs[0])
	}
	if verbs[1] != "update" {
		t.Fatalf("second call must be vnic update, got %q", verbs[1])
	}
	got := strings.Join(f.calls[1], " ")
	want := "network vnic update --vnic-id ocid1.vnic.oc1..vnic1 --skip-source-dest-check true"
	if got != want {
		t.Fatalf("update argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestEnsureForwardingDisabledRestoresWhenPriorFalse(t *testing.T) {
	f := &fakeOCI{}
	spec := reqSpec(actionEnsureFwdDisabled, modeExecute)
	spec.Parameters = map[string]string{"priorSkipSourceDestCheck": "false"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if len(f.calls) != 1 {
		t.Fatalf("expected one update call, got %v", f.calls)
	}
	got := strings.Join(f.calls[0], " ")
	want := "network vnic update --vnic-id ocid1.vnic.oc1..vnic1 --skip-source-dest-check false"
	if got != want {
		t.Fatalf("restore argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestEnsureForwardingDisabledNoOpWhenPriorTrue(t *testing.T) {
	f := &fakeOCI{}
	spec := reqSpec(actionEnsureFwdDisabled, modeExecute)
	spec.Parameters = map[string]string{"priorSkipSourceDestCheck": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSkipped {
		t.Fatalf("prior=true must be a NO-OP skipped, got %q", res.Status.Status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("NO-OP must issue ZERO oci calls, got %v", f.calls)
	}
	if !strings.Contains(res.Status.Message, "already true") {
		t.Errorf("message should explain the no-op, got %q", res.Status.Message)
	}
}

func TestEnsureForwardingDisabledMissingPriorErrors(t *testing.T) {
	f := &fakeOCI{}
	spec := reqSpec(actionEnsureFwdDisabled, modeExecute) // no priorSkipSourceDestCheck
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("missing priorSkipSourceDestCheck must fail (never blind), got %q", res.Status.Status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("must not call oci without a prior fact, got %v", f.calls)
	}
}

func TestUnassignExecuteLooksUpThenDeletes(t *testing.T) {
	f := &fakeOCI{listOut: cannedPrivateIPList("10.88.60.9", "ocid1.privateip.oc1..pip9")}
	res := dispatchWith(reqSpec(actionUnassignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	verbs := verbsOf(f.calls)
	if len(verbs) != 2 || verbs[0] != "list" || verbs[1] != "delete" {
		t.Fatalf("expected list (OCID lookup) THEN delete, got %v", f.calls)
	}
	gotList := strings.Join(f.calls[0], " ")
	wantList := "network private-ip list --vnic-id ocid1.vnic.oc1..vnic1"
	if gotList != wantList {
		t.Fatalf("list argv mismatch:\n got: %s\nwant: %s", gotList, wantList)
	}
	gotDel := strings.Join(f.calls[1], " ")
	wantDel := "network private-ip delete --private-ip-id ocid1.privateip.oc1..pip9 --force"
	if gotDel != wantDel {
		t.Fatalf("delete argv mismatch:\n got: %s\nwant: %s", gotDel, wantDel)
	}
	if !strings.Contains(res.Status.Message, "unassigned 10.88.60.9 from") {
		t.Fatalf("unassign message should use provider-form bare IP, got %q", res.Status.Message)
	}
}

func TestUnassignExecuteAddressNotFoundFails(t *testing.T) {
	f := &fakeOCI{listOut: cannedPrivateIPList("10.99.99.99", "ocid1.privateip.oc1..other")}
	res := dispatchWith(reqSpec(actionUnassignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("address not in list must fail (no blind delete), got %q", res.Status.Status)
	}
	// Only the list call; never a delete.
	for _, v := range verbsOf(f.calls) {
		if v == "delete" {
			t.Fatalf("must NOT delete when address not found, calls=%v", f.calls)
		}
	}
}

func TestMissingTargetFieldsError(t *testing.T) {
	cases := []struct {
		name  string
		mutTk func(m map[string]string)
	}{
		{"no vnic", func(m map[string]string) { delete(m, "nicRef") }},
		{"no address", func(m map[string]string) { delete(m, "address") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeOCI{}
			spec := reqSpec(actionAssignSecondaryIP, modeExecute)
			tc.mutTk(spec.Target)
			res := dispatchWith(spec, f.run)
			if res.Status.Status != statusFailed {
				t.Fatalf("%s should fail clearly, got %q", tc.name, res.Status.Status)
			}
			if len(f.calls) != 0 {
				t.Fatalf("%s must not invoke oci, got %v", tc.name, f.calls)
			}
		})
	}
}

func TestInvalidModeFails(t *testing.T) {
	f := &fakeOCI{}
	res := dispatchWith(reqSpec(actionAssignSecondaryIP, "apply"), f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("invalid mode must fail, got %q", res.Status.Status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("invalid mode must not invoke oci, got %v", f.calls)
	}
}

func TestGuardedRunnerRejectsNonReadOnly(t *testing.T) {
	f := &fakeOCI{}
	guarded := guardedRunner(f.run)
	if _, err := guarded(context.Background(), "network", "private-ip", "create", "--vnic-id", "v"); err == nil {
		t.Fatal("guarded runner must reject a mutating verb")
	}
	if len(f.calls) != 0 {
		t.Fatalf("guarded runner must not invoke the inner runner for a mutating verb, got %v", f.calls)
	}
	if _, err := guarded(context.Background(), "network", "vnic", "get", "--vnic-id", "v"); err != nil {
		t.Fatalf("guarded runner must allow get: %v", err)
	}
	if _, err := guarded(context.Background(), "network", "private-ip", "list", "--vnic-id", "v"); err != nil {
		t.Fatalf("guarded runner must allow list: %v", err)
	}
}

func TestRunEndToEndStdInOut(t *testing.T) {
	f := &fakeOCI{}
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

// TestExecutorImportsNoCloudSDK asserts the oci-provider-executor shipped code
// imports NO cloud SDK. It LEGITIMATELY uses os/exec (it runs the `oci` CLI), so
// os/exec is allowed here — but pulling in an OCI/AWS/Azure/GCP SDK is forbidden:
// the executor's only external dependency is exec of the `oci` binary. (The
// fleet-wide examples/plugins no-exec invariant in internal/addressclaim
// excludes THIS directory for exactly this reason.)
func TestExecutorImportsNoCloudSDK(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	dir := filepath.Dir(thisFile)
	forbidden := []string{
		"github.com/oracle/oci-go-sdk",
		"github.com/aws/",
		"github.com/Azure/",
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
					t.Errorf("%s imports forbidden cloud SDK %q (executor may exec `oci`, not link an SDK)", name, p)
				}
			}
		}
	}
	if !usesExec {
		t.Error("expected the oci executor to use os/exec to run the `oci` CLI")
	}
}

func TestResolveOCICommandReportsNonExecutableOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oci")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatalf("write fake oci: %v", err)
	}
	t.Setenv("OCI_CLI_PATH", path)
	_, err := resolveOCICommand()
	if err == nil || !strings.Contains(err.Error(), "OCI_CLI_PATH") || !strings.Contains(err.Error(), "no execute bit") {
		t.Fatalf("resolveOCICommand error = %v, want non-executable OCI_CLI_PATH error", err)
	}
}
