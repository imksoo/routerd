// SPDX-License-Identifier: BSD-3-Clause

package provideraction

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/state"
)

func boolPtr(b bool) *bool { return &b }

func mustStore(t *testing.T) *state.SQLiteStore {
	t.Helper()
	s, err := state.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// fakeRunner records the last request and returns a scripted result.
type fakeRunner struct {
	calls  int
	last   ExecuteActionRequest
	result ExecuteActionResult
	err    error
}

func (f *fakeRunner) run(ctx context.Context, spec api.PluginSpec, req ExecuteActionRequest) (ExecuteActionResult, RunOutcome, error) {
	f.calls++
	f.last = req
	return f.result, RunOutcome{}, f.err
}

func succeededResult() ExecuteActionResult {
	return ExecuteActionResult{
		TypeMeta: TypeMeta{APIVersion: ProtocolAPIVersion, Kind: KindExecuteActionResult},
		Status:   ExecuteActionResultStatus{Status: ResultSucceeded, Message: "ok"},
	}
}

// executorPlugin returns a Plugin resource that resolveExecutor will match by
// the "<provider>-executor" convention.
func executorPlugin(provider string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"},
		Metadata: api.ObjectMeta{Name: provider + "-executor"},
		Spec: map[string]any{
			"executable":   "/bin/true",
			"capabilities": []any{CapabilityExecuteProviderAction},
		},
	}
}

func newEngine(t *testing.T, store Store, runner ExecutorRunner, plugins []api.Resource) *Engine {
	t.Helper()
	e, err := NewEngine(Config{Store: store, Runner: runner, Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }, Plugins: plugins})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return e
}

// seedPart writes a DynamicConfigPart with the given plans into the store.
func seedPart(t *testing.T, store *state.SQLiteStore, source string, plans []dynamicconfig.ActionPlan) {
	t.Helper()
	b, err := json.Marshal(plans)
	if err != nil {
		t.Fatal(err)
	}
	rec := state.DynamicConfigPartRecord{
		Source:          source,
		Generation:      1,
		ObservedAt:      time.Now().UTC(),
		Digest:          source + "-digest",
		ActionPlansJSON: string(b),
		Status:          "active",
	}
	if err := store.UpsertDynamicConfigPart(rec); err != nil {
		t.Fatalf("seed part: %v", err)
	}
}

func samplePlan(key string) dynamicconfig.ActionPlan {
	return dynamicconfig.ActionPlan{
		Name:           "claim-" + key,
		Provider:       "aws",
		Action:         "assign-secondary-ip",
		ProviderRef:    "aws-prod",
		Target:         map[string]string{"address": "10.0.0.5/32", "nicRef": "eni-1"},
		IdempotencyKey: key,
		Undo:           &dynamicconfig.ActionUndo{Action: "unassign-secondary-ip", Parameters: map[string]string{"address": "10.0.0.5/32"}},
	}
}

func sampleForwardingPlan(key, address string) dynamicconfig.ActionPlan {
	return dynamicconfig.ActionPlan{
		Name:           "forwarding-" + key,
		Provider:       "aws",
		Action:         "ensure-forwarding-enabled",
		ProviderRef:    "aws-prod",
		Target:         map[string]string{"address": address, "nicRef": "eni-1"},
		IdempotencyKey: key,
		Parameters:     map[string]string{"sourceDestCheck": "false"},
		Undo:           &dynamicconfig.ActionUndo{Action: "ensure-forwarding-disabled", Parameters: map[string]string{"address": address, "nicRef": "eni-1", "sourceDestCheck": "false"}},
	}
}

func allowPolicy() api.ProviderActionPolicySpec {
	return api.ProviderActionPolicySpec{
		Enabled:          true,
		DryRunOnly:       boolPtr(false),
		RequireApproval:  boolPtr(true),
		AllowedProviders: []string{"aws"},
		AllowedActions:   []string{"assign-secondary-ip", "unassign-secondary-ip"},
		AllowedCIDRs:     []string{"10.0.0.0/24"},
		MaxActionsPerRun: 5,
		AllowUndo:        true,
	}
}

