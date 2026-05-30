// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/provideraction"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// TestActionEndToEnd drives the WHOLE provider-action operator path through the
// `routerctl action` CLI functions using the SHIPPED fake executor (no real
// cloud). It exercises import -> list -> approval gate -> dry-run preview ->
// live execute -> idempotent no-op -> dryRunOnly rejection -> journal ->
// executor-reported failure, and asserts the no-cloud / no-secret invariants.
func TestActionEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	fakeBin := buildFakeProviderExecutor(t, tmp)

	statePath := filepath.Join(tmp, "routerd.db")
	const provider = "aws"

	// 1+2. Seed a DynamicConfigPart whose actionPlans carry an
	// assign-secondary-ip with an idempotencyKey, then import it via the CLI.
	seedActionPart(t, statePath, "sub-claims", []dynamicconfig.ActionPlan{
		{
			Name:           "claim-k1",
			Provider:       provider,
			Action:         "assign-secondary-ip",
			ProviderRef:    "aws-prod",
			Target:         map[string]string{"address": "10.0.0.5/32", "nicRef": "eni-1"},
			IdempotencyKey: "k1",
			Undo:           &dynamicconfig.ActionUndo{Action: "unassign-secondary-ip", Parameters: map[string]string{"address": "10.0.0.5/32"}},
		},
		// A SECOND action exercised against a dryRunOnly policy later.
		{
			Name:           "claim-k2",
			Provider:       provider,
			Action:         "assign-secondary-ip",
			ProviderRef:    "aws-prod",
			Target:         map[string]string{"address": "10.0.0.6/32", "nicRef": "eni-2"},
			IdempotencyKey: "k2",
		},
		// A THIRD action that the fake executor will report failed (via params).
		{
			Name:           "claim-k3",
			Provider:       provider,
			Action:         "assign-secondary-ip",
			ProviderRef:    "aws-prod",
			Target:         map[string]string{"address": "10.0.0.7/32", "nicRef": "eni-3"},
			IdempotencyKey: "k3",
			Parameters:     map[string]string{"fakeOutcome": "failed"},
		},
	})

	// 3. Config: a live-capable ProviderActionPolicy + an aws-executor Plugin
	// pointing at the fake binary. NO secret is configured anywhere.
	configPath := filepath.Join(tmp, "router.yaml")
	writeActionConfig(t, configPath, provider, fakeBin, false /* dryRunOnly */)
	dryRunOnlyConfig := filepath.Join(tmp, "router-dryrunonly.yaml")
	writeActionConfig(t, dryRunOnlyConfig, provider, fakeBin, true /* dryRunOnly */)

	// ---- action import -> 1+ imported (pending) ----
	out := mustRunAction(t, "import", "--state-file", statePath)
	if !strings.Contains(out, "3 inserted") {
		t.Fatalf("import: want 3 inserted, got: %s", out)
	}
	// Re-import -> 0 inserted (dedup).
	out = mustRunAction(t, "import", "--state-file", statePath)
	if !strings.Contains(out, "0 inserted") || !strings.Contains(out, "3 duplicate") {
		t.Fatalf("re-import: want 0 inserted / 3 duplicate, got: %s", out)
	}

	id1 := actionIDForKey(t, statePath, "k1")
	id2 := actionIDForKey(t, statePath, "k2")
	id3 := actionIDForKey(t, statePath, "k3")

	// ---- action list -> shows the pending action ----
	out = mustRunAction(t, "list", "--state-file", statePath)
	if !strings.Contains(out, "k1") || !strings.Contains(out, "pending") || !strings.Contains(out, "10.0.0.5/32") {
		t.Fatalf("list: want pending k1 with target, got: %s", out)
	}

	// ---- execute --approved BEFORE approval -> REJECTED (not approved) ----
	if _, err := runAction(t, "execute", strID(id1), "--approved", "--config", configPath, "--state-file", statePath); err == nil {
		t.Fatal("execute --approved before approval must be rejected")
	} else if !strings.Contains(err.Error(), "approv") {
		t.Fatalf("want approval rejection, got: %v", err)
	}
	assertStatus(t, statePath, id1, routerstate.ActionPending)

	// ---- bare execute (no mode flag) -> REJECTED (must specify mode) ----
	if _, err := runAction(t, "execute", strID(id1), "--config", configPath, "--state-file", statePath); err == nil {
		t.Fatal("bare execute must be rejected")
	} else if !strings.Contains(err.Error(), "explicit mode") {
		t.Fatalf("want explicit-mode rejection, got: %v", err)
	}
	assertStatus(t, statePath, id1, routerstate.ActionPending)

	// ---- approve ----
	out = mustRunAction(t, "approve", strID(id1), "--by", "alice", "--state-file", statePath)
	if !strings.Contains(out, "approved") {
		t.Fatalf("approve: %s", out)
	}
	assertStatus(t, statePath, id1, routerstate.ActionApproved)

	// ---- execute --dry-run -> fake executor succeeded; journal lifecycle
	//      unchanged (still approved) so a later live execute proceeds. ----
	out = mustRunAction(t, "execute", strID(id1), "--dry-run", "--config", configPath, "--state-file", statePath)
	if !strings.Contains(out, "succeeded") || !strings.Contains(out, "would assign-secondary-ip") {
		t.Fatalf("dry-run: want succeeded + 'would ...', got: %s", out)
	}
	// SEMANTICS: dry-run is a non-destructive preview and does NOT consume the
	// approval — the journal row remains approved.
	assertStatus(t, statePath, id1, routerstate.ActionApproved)

	// ---- execute --approved -> succeeded; journal shows succeeded ----
	out = mustRunAction(t, "execute", strID(id1), "--approved", "--config", configPath, "--state-file", statePath)
	if !strings.Contains(out, "succeeded") {
		t.Fatalf("live execute: want succeeded, got: %s", out)
	}
	assertStatus(t, statePath, id1, routerstate.ActionSucceeded)

	// ---- re-run --approved -> idempotent no-op (already succeeded, NOT re-run) ----
	out = mustRunAction(t, "execute", strID(id1), "--approved", "--config", configPath, "--state-file", statePath)
	if !strings.Contains(out, "succeeded") {
		t.Fatalf("re-execute should be a no-op reporting succeeded, got: %s", out)
	}
	assertStatus(t, statePath, id1, routerstate.ActionSucceeded)

	// ---- SECOND action under a dryRunOnly policy -> --approved REJECTED ----
	mustRunAction(t, "approve", strID(id2), "--state-file", statePath)
	if _, err := runAction(t, "execute", strID(id2), "--approved", "--config", dryRunOnlyConfig, "--state-file", statePath); err == nil {
		t.Fatal("execute --approved under dryRunOnly policy must be rejected")
	} else if !strings.Contains(err.Error(), "dryRunOnly") {
		t.Fatalf("want dryRunOnly rejection, got: %v", err)
	}
	assertStatus(t, statePath, id2, routerstate.ActionApproved)

	// ---- action journal shows the history (all statuses) ----
	out = mustRunAction(t, "journal", "--state-file", statePath)
	for _, want := range []string{"k1", "k2", "k3", "succeeded", "approved", "pending"} {
		if !strings.Contains(out, want) {
			t.Fatalf("journal missing %q\n%s", want, out)
		}
	}

	// ---- fake executor forced to fail -> journaled failed (separate action) ----
	mustRunAction(t, "approve", strID(id3), "--state-file", statePath)
	// The CLI execute returns an error when the executor reports failed via the
	// engine? No — the engine journals failed and Execute returns nil for an
	// executor-reported failure. The CLI prints status=failed.
	out, err := runAction(t, "execute", strID(id3), "--approved", "--config", configPath, "--state-file", statePath)
	if err != nil {
		t.Fatalf("execute of failing action should journal failed without a CLI error, got: %v\n%s", err, out)
	}
	if !strings.Contains(out, "failed") {
		t.Fatalf("want status failed reported, got: %s", out)
	}
	assertStatus(t, statePath, id3, routerstate.ActionFailed)

	// ---- show <id> renders the full record incl. result ----
	out = mustRunAction(t, "show", strID(id1), "--state-file", statePath)
	for _, want := range []string{"Status:", "succeeded", "Idempotency Key:", "k1", "10.0.0.5/32"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show missing %q\n%s", want, out)
		}
	}

	// 5. No-cloud / no-secret invariants.
	assertFakeExecutorNoCloudNoSecret(t)
}

