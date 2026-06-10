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

// fakeAWS is a FAKE aws command runner: it records every argv and returns canned
// describe JSON. It NEVER calls real AWS. Tests assert against recorded calls.
type fakeAWS struct {
	calls       [][]string
	describeOut []byte
	routeOut    []byte
	err         error
}

func (f *fakeAWS) run(ctx context.Context, argv ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), argv...))
	if f.err != nil {
		return nil, f.err
	}
	if len(argv) >= 2 && argv[1] == "describe-network-interfaces" {
		if f.describeOut != nil {
			return f.describeOut, nil
		}
		return cannedDescribe(true, "10.88.60.5"), nil
	}
	if len(argv) >= 2 && argv[1] == "describe-route-tables" {
		if f.routeOut != nil {
			return f.routeOut, nil
		}
		return cannedDescribeRouteTable("rtb-cloudedge", "10.88.60.9/32", "eni-1"), nil
	}
	// Mutating verbs return a benign (ignored) JSON body.
	return []byte(`{}`), nil
}

// cannedDescribe builds a describe-network-interfaces JSON body with the given
// SourceDestCheck and one secondary private IP.
func cannedDescribe(sourceDestCheck bool, secondary string) []byte {
	body := fmt.Sprintf(`{"NetworkInterfaces":[{"NetworkInterfaceId":"eni-1","SourceDestCheck":%t,"PrivateIpAddresses":[{"PrivateIpAddress":"10.88.60.1","Primary":true},{"PrivateIpAddress":%q,"Primary":false}]}]}`, sourceDestCheck, secondary)
	return []byte(body)
}

func cannedDescribeRouteTable(routeTable, address, eni string) []byte {
	return []byte(fmt.Sprintf(`{"RouteTables":[{"RouteTableId":%q,"Routes":[{"DestinationCidrBlock":%q,"NetworkInterfaceId":%q}]}]}`, routeTable, address, eni))
}

func reqSpec(action, mode string) executeActionRequestSpec {
	return executeActionRequestSpec{
		Action:         action,
		Provider:       "aws",
		Mode:           mode,
		IdempotencyKey: "k1",
		Target:         map[string]string{"nicRef": "eni-1", "address": "10.88.60.9/32", "region": "ap-northeast-1"},
	}
}

func routeReqSpec(action, mode string) executeActionRequestSpec {
	spec := reqSpec(action, mode)
	spec.Target["routeTableRef"] = "rtb-cloudedge"
	return spec
}

func dispatchWith(spec executeActionRequestSpec, runner awsRunner) executeActionResult {
	return dispatch(context.Background(), executeActionRequest{Spec: spec}, runner)
}

func verbsOf(calls [][]string) []string {
	var out []string
	for _, c := range calls {
		if len(c) >= 2 {
			out = append(out, c[1])
		}
	}
	return out
}