func allowForwardingPolicy() api.ProviderActionPolicySpec {
	pol := allowPolicy()
	pol.AllowedActions = []string{"ensure-forwarding-enabled", "ensure-forwarding-disabled"}
	return pol
}

func TestImportDedup(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	e := newEngine(t, store, runner.run, nil)

	seedPart(t, store, "sub-a", []dynamicconfig.ActionPlan{samplePlan("k1"), samplePlan("k2")})
	res, err := e.ImportFromDynamicParts()
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Inserted != 2 || res.Duplicates != 0 {
		t.Fatalf("first import: inserted=%d dup=%d", res.Inserted, res.Duplicates)
	}
	// Re-import the same keys -> all duplicates, no new rows.
	res, err = e.ImportFromDynamicParts()
	if err != nil {
		t.Fatalf("reimport: %v", err)
	}
	if res.Inserted != 0 || res.Duplicates != 2 {
		t.Fatalf("reimport: inserted=%d dup=%d", res.Inserted, res.Duplicates)
	}
	rows, _ := store.ListActions(state.ActionExecutionFilter{})
	if len(rows) != 2 {
		t.Fatalf("want 2 journal rows, got %d", len(rows))
	}
}

func TestImportSkipsMissingIdempotencyKey(t *testing.T) {
	store := mustStore(t)
	e := newEngine(t, store, (&fakeRunner{result: succeededResult()}).run, nil)
	plan := samplePlan("")
	plan.IdempotencyKey = ""
	seedPart(t, store, "sub-a", []dynamicconfig.ActionPlan{plan})
	res, err := e.ImportFromDynamicParts()
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Skipped != 1 || res.Inserted != 0 {
		t.Fatalf("want 1 skipped 0 inserted, got skipped=%d inserted=%d", res.Skipped, res.Inserted)
	}
}

func TestImportFencesStaleMobilityCaptureEpochActions(t *testing.T) {
	store := mustStore(t)
	e := newEngine(t, store, (&fakeRunner{result: succeededResult()}).run, nil)
	captureKey := "cloudedge\x0010.0.0.5/32\x00provider:aws:placement:edge"
	if _, err := store.ReconcileMobilityCaptureEpochs([]state.MobilityCaptureEpochRecord{{
		CaptureKey:    captureKey,
		Pool:          "cloudedge",
		Address:       "10.0.0.5/32",
		CaptureDomain: "provider:aws:placement:edge",
		Holder:        "router-a",
	}}); err != nil {
		t.Fatalf("seed initial capture epoch: %v", err)
	}
	if _, err := store.ReconcileMobilityCaptureEpochs([]state.MobilityCaptureEpochRecord{{
		CaptureKey:    captureKey,
		Pool:          "cloudedge",
		Address:       "10.0.0.5/32",
		CaptureDomain: "provider:aws:placement:edge",
		Holder:        "router-b",
	}}); err != nil {
		t.Fatalf("advance capture epoch: %v", err)
	}
	oldPlan := samplePlan("old-assign")
	oldPlan.Parameters = map[string]string{
		captureParamKey:    captureKey,
		captureParamEpoch:  "1",
		captureParamHolder: "router-a",
	}
	oldRec, err := recordFromPlan("previous", oldPlan)
	if err != nil {
		t.Fatalf("recordFromPlan: %v", err)
	}
	if _, err := store.ImportAction(oldRec); err != nil {
		t.Fatalf("seed stale pending action: %v", err)
	}
	seedPart(t, store, "sub-a", []dynamicconfig.ActionPlan{oldPlan})

	res, err := e.ImportFromDynamicParts()
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Inserted != 0 || res.Skipped != 2 {
		t.Fatalf("want stale pending + stale plan skipped, got %+v", res)
	}
	rec, ok, err := store.GetActionByIdempotencyKey("old-assign")
	if err != nil || !ok {
		t.Fatalf("lookup stale action: ok=%v err=%v", ok, err)
	}
	if rec.Status != state.ActionSkipped {
		t.Fatalf("stale pending status = %q, want skipped", rec.Status)
	}
}