// TestActionRollbackDryRunPreview exercises the rollback --dry-run preview path
// through the CLI without mutating the journal (succeeded action stays
// succeeded).
func TestActionRollbackDryRun(t *testing.T) {
	tmp := t.TempDir()
	fakeBin := buildFakeProviderExecutor(t, tmp)
	statePath := filepath.Join(tmp, "routerd.db")
	const provider = "aws"

	seedActionPart(t, statePath, "sub-rb", []dynamicconfig.ActionPlan{{
		Name:           "claim-r1",
		Provider:       provider,
		Action:         "assign-secondary-ip",
		ProviderRef:    "aws-prod",
		Target:         map[string]string{"address": "10.0.0.9/32", "nicRef": "eni-9"},
		IdempotencyKey: "r1",
		Undo:           &dynamicconfig.ActionUndo{Action: "unassign-secondary-ip", Parameters: map[string]string{"address": "10.0.0.9/32"}},
	}})
	configPath := filepath.Join(tmp, "router.yaml")
	writeActionConfig(t, configPath, provider, fakeBin, false)

	mustRunAction(t, "import", "--state-file", statePath)
	id := actionIDForKey(t, statePath, "r1")
	mustRunAction(t, "approve", strID(id), "--state-file", statePath)
	mustRunAction(t, "execute", strID(id), "--approved", "--config", configPath, "--state-file", statePath)
	assertStatus(t, statePath, id, routerstate.ActionSucceeded)

	out := mustRunAction(t, "rollback", strID(id), "--dry-run", "--config", configPath, "--state-file", statePath)
	if !strings.Contains(out, "succeeded") || !strings.Contains(out, "would unassign-secondary-ip") {
		t.Fatalf("rollback dry-run: want preview of undo, got: %s", out)
	}
	// Preview must NOT mutate the journal: action stays succeeded.
	assertStatus(t, statePath, id, routerstate.ActionSucceeded)

	// Bare rollback (no mode) must refuse.
	if _, err := runAction(t, "rollback", strID(id), "--config", configPath, "--state-file", statePath); err == nil {
		t.Fatal("bare rollback must be rejected")
	}
}