func TestAssignDryRunDescribesOnly(t *testing.T) {
	f := &fakeAWS{}
	res := dispatchWith(reqSpec(actionAssignSecondaryIP, modeDryRun), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if !strings.HasPrefix(res.Status.Message, "would assign ") {
		t.Errorf("dry-run message: %q", res.Status.Message)
	}
	if res.Status.Observed["currentSecondaryIps"] != "10.88.60.5" {
		t.Errorf("want currentSecondaryIps observed, got %+v", res.Status.Observed)
	}
	if !res.Status.UndoAvailable {
		t.Errorf("assign must report UndoAvailable")
	}
	for _, v := range verbsOf(f.calls) {
		if !strings.HasPrefix(v, "describe-") {
			t.Fatalf("dry-run issued a non-describe verb %q (must NOT mutate); calls=%v", v, f.calls)
		}
	}
}

func TestAssignExecuteIssuesAssign(t *testing.T) {
	f := &fakeAWS{}
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
	want := "ec2 assign-private-ip-addresses --network-interface-id eni-1 --private-ip-addresses 10.88.60.9 --region ap-northeast-1"
	if got != want {
		t.Fatalf("assign argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestAssignExecuteAllowReassignment(t *testing.T) {
	f := &fakeAWS{}
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	got := strings.Join(f.calls[0], " ")
	want := "ec2 assign-private-ip-addresses --network-interface-id eni-1 --private-ip-addresses 10.88.60.9 --region ap-northeast-1 --allow-reassignment"
	if got != want {
		t.Fatalf("assign argv mismatch:\n got: %s\nwant: %s", got, want)
	}
	if !strings.Contains(res.Status.Message, "seized/reassigned") {
		t.Fatalf("message = %q, want seize/reassign", res.Status.Message)
	}
}

func TestEnsureForwardingEnabledDryRunCapturesPriorNoMutation(t *testing.T) {
	f := &fakeAWS{describeOut: cannedDescribe(true, "10.88.60.5")}
	res := dispatchWith(reqSpec(actionEnsureFwdEnabled, modeDryRun), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q", res.Status.Status)
	}
	if res.Status.Message != "would set SourceDestCheck=false" {
		t.Errorf("message: %q", res.Status.Message)
	}
	if res.Status.Observed["priorSourceDestCheck"] != "true" {
		t.Errorf("want priorSourceDestCheck=true, got %+v", res.Status.Observed)
	}
	for _, v := range verbsOf(f.calls) {
		if !strings.HasPrefix(v, "describe-") {
			t.Fatalf("dry-run mutated via %q; calls=%v", v, f.calls)
		}
	}
}

func TestEnsureForwardingEnabledExecuteDescribesThenModifies(t *testing.T) {
	f := &fakeAWS{describeOut: cannedDescribe(true, "10.88.60.5")}
	res := dispatchWith(reqSpec(actionEnsureFwdEnabled, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if res.Status.Observed["priorSourceDestCheck"] != "true" {
		t.Fatalf("want prior captured =true, got %+v", res.Status.Observed)
	}
	verbs := verbsOf(f.calls)
	if len(verbs) != 2 {
		t.Fatalf("expected describe THEN modify (2 calls), got %v", f.calls)
	}
	if verbs[0] != "describe-network-interfaces" {
		t.Fatalf("first call must be describe (capture prior), got %q", verbs[0])
	}
	if verbs[1] != "modify-network-interface-attribute" {
		t.Fatalf("second call must be modify, got %q", verbs[1])
	}
	if !contains(f.calls[1], "--no-source-dest-check") {
		t.Fatalf("modify must use --no-source-dest-check, got %v", f.calls[1])
	}
}

func TestEnsureForwardingDisabledRestoresWhenPriorTrue(t *testing.T) {
	f := &fakeAWS{}
	spec := reqSpec(actionEnsureFwdDisabled, modeExecute)
	spec.Parameters = map[string]string{"priorSourceDestCheck": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if len(f.calls) != 1 || f.calls[0][1] != "modify-network-interface-attribute" {
		t.Fatalf("expected one modify call, got %v", f.calls)
	}
	if !contains(f.calls[0], "--source-dest-check") || contains(f.calls[0], "--no-source-dest-check") {
		t.Fatalf("restore must re-enable with --source-dest-check, got %v", f.calls[0])
	}
}

func TestEnsureForwardingDisabledNoOpWhenPriorFalse(t *testing.T) {
	f := &fakeAWS{}
	spec := reqSpec(actionEnsureFwdDisabled, modeExecute)
	spec.Parameters = map[string]string{"priorSourceDestCheck": "false"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSkipped {
		t.Fatalf("prior=false must be a NO-OP skipped, got %q", res.Status.Status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("NO-OP must issue ZERO aws calls, got %v", f.calls)
	}
	if !strings.Contains(res.Status.Message, "already false") {
		t.Errorf("message should explain the no-op, got %q", res.Status.Message)
	}
}

func TestEnsureForwardingDisabledMissingPriorErrors(t *testing.T) {
	f := &fakeAWS{}
	spec := reqSpec(actionEnsureFwdDisabled, modeExecute) // no priorSourceDestCheck
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("missing priorSourceDestCheck must fail (never blind re-enable), got %q", res.Status.Status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("must not call aws without a prior fact, got %v", f.calls)
	}
}

func TestUnassignExecuteIssuesUnassign(t *testing.T) {
	f := &fakeAWS{}
	res := dispatchWith(reqSpec(actionUnassignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q", res.Status.Status)
	}
	got := strings.Join(f.calls[0], " ")
	want := "ec2 unassign-private-ip-addresses --network-interface-id eni-1 --private-ip-addresses 10.88.60.9 --region ap-northeast-1"
	if got != want {
		t.Fatalf("unassign argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestAssignRouteTableExecuteCreatesRoute(t *testing.T) {
	f := &fakeAWS{}
	res := dispatchWith(routeReqSpec(actionAssignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	got := strings.Join(f.calls[0], " ")
	want := "ec2 create-route --route-table-id rtb-cloudedge --destination-cidr-block 10.88.60.9/32 --network-interface-id eni-1 --region ap-northeast-1"
	if got != want {
		t.Fatalf("create route argv mismatch:\n got: %s\nwant: %s", got, want)
	}
	if res.Status.Observed["assignedRoute"] != "10.88.60.9/32" || res.Status.Observed["routeTableRef"] != "rtb-cloudedge" || res.Status.Observed["nextHopNICRef"] != "eni-1" {
		t.Fatalf("observed = %#v, want assigned route metadata", res.Status.Observed)
	}
}

func TestAssignRouteTableSeizeReplacesRoute(t *testing.T) {
	f := &fakeAWS{}
	spec := routeReqSpec(actionAssignRouteTableRoute, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	got := strings.Join(f.calls[0], " ")
	want := "ec2 replace-route --route-table-id rtb-cloudedge --destination-cidr-block 10.88.60.9/32 --network-interface-id eni-1 --region ap-northeast-1"
	if got != want {
		t.Fatalf("replace route argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestUnassignRouteTableExecuteDeletesRoute(t *testing.T) {
	f := &fakeAWS{}
	res := dispatchWith(routeReqSpec(actionUnassignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if len(f.calls) != 2 {
		t.Fatalf("calls = %v, want describe then delete", f.calls)
	}
	if got := strings.Join(f.calls[0], " "); got != "ec2 describe-route-tables --route-table-ids rtb-cloudedge --region ap-northeast-1" {
		t.Fatalf("describe route argv mismatch:\n got: %s", got)
	}
	got := strings.Join(f.calls[1], " ")
	want := "ec2 delete-route --route-table-id rtb-cloudedge --destination-cidr-block 10.88.60.9/32 --region ap-northeast-1"
	if got != want {
		t.Fatalf("delete route argv mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestUnassignRouteTableSkipsForeignHolder(t *testing.T) {
	f := &fakeAWS{routeOut: cannedDescribeRouteTable("rtb-cloudedge", "10.88.60.9/32", "eni-other")}
	res := dispatchWith(routeReqSpec(actionUnassignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSkipped {
		t.Fatalf("want skipped, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if len(f.calls) != 1 || f.calls[0][1] != "describe-route-tables" {
		t.Fatalf("calls = %v, want describe only", f.calls)
	}
}

func TestUnassignRouteTableSkipsMissingRoute(t *testing.T) {
	f := &fakeAWS{routeOut: cannedDescribeRouteTable("rtb-cloudedge", "10.88.60.10/32", "eni-1")}
	res := dispatchWith(routeReqSpec(actionUnassignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSkipped {
		t.Fatalf("want skipped, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if len(f.calls) != 1 || f.calls[0][1] != "describe-route-tables" {
		t.Fatalf("calls = %v, want describe only", f.calls)
	}
}

func TestMissingTargetFieldsError(t *testing.T) {
	cases := []struct {
		name  string
		mutTk func(m map[string]string)
	}{
		{"no eni", func(m map[string]string) { delete(m, "nicRef") }},
		{"no region", func(m map[string]string) { delete(m, "region") }},
		{"no address", func(m map[string]string) { delete(m, "address") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAWS{}
			spec := reqSpec(actionAssignSecondaryIP, modeExecute)
			tc.mutTk(spec.Target)
			res := dispatchWith(spec, f.run)
			if res.Status.Status != statusFailed {
				t.Fatalf("%s should fail clearly, got %q", tc.name, res.Status.Status)
			}
			if len(f.calls) != 0 {
				t.Fatalf("%s must not invoke aws, got %v", tc.name, f.calls)
			}
		})
	}
}

func TestInvalidModeFails(t *testing.T) {
	f := &fakeAWS{}
	res := dispatchWith(reqSpec(actionAssignSecondaryIP, "apply"), f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("invalid mode must fail, got %q", res.Status.Status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("invalid mode must not invoke aws, got %v", f.calls)
	}
}

func TestGuardedRunnerRejectsNonDescribe(t *testing.T) {
	f := &fakeAWS{}
	guarded := guardedRunner(f.run)
	if _, err := guarded(context.Background(), "ec2", "assign-private-ip-addresses"); err == nil {
		t.Fatal("guarded runner must reject a non-describe verb")
	}
	if len(f.calls) != 0 {
		t.Fatalf("guarded runner must not invoke the inner runner for a mutating verb, got %v", f.calls)
	}
	if _, err := guarded(context.Background(), "ec2", "describe-network-interfaces", "--network-interface-ids", "eni-1", "--region", "r"); err != nil {
		t.Fatalf("guarded runner must allow describe-*: %v", err)
	}
}

func TestRunEndToEndStdInOut(t *testing.T) {
	f := &fakeAWS{}
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

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestExecutorImportsNoCloudSDK asserts the aws-provider-executor shipped code
// imports NO cloud SDK. It LEGITIMATELY uses os/exec (it runs the `aws` CLI),
// so os/exec is allowed here — but pulling in an AWS/Azure/OCI/GCP SDK is
// forbidden: the executor's only external dependency is exec of the `aws`
// binary. (The fleet-wide examples/plugins no-exec invariant in
// internal/addressclaim excludes THIS directory for exactly this reason.)
func TestExecutorImportsNoCloudSDK(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	dir := filepath.Dir(thisFile)
	forbidden := []string{
		"github.com/aws/",
		"github.com/Azure/",
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
					t.Errorf("%s imports forbidden cloud SDK %q (executor may exec `aws`, not link an SDK)", name, p)
				}
			}
		}
	}
	if !usesExec {
		t.Error("expected the aws executor to use os/exec to run the `aws` CLI")
	}
}