func TestImportAllowsCurrentEpochDeprovisionForPreviousHolder(t *testing.T) {
	store := mustStore(t)
	e := newEngine(t, store, (&fakeRunner{result: succeededResult()}).run, nil)
	captureKey := "cloudedge\x0010.0.0.5/32\x00provider:aws:placement:edge"
	if _, err := store.ReconcileMobilityCaptureEpochs([]state.MobilityCaptureEpochRecord{{
		CaptureKey:    captureKey,
		Pool:          "cloudedge",
		Address:       "10.0.0.5/32",
		CaptureDomain: "provider:aws:placement:edge",
		Holder:        "router-a",
	}}); err != nil {
		t.Fatalf("seed initial capture epoch: %v", err)
	}
	if _, err := store.ReconcileMobilityCaptureEpochs([]state.MobilityCaptureEpochRecord{{
		CaptureKey:    captureKey,
		Pool:          "cloudedge",
		Address:       "10.0.0.5/32",
		CaptureDomain: "provider:aws:placement:edge",
		Holder:        "router-b",
	}}); err != nil {
		t.Fatalf("advance capture epoch: %v", err)
	}
	plan := samplePlan("unassign-router-a-epoch-2")
	plan.Action = "unassign-secondary-ip"
	plan.Parameters = map[string]string{
		captureParamKey:    captureKey,
		captureParamEpoch:  "2",
		captureParamHolder: "router-a",
	}
	seedPart(t, store, "sub-a", []dynamicconfig.ActionPlan{plan})

	res, err := e.ImportFromDynamicParts()
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Inserted != 1 || res.Skipped != 0 {
		t.Fatalf("want current-epoch previous-holder deprovision imported, got %+v", res)
	}
}

func TestImportFencesStaleMobilityOwnershipEpochActions(t *testing.T) {
	store := mustStore(t)
	e := newEngine(t, store, (&fakeRunner{result: succeededResult()}).run, nil)
	if _, err := store.ReconcileMobilityOwnershipEpochs([]state.MobilityOwnershipEpochRecord{{
		Pool:      "cloudedge",
		Address:   "10.0.0.5/32",
		OwnerNode: "router-a",
	}}); err != nil {
		t.Fatalf("seed initial ownership epoch: %v", err)
	}
	if _, err := store.ReconcileMobilityOwnershipEpochs([]state.MobilityOwnershipEpochRecord{{
		Pool:      "cloudedge",
		Address:   "10.0.0.5/32",
		OwnerNode: "router-b",
	}}); err != nil {
		t.Fatalf("advance ownership epoch: %v", err)
	}
	oldPlan := samplePlan("old-assign")
	oldPlan.Parameters = map[string]string{
		ownershipParamPool:    "cloudedge",
		ownershipParamAddress: "10.0.0.5/32",
		ownershipParamEpoch:   "1",
		ownershipParamOwner:   "router-a",
	}
	oldRec, err := recordFromPlan("previous", oldPlan)
	if err != nil {
		t.Fatalf("recordFromPlan: %v", err)
	}
	if _, err := store.ImportAction(oldRec); err != nil {
		t.Fatalf("seed stale pending action: %v", err)
	}
	seedPart(t, store, "sub-a", []dynamicconfig.ActionPlan{oldPlan})

	res, err := e.ImportFromDynamicParts()
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Inserted != 0 || res.Skipped != 2 {
		t.Fatalf("want stale pending + stale plan skipped, got %+v", res)
	}
	rec, ok, err := store.GetActionByIdempotencyKey("old-assign")
	if err != nil || !ok {
		t.Fatalf("lookup stale action: ok=%v err=%v", ok, err)
	}
	if rec.Status != state.ActionSkipped {
		t.Fatalf("stale pending status = %q, want skipped", rec.Status)
	}
}

