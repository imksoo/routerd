// SPDX-License-Identifier: BSD-3-Clause

package provideraction

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

// buildFakeExecutor compiles the example fake-provider-executor into a temp dir
// and returns its path. It is the single executor binary the round-trip tests
// (here and engine_test.go) use.
func buildFakeExecutor(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	// .../pkg/provideraction/executor_test.go -> repo root
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	src := filepath.Join(root, "examples", "plugins", "fake-provider-executor")
	bin := filepath.Join(t.TempDir(), "fake-provider-executor")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake executor: %v\n%s", err, out)
	}
	return bin
}

func executorSpec(bin string) api.PluginSpec {
	return api.PluginSpec{
		Executable:   bin,
		Timeout:      "10s",
		Capabilities: []string{CapabilityExecuteProviderAction},
	}
}

func TestRunExecutorRejectsMissingCapability(t *testing.T) {
	bin := buildFakeExecutor(t)
	spec := executorSpec(bin)
	spec.Capabilities = []string{"observe.cloud"} // no execute.providerAction
	req := NewExecuteActionRequest(ExecuteActionRequestSpec{Action: "assign-secondary-ip", Provider: "aws", Mode: ModeDryRun, IdempotencyKey: "k1"})
	_, _, err := RunExecutor(context.Background(), spec, req)
	if err == nil || !strings.Contains(err.Error(), "lacks capability") {
		t.Fatalf("want capability refusal, got %v", err)
	}
}

func TestRunExecutorRejectsBadMode(t *testing.T) {
	bin := buildFakeExecutor(t)
	spec := executorSpec(bin)
	req := NewExecuteActionRequest(ExecuteActionRequestSpec{Action: "assign-secondary-ip", Provider: "aws", Mode: "apply", IdempotencyKey: "k1"})
	_, _, err := RunExecutor(context.Background(), spec, req)
	if err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("want bad-mode refusal, got %v", err)
	}
}

func TestRunExecutorRoundTripSucceeded(t *testing.T) {
	bin := buildFakeExecutor(t)
	spec := executorSpec(bin)
	req := NewExecuteActionRequest(ExecuteActionRequestSpec{
		Action:         "assign-secondary-ip",
		Provider:       "aws",
		Target:         map[string]string{"address": "10.0.0.5/32"},
		Mode:           ModeExecute,
		IdempotencyKey: "k1",
	})
	res, outcome, err := RunExecutor(context.Background(), spec, req)
	if err != nil {
		t.Fatalf("run: %v (stderr=%s)", err, outcome.Stderr)
	}
	if res.Status.Status != ResultSucceeded {
		t.Fatalf("want succeeded, got %q", res.Status.Status)
	}
	if !res.Status.UndoAvailable {
		t.Errorf("assign-secondary-ip should report UndoAvailable=true")
	}
	if res.Status.Observed["assignedAddress"] != "10.0.0.5/32" {
		t.Errorf("want observed assignedAddress, got %v", res.Status.Observed)
	}
}

func TestRunExecutorDryRunNoMutation(t *testing.T) {
	bin := buildFakeExecutor(t)
	spec := executorSpec(bin)
	req := NewExecuteActionRequest(ExecuteActionRequestSpec{Action: "assign-secondary-ip", Provider: "aws", Mode: ModeDryRun, IdempotencyKey: "k1"})
	res, _, err := RunExecutor(context.Background(), spec, req)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status.Status != ResultSucceeded {
		t.Fatalf("want succeeded, got %q", res.Status.Status)
	}
	if !strings.HasPrefix(res.Status.Message, "would ") {
		t.Errorf("dry-run message should start with 'would ', got %q", res.Status.Message)
	}
	if len(res.Status.Observed) != 0 {
		t.Errorf("dry-run must report no observed mutation facts, got %v", res.Status.Observed)
	}
}

func TestRunExecutorFakeOutcomeFailed(t *testing.T) {
	bin := buildFakeExecutor(t)
	spec := executorSpec(bin)
	req := NewExecuteActionRequest(ExecuteActionRequestSpec{
		Action:         "assign-secondary-ip",
		Provider:       "aws",
		Parameters:     map[string]string{"fakeOutcome": "failed"},
		Mode:           ModeExecute,
		IdempotencyKey: "k1",
	})
	res, _, err := RunExecutor(context.Background(), spec, req)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status.Status != ResultFailed {
		t.Fatalf("want failed, got %q", res.Status.Status)
	}
}

// TestRunExecutorEnvNotInherited proves the executor process does NOT inherit
// routerd's parent environment: a secret-shaped variable set in the parent is
// invisible to the executor, and FAKE_OUTCOME in spec.Env IS visible (proving
// only the allowlisted env passes through).
func TestRunExecutorEnvNotInherited(t *testing.T) {
	bin := buildFakeExecutor(t)

	// A secret in routerd's own environment must NOT reach the executor. If it
	// did, the executor would honor FAKE_OUTCOME and skip; since it is set only
	// in the parent (not spec.Env), the executor must NOT see it and succeeds.
	t.Setenv("FAKE_OUTCOME", "skipped")

	spec := executorSpec(bin)
	req := NewExecuteActionRequest(ExecuteActionRequestSpec{Action: "x", Provider: "aws", Mode: ModeExecute, IdempotencyKey: "k1"})
	res, _, err := RunExecutor(context.Background(), spec, req)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status.Status != ResultSucceeded {
		t.Fatalf("parent env leaked into executor: want succeeded (parent FAKE_OUTCOME ignored), got %q", res.Status.Status)
	}

	// Now pass FAKE_OUTCOME explicitly via spec.Env: the executor MUST see it.
	spec.Env = map[string]string{"FAKE_OUTCOME": "skipped"}
	res, _, err = RunExecutor(context.Background(), spec, req)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status.Status != ResultSkipped {
		t.Fatalf("spec.Env not honored: want skipped, got %q", res.Status.Status)
	}
}

func TestExecutorEnvironmentNoInherit(t *testing.T) {
	os.Setenv("ROUTERD_SECRET_PROBE", "leak")
	defer os.Unsetenv("ROUTERD_SECRET_PROBE")
	env := executorEnvironment(map[string]string{"FOO": "bar"})
	for _, kv := range env {
		if strings.HasPrefix(kv, "ROUTERD_SECRET_PROBE=") {
			t.Fatalf("parent env leaked: %q", kv)
		}
	}
	var sawFoo, sawPath bool
	for _, kv := range env {
		if kv == "FOO=bar" {
			sawFoo = true
		}
		if strings.HasPrefix(kv, "PATH=") {
			sawPath = true
		}
	}
	if !sawFoo || !sawPath {
		t.Fatalf("env missing FOO/PATH: %v", env)
	}
}