// --- helpers ---

func runAction(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	err := run(append([]string{"action"}, args...), &out, &errBuf)
	return out.String() + errBuf.String(), err
}

func mustRunAction(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runAction(t, args...)
	if err != nil {
		t.Fatalf("action %v failed: %v\n%s", args, err, out)
	}
	return out
}

func strID(id int64) string { return strconv.FormatInt(id, 10) }

func seedActionPart(t *testing.T, statePath, source string, plans []dynamicconfig.ActionPlan) {
	t.Helper()
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	b, err := json.Marshal(plans)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:          source,
		Generation:      1,
		ObservedAt:      time.Now().UTC(),
		Digest:          source + "-digest",
		ActionPlansJSON: string(b),
		Status:          "active",
	}); err != nil {
		t.Fatalf("seed part: %v", err)
	}
}

func actionIDForKey(t *testing.T, statePath, key string) int64 {
	t.Helper()
	store, err := routerstate.OpenSQLiteReadOnly(statePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	rec, ok, err := store.GetActionByIdempotencyKey(key)
	if err != nil || !ok {
		t.Fatalf("lookup key %q: ok=%v err=%v", key, ok, err)
	}
	return rec.ID
}

func assertStatus(t *testing.T, statePath string, id int64, want string) {
	t.Helper()
	store, err := routerstate.OpenSQLiteReadOnly(statePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	rec, ok, err := store.GetActionByID(id)
	if err != nil || !ok {
		t.Fatalf("get action %d: ok=%v err=%v", id, ok, err)
	}
	if rec.Status != want {
		t.Fatalf("action %d status: want %q, got %q", id, want, rec.Status)
	}
}

// writeActionConfig writes a Router config carrying a ProviderActionPolicy and an
// "<provider>-executor" Plugin pointing at the fake binary. NO secret is
// configured: routerd hands the executor no credentials.
func writeActionConfig(t *testing.T, path, provider, fakeBin string, dryRunOnly bool) {
	t.Helper()
	dro := "false"
	if dryRunOnly {
		dro = "true"
	}
	yaml := `apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: action-e2e
spec:
  resources:
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: ProviderActionPolicy
      metadata:
        name: policy
      spec:
        enabled: true
        dryRunOnly: ` + dro + `
        requireApproval: true
        allowedProviders:
          - ` + provider + `
        allowedActions:
          - assign-secondary-ip
          - unassign-secondary-ip
        allowedCIDRs:
          - 10.0.0.0/24
        maxActionsPerRun: 5
        allowUndo: true
    - apiVersion: plugin.routerd.net/v1alpha1
      kind: Plugin
      metadata:
        name: ` + provider + `-executor
      spec:
        executable: ` + fakeBin + `
        timeout: 10s
        capabilities:
          - execute.providerAction
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// buildFakeProviderExecutor compiles the SHIPPED examples/plugins/fake-provider-executor
// into the given temp dir. The build is to t.TempDir() so no stray binary is
// left in the working tree.
func buildFakeProviderExecutor(t *testing.T, dir string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	pkgDir := filepath.Join(repoRoot, "examples", "plugins", "fake-provider-executor")
	bin := filepath.Join(dir, "fake-provider-executor")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = pkgDir
	if outB, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake executor: %v\n%s", err, outB)
	}
	return bin
}

// assertFakeExecutorNoCloudNoSecret proves the no-real-cloud invariant: the
// shipped fake executor imports no cloud SDK, no os/exec, and no net package, so
// no real cloud call is possible; and its wire input (ExecuteActionRequest)
// carries no secret values or paths.
func assertFakeExecutorNoCloudNoSecret(t *testing.T) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	src := filepath.Join(repoRoot, "examples", "plugins", "fake-provider-executor", "main.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse fake executor: %v", err)
	}
	forbidden := []string{
		"github.com/aws/",
		"github.com/Azure/",
		"github.com/oracle/oci-go-sdk",
		"cloud.google.com/go",
		"os/exec",
		"net/http",
		"net",
	}
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		for _, bad := range forbidden {
			if p == bad || (strings.HasSuffix(bad, "/") && strings.HasPrefix(p, bad)) {
				t.Errorf("fake executor imports forbidden package %q (no real cloud / no network possible)", p)
			}
		}
	}

	// The wire request the executor receives carries no secret fields: the
	// ExecuteActionRequestSpec the fake decodes has only action/provider/target/
	// parameters/mode/idempotencyKey/context — none of which is a credential.
	// Marshal a representative request and assert no secret-shaped key/value.
	req := provideraction.NewExecuteActionRequest(provideraction.ExecuteActionRequestSpec{
		Action:         "assign-secondary-ip",
		Provider:       "aws",
		ProviderRef:    "aws-prod",
		Target:         map[string]string{"address": "10.0.0.5/32", "nicRef": "eni-1"},
		Mode:           provideraction.ModeExecute,
		IdempotencyKey: "k1",
	})
	data, _ := json.Marshal(req)
	low := strings.ToLower(string(data))
	for _, secretToken := range []string{"secret", "password", "credential", "private_key", "privatekey", "token", "accesskey"} {
		if strings.Contains(low, secretToken) {
			t.Errorf("ExecuteActionRequest wire JSON contains secret-shaped token %q: %s", secretToken, data)
		}
	}
}