func TestImportAllowsCurrentOwnershipEpochDeprovisionForPreviousOwner(t *testing.T) {
	store := mustStore(t)
	e := newEngine(t, store, (&fakeRunner{result: succeededResult()}).run, nil)
	if _, err := store.ReconcileMobilityOwnershipEpochs([]state.MobilityOwnershipEpochRecord{{
		Pool:      "cloudedge",
		Address:   "10.0.0.5/32",
		OwnerNode: "router-a",
	}}); err != nil {
		t.Fatalf("seed initial ownership epoch: %v", err)
	}
	rows, err := store.ReconcileMobilityOwnershipEpochs([]state.MobilityOwnershipEpochRecord{{
		Pool:      "cloudedge",
		Address:   "10.0.0.5/32",
		OwnerNode: "router-b",
	}})
	if err != nil {
		t.Fatalf("advance ownership epoch: %v", err)
	}
	plan := samplePlan("unassign-router-a-epoch-2")
	plan.Action = "unassign-secondary-ip"
	plan.Parameters = map[string]string{
		ownershipParamPool:    "cloudedge",
		ownershipParamAddress: "10.0.0.5/32",
		ownershipParamEpoch:   fmt.Sprint(rows[0].Epoch),
		ownershipParamOwner:   "router-a",
	}
	seedPart(t, store, "sub-a", []dynamicconfig.ActionPlan{plan})

	res, err := e.ImportFromDynamicParts()
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Inserted != 1 || res.Skipped != 0 {
		t.Fatalf("want current-epoch previous-owner deprovision imported, got %+v", res)
	}
}

func TestImportFencesWrongOwnerAtCurrentOwnershipEpoch(t *testing.T) {
	store := mustStore(t)
	e := newEngine(t, store, (&fakeRunner{result: succeededResult()}).run, nil)
	rows, err := store.ReconcileMobilityOwnershipEpochs([]state.MobilityOwnershipEpochRecord{{
		Pool:      "cloudedge",
		Address:   "10.0.0.5/32",
		OwnerNode: "router-b",
	}})
	if err != nil {
		t.Fatalf("seed ownership epoch: %v", err)
	}
	plan := samplePlan("wrong-owner-assign")
	plan.Parameters = map[string]string{
		ownershipParamPool:    "cloudedge",
		ownershipParamAddress: "10.0.0.5/32",
		ownershipParamEpoch:   fmt.Sprint(rows[0].Epoch),
		ownershipParamOwner:   "router-a",
	}
	seedPart(t, store, "sub-a", []dynamicconfig.ActionPlan{plan})

	res, err := e.ImportFromDynamicParts()
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Inserted != 0 || res.Skipped != 1 {
		t.Fatalf("want wrong-owner plan skipped, got %+v", res)
	}
}

func TestImportSkipsExpiredDynamicParts(t *testing.T) {
	store := mustStore(t)
	now := time.Unix(1700000000, 0).UTC()
	plan := samplePlan("expired-key")
	data, err := json.Marshal([]dynamicconfig.ActionPlan{plan})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDynamicConfigPart(state.DynamicConfigPartRecord{
		Source:          "MobilityPool/cloudedge/node/cloud",
		Generation:      1,
		ObservedAt:      now.Add(-time.Hour),
		ExpiresAt:       now.Add(-time.Minute),
		Digest:          "expired-digest",
		ActionPlansJSON: string(data),
		Status:          "active",
	}); err != nil {
		t.Fatalf("seed expired part: %v", err)
	}
	e, err := NewEngine(Config{Store: store, Runner: (&fakeRunner{result: succeededResult()}).run, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	res, err := e.ImportFromDynamicParts()
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Inserted != 0 || res.Duplicates != 0 || res.Skipped != 0 {
		t.Fatalf("want no imports from expired part, got %+v", res)
	}
	rows, err := store.ListActions(state.ActionExecutionFilter{})
	if err != nil {
		t.Fatalf("ListActions: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("want no journal rows, got %d", len(rows))
	}
}

// importOne imports one plan and returns the journal id.
func importOne(t *testing.T, store *state.SQLiteStore, e *Engine, key string) int64 {
	t.Helper()
	seedPart(t, store, "sub-"+key, []dynamicconfig.ActionPlan{samplePlan(key)})
	if _, err := e.ImportFromDynamicParts(); err != nil {
		t.Fatalf("import: %v", err)
	}
	rec, ok, err := store.GetActionByIdempotencyKey(key)
	if err != nil || !ok {
		t.Fatalf("lookup %s: ok=%v err=%v", key, ok, err)
	}
	return rec.ID
}

func TestExecuteWithoutApprovalRejected(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	// Pending + requireApproval=true -> reject before launching executor.
	err := e.Execute(context.Background(), id, ModeExecute, allowPolicy())
	if err == nil {
		t.Fatal("expected approval rejection")
	}
	if runner.calls != 0 {
		t.Fatalf("executor must NOT be launched on approval rejection; calls=%d", runner.calls)
	}
}

func TestExecuteDryRunOnlyBlocksExecuteMode(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	if err := e.Approve(id, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	pol := allowPolicy()
	pol.DryRunOnly = boolPtr(true) // only dry-run permitted
	err := e.Execute(context.Background(), id, ModeExecute, pol)
	if err == nil {
		t.Fatal("expected dryRunOnly rejection for mode=execute")
	}
	if runner.calls != 0 {
		t.Fatalf("executor must NOT launch; calls=%d", runner.calls)
	}
}

func TestExecuteApprovedDryRunSucceeds(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	if err := e.Approve(id, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := e.Execute(context.Background(), id, ModeDryRun, allowPolicy()); err != nil {
		t.Fatalf("execute dry-run: %v", err)
	}
	if runner.calls != 1 || runner.last.Spec.Mode != ModeDryRun {
		t.Fatalf("expected 1 dry-run call, got calls=%d mode=%q", runner.calls, runner.last.Spec.Mode)
	}
	rec, _, _ := store.GetActionByID(id)
	if rec.Status != state.ActionSucceeded {
		t.Fatalf("want succeeded journaled, got %q", rec.Status)
	}
}

func TestExecuteModeExecuteApprovedSucceeds(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	if err := e.Approve(id, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := e.Execute(context.Background(), id, ModeExecute, allowPolicy()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if runner.last.Spec.Mode != ModeExecute {
		t.Fatalf("want execute mode passed to executor, got %q", runner.last.Spec.Mode)
	}
	rec, _, _ := store.GetActionByID(id)
	if rec.Status != state.ActionSucceeded {
		t.Fatalf("want succeeded, got %q", rec.Status)
	}
}

func TestExecutePolicyAutoApprove(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	pol := allowPolicy()
	pol.RequireApproval = boolPtr(false) // policy auto-approve
	// Pending + auto-approve path -> executes without operator approval.
	if err := e.Execute(context.Background(), id, ModeExecute, pol); err != nil {
		t.Fatalf("auto-approve execute: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("expected executor launched once, got %d", runner.calls)
	}
	rec, _, _ := store.GetActionByID(id)
	if rec.Status != state.ActionSucceeded {
		t.Fatalf("want succeeded, got %q", rec.Status)
	}
}

func TestExecuteDuplicateNotReExecuted(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	if err := e.Approve(id, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := e.Execute(context.Background(), id, ModeExecute, allowPolicy()); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	// Second execute on an already-succeeded action: no re-run.
	if err := e.Execute(context.Background(), id, ModeExecute, allowPolicy()); err != nil {
		t.Fatalf("second execute should be a no-op, got %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("already-succeeded action must NOT re-execute; calls=%d", runner.calls)
	}
}

func TestExecutePolicyAllowlistDenies(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(p *api.ProviderActionPolicySpec)
		plan   func(p *dynamicconfig.ActionPlan)
	}{
		{"provider", func(p *api.ProviderActionPolicySpec) { p.AllowedProviders = []string{"azure"} }, nil},
		{"action", func(p *api.ProviderActionPolicySpec) { p.AllowedActions = []string{"something-else"} }, nil},
		{"cidr", func(p *api.ProviderActionPolicySpec) { p.AllowedCIDRs = []string{"192.168.0.0/24"} }, nil},
		{"disabled", func(p *api.ProviderActionPolicySpec) { p.Enabled = false }, nil},
		{"maxActions", func(p *api.ProviderActionPolicySpec) { p.MaxActionsPerRun = 0 }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := mustStore(t)
			runner := &fakeRunner{result: succeededResult()}
			e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
			id := importOne(t, store, e, "k1")
			if err := e.Approve(id, "alice"); err != nil {
				t.Fatalf("approve: %v", err)
			}
			pol := allowPolicy()
			tc.mutate(&pol)
			if err := e.Execute(context.Background(), id, ModeExecute, pol); err == nil {
				t.Fatalf("expected %s denial", tc.name)
			}
			if runner.calls != 0 {
				t.Fatalf("executor must NOT launch on %s denial; calls=%d", tc.name, runner.calls)
			}
		})
	}
}

func TestExecuteForwardingWithTargetAddressPassesAllowedCIDRs(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	plan := sampleForwardingPlan("fwd-in-cidr", "10.0.0.5/32")
	seedPart(t, store, "sub-fwd", []dynamicconfig.ActionPlan{plan})
	if _, err := e.ImportFromDynamicParts(); err != nil {
		t.Fatalf("import: %v", err)
	}
	rec, ok, err := store.GetActionByIdempotencyKey(plan.IdempotencyKey)
	if err != nil || !ok {
		t.Fatalf("lookup %s: ok=%v err=%v", plan.IdempotencyKey, ok, err)
	}
	if err := e.Approve(rec.ID, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := e.Execute(context.Background(), rec.ID, ModeDryRun, allowForwardingPolicy()); err != nil {
		t.Fatalf("forwarding dry-run should pass allowedCIDRs: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", runner.calls)
	}
}

func TestExecuteFailedExecutorJournaledFailed(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: ExecuteActionResult{Status: ExecuteActionResultStatus{Status: ResultFailed, Error: "boom"}}}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	if err := e.Approve(id, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := e.Execute(context.Background(), id, ModeExecute, allowPolicy()); err != nil {
		t.Fatalf("execute returning failed result should not error the call: %v", err)
	}
	rec, _, _ := store.GetActionByID(id)
	if rec.Status != state.ActionFailed {
		t.Fatalf("want failed journaled (never succeeded), got %q", rec.Status)
	}
	if rec.Error != "boom" {
		t.Fatalf("want error journaled, got %q", rec.Error)
	}
}

func TestExecuteSkippedExecutorJournaledSkipped(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: ExecuteActionResult{Status: ExecuteActionResultStatus{Status: ResultSkipped, Message: "nothing to do"}}}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	if err := e.Approve(id, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := e.Execute(context.Background(), id, ModeExecute, allowPolicy()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	rec, _, _ := store.GetActionByID(id)
	if rec.Status != state.ActionSkipped {
		t.Fatalf("want skipped, got %q", rec.Status)
	}
}

// TestExecuteJournalsObserved verifies the executor's Observed facts are
// persisted to the journal on a successful execute.
func TestExecuteJournalsObserved(t *testing.T) {
	store := mustStore(t)
	res := succeededResult()
	res.Status.Observed = map[string]string{"priorSourceDestCheck": "true"}
	runner := &fakeRunner{result: res}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	if err := e.Approve(id, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := e.Execute(context.Background(), id, ModeExecute, allowPolicy()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	rec, _, _ := store.GetActionByID(id)
	if rec.Observed["priorSourceDestCheck"] != "true" {
		t.Fatalf("want priorSourceDestCheck journaled, got %+v", rec.Observed)
	}
}

// TestRollbackInjectsPriorObserved verifies Rollback reads the journal's
// Observed and injects priorSourceDestCheck into the undo request Parameters so
// the executor can restore the captured prior state.
func TestRollbackInjectsPriorObserved(t *testing.T) {
	store := mustStore(t)
	res := succeededResult()
	res.Status.Observed = map[string]string{"priorSourceDestCheck": "true"}
	runner := &fakeRunner{result: res}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	if err := e.Approve(id, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := e.Execute(context.Background(), id, ModeExecute, allowPolicy()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := e.Rollback(context.Background(), id, allowPolicy()); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := runner.last.Spec.Parameters["priorSourceDestCheck"]; got != "true" {
		t.Fatalf("undo request must carry injected priorSourceDestCheck=true, got %q (params=%+v)", got, runner.last.Spec.Parameters)
	}
	// The planned undo parameter (address) must survive the merge.
	if runner.last.Spec.Parameters["address"] != "10.0.0.5/32" {
		t.Fatalf("planned undo parameter lost: %+v", runner.last.Spec.Parameters)
	}
}

// TestRollbackInjectsFullObserved verifies Rollback injects the FULL journaled
// Observed map into the undo request Parameters (provider-agnostic), so a
// non-AWS prior key (e.g. OCI's priorSkipSourceDestCheck) also flows to its
// executor — not just the legacy AWS-specific priorSourceDestCheck.
func TestRollbackInjectsFullObserved(t *testing.T) {
	store := mustStore(t)
	res := succeededResult()
	res.Status.Observed = map[string]string{
		"priorSkipSourceDestCheck": "false",
		"priorIpForwarding":        "true",
		"assignedAddress":          "10.0.0.5/32",
	}
	runner := &fakeRunner{result: res}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	if err := e.Approve(id, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := e.Execute(context.Background(), id, ModeExecute, allowPolicy()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := e.Rollback(context.Background(), id, allowPolicy()); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	params := runner.last.Spec.Parameters
	if params["priorSkipSourceDestCheck"] != "false" {
		t.Fatalf("undo must carry injected priorSkipSourceDestCheck=false, got %+v", params)
	}
	if params["priorIpForwarding"] != "true" {
		t.Fatalf("undo must carry injected priorIpForwarding=true, got %+v", params)
	}
	// The planned undo parameter (address) must still win the merge.
	if params["address"] != "10.0.0.5/32" {
		t.Fatalf("planned undo parameter lost: %+v", params)
	}
}

func TestRollbackBestEffort(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	if err := e.Approve(id, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := e.Execute(context.Background(), id, ModeExecute, allowPolicy()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	runner.calls = 0
	// Refused when AllowUndo=false.
	pol := allowPolicy()
	pol.AllowUndo = false
	if err := e.Rollback(context.Background(), id, pol); err == nil {
		t.Fatal("rollback must be refused when allowUndo=false")
	}
	if runner.calls != 0 {
		t.Fatalf("executor must NOT launch on refused rollback; calls=%d", runner.calls)
	}
	// Allowed when AllowUndo=true.
	if err := e.Rollback(context.Background(), id, allowPolicy()); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if runner.last.Spec.Action != "unassign-secondary-ip" {
		t.Fatalf("rollback should run undo action, got %q", runner.last.Spec.Action)
	}
	rec, _, _ := store.GetActionByID(id)
	if rec.Status != state.ActionRolledBack {
		t.Fatalf("want rolledBack, got %q", rec.Status)
	}
}

func TestRollbackRefusedWhenNotSucceeded(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	// Still pending: rollback must refuse.
	if err := e.Rollback(context.Background(), id, allowPolicy()); err == nil {
		t.Fatal("rollback must refuse a non-succeeded action")
	}
	if runner.calls != 0 {
		t.Fatalf("executor must NOT launch; calls=%d", runner.calls)
	}
}

func TestExecuteBadModeRejected(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	e := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, e, "k1")
	if err := e.Execute(context.Background(), id, "apply", allowPolicy()); err == nil {
		t.Fatal("expected bad-mode rejection")
	}
	if runner.calls != 0 {
		t.Fatalf("executor must NOT launch on bad mode; calls=%d", runner.calls)
	}
}

// TestExecuteEndToEndWithRealExecutor wires the engine to RunExecutor + the
// compiled fake binary, proving the full launch path journals a success.
func TestExecuteEndToEndWithRealExecutor(t *testing.T) {
	bin := buildFakeExecutor(t)
	store := mustStore(t)
	plug := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"},
		Metadata: api.ObjectMeta{Name: "aws-executor"},
		Spec: map[string]any{
			"executable":   bin,
			"timeout":      "10s",
			"capabilities": []any{CapabilityExecuteProviderAction},
		},
	}
	e := newEngine(t, store, RunExecutor, []api.Resource{plug})
	id := importOne(t, store, e, "k1")
	if err := e.Approve(id, "alice"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := e.Execute(context.Background(), id, ModeExecute, allowPolicy()); err != nil {
		t.Fatalf("execute end-to-end: %v", err)
	}
	rec, _, _ := store.GetActionByID(id)
	if rec.Status != state.ActionSucceeded {
		t.Fatalf("want succeeded, got %q", rec.Status)
	}
}
