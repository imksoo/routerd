// SPDX-License-Identifier: BSD-3-Clause

package provideraction

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/state"
)

// Store is the narrow journal + dynamic-part read surface the engine needs. It
// is satisfied by *state.SQLiteStore; tests may provide a fake.
type Store interface {
	ImportAction(rec state.ActionExecutionRecord) (bool, error)
	GetActionByID(id int64) (state.ActionExecutionRecord, bool, error)
	ApproveAction(id int64, approvedBy string, now time.Time) error
	BeginActionExecution(id int64, now time.Time) (bool, error)
	RequeueStaleRunningActions(cutoff, now time.Time) (int, error)
	ListActions(state.ActionExecutionFilter) ([]state.ActionExecutionRecord, error)
	MarkActionSkippedByIdempotencyKey(key, message string, now time.Time) error
	MarkActionResult(id int64, status, message, errMsg string, observed map[string]string, executedAt time.Time) error
	MarkActionRolledBack(id int64, message string, now time.Time) error
	ListDynamicConfigParts() ([]state.DynamicConfigPartRecord, error)
}

// Logger is the minimal logging seam (skipped imports without idempotencyKey,
// gate denials). nil disables logging.
type Logger interface {
	Printf(format string, args ...any)
}

// Engine drives the gated provider-action execution path. It is pure-ish: the
// executor runner, clock, store, and logger are all injected.
type Engine struct {
	store               Store
	run                 ExecutorRunner
	now                 func() time.Time
	log                 Logger
	plugins             []api.Resource
	staleRunningTimeout time.Duration
}

// Config configures an Engine.
type Config struct {
	// Store is the journal + dynamic-part surface (required).
	Store Store
	// Runner launches an executor (required). Production: RunExecutor.
	Runner ExecutorRunner
	// Now is the clock (defaults to time.Now).
	Now func() time.Time
	// Log optionally records skips/denials.
	Log Logger
	// Plugins is the set of Plugin resources used to resolve an executor by
	// provider (see resolveExecutor). Optional for import/approve; required for
	// Execute/Rollback.
	Plugins []api.Resource
	// StaleRunningTimeout is the minimum age of a running action before it may
	// be requeued for execution after a daemon crash/restart. Defaults to
	// DefaultStaleRunningTimeout.
	StaleRunningTimeout time.Duration
}

const (
	pathSigParam = "mobilityPathSig"

	DefaultStaleRunningTimeout = 2 * time.Minute
)

// NewEngine builds an Engine.
func NewEngine(cfg Config) (*Engine, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("provideraction engine requires a Store")
	}
	if cfg.Runner == nil {
		return nil, fmt.Errorf("provideraction engine requires a Runner")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	staleRunningTimeout := cfg.StaleRunningTimeout
	if staleRunningTimeout <= 0 {
		staleRunningTimeout = DefaultStaleRunningTimeout
	}
	return &Engine{
		store:               cfg.Store,
		run:                 cfg.Runner,
		now:                 now,
		log:                 cfg.Log,
		plugins:             cfg.Plugins,
		staleRunningTimeout: staleRunningTimeout,
	}, nil
}

func (e *Engine) logf(format string, args ...any) {
	if e.log != nil {
		e.log.Printf(format, args...)
	}
}

// ImportResult summarizes an ImportFromDynamicParts run.
type ImportResult struct {
	// Inserted is the count of newly journaled pending actions.
	Inserted int
	// Duplicates is the count of plans whose idempotencyKey already existed.
	Duplicates int
	// Skipped is the count of plans skipped because they lacked an
	// idempotencyKey (logged, not journaled).
	Skipped int
}

// ImportFromDynamicParts scans every stored DynamicConfigPart's actionPlans and
// imports each into the journal as pending, keyed by ActionPlan.IdempotencyKey.
// Plans without a non-empty idempotencyKey are skipped + logged (never
// journaled). Dedup is provided by the store's ON CONFLICT(idempotency_key)
// import, so a repeated key never creates a second row.
func (e *Engine) ImportFromDynamicParts() (ImportResult, error) {
	var res ImportResult
	fenced, err := e.fenceStalePendingActions()
	if err != nil {
		return res, err
	}
	res.Skipped += fenced
	parts, err := e.store.ListDynamicConfigParts()
	if err != nil {
		return res, fmt.Errorf("list dynamic config parts: %w", err)
	}
	for _, part := range parts {
		if part.EffectiveStatus(e.now()) == "expired" {
			continue
		}
		if strings.TrimSpace(part.ActionPlansJSON) == "" {
			continue
		}
		var plans []dynamicconfig.ActionPlan
		if err := json.Unmarshal([]byte(part.ActionPlansJSON), &plans); err != nil {
			return res, fmt.Errorf("decode actionPlans for source %q: %w", part.Source, err)
		}
		for _, plan := range plans {
			if strings.TrimSpace(plan.IdempotencyKey) == "" {
				res.Skipped++
				e.logf("provideraction: skipping plan %q from source %q: missing idempotencyKey", plan.Name, part.Source)
				continue
			}
			stale, err := e.planStaleByCurrentDesired(plan)
			if err != nil {
				return res, err
			}
			if stale {
				res.Skipped++
				e.logf("provideraction: skipping stale capture action %q from source %q", plan.IdempotencyKey, part.Source)
				continue
			}
			rec, err := recordFromPlan(part.Source, plan)
			if err != nil {
				return res, err
			}
			inserted, err := e.store.ImportAction(rec)
			if err != nil {
				return res, fmt.Errorf("import action %q: %w", plan.IdempotencyKey, err)
			}
			if inserted {
				res.Inserted++
			} else {
				res.Duplicates++
			}
		}
	}
	return res, nil
}

// RecoverStaleRunningActions requeues orphaned running actions so they can flow
// through the existing policy, fencing, approval, and executor path again.
func (e *Engine) RecoverStaleRunningActions() (int, error) {
	now := e.now().UTC()
	cutoff := now.Add(-e.staleRunningTimeout)
	count, err := e.store.RequeueStaleRunningActions(cutoff, now)
	if err != nil {
		return 0, fmt.Errorf("requeue stale running provider actions: %w", err)
	}
	if count > 0 {
		e.logf("provideraction: requeued %d stale running action(s)", count)
	}
	return count, nil
}

func (e *Engine) fenceStalePendingActions() (int, error) {
	rows, err := e.store.ListActions(state.ActionExecutionFilter{})
	if err != nil {
		return 0, fmt.Errorf("list action journal for capture fencing: %w", err)
	}
	count := 0
	for _, row := range rows {
		switch row.Status {
		case state.ActionPending, state.ActionApproved:
		default:
			continue
		}
		plan, err := planFromRecord(row)
		if err != nil {
			return count, err
		}
		stale, err := e.planStaleByCurrentDesired(plan)
		if err != nil {
			return count, err
		}
		if !stale {
			continue
		}
		if err := e.store.MarkActionSkippedByIdempotencyKey(row.IdempotencyKey, "stale mobility desired path", e.now().UTC()); err != nil {
			return count, fmt.Errorf("mark stale action %q skipped: %w", row.IdempotencyKey, err)
		}
		count++
		e.logf("provideraction: fenced stale capture action %q", row.IdempotencyKey)
	}
	return count, nil
}

func (e *Engine) planStaleByCurrentDesired(plan dynamicconfig.ActionPlan) (bool, error) {
	if strings.TrimSpace(plan.Parameters[pathSigParam]) == "" {
		return false, nil
	}
	desired, err := e.currentDesiredPathFenceKeys()
	if err != nil {
		return false, err
	}
	return !desired[strings.TrimSpace(plan.IdempotencyKey)], nil
}

func (e *Engine) currentDesiredPathFenceKeys() (map[string]bool, error) {
	out := map[string]bool{}
	parts, err := e.store.ListDynamicConfigParts()
	if err != nil {
		return nil, fmt.Errorf("list dynamic config parts for provider action fencing: %w", err)
	}
	for _, part := range parts {
		if part.EffectiveStatus(e.now()) == "expired" || strings.TrimSpace(part.ActionPlansJSON) == "" {
			continue
		}
		var plans []dynamicconfig.ActionPlan
		if err := json.Unmarshal([]byte(part.ActionPlansJSON), &plans); err != nil {
			return nil, fmt.Errorf("decode actionPlans for source %q: %w", part.Source, err)
		}
		for _, plan := range plans {
			if strings.TrimSpace(plan.Parameters[pathSigParam]) == "" {
				continue
			}
			if key := strings.TrimSpace(plan.IdempotencyKey); key != "" {
				out[key] = true
			}
		}
	}
	return out, nil
}

// recordFromPlan converts a planned action into a journal record (pending). It
// JSON-encodes the target/parameters/undo so the journal mirrors the plan
// without secrets (plans never carry secrets).
func recordFromPlan(source string, plan dynamicconfig.ActionPlan) (state.ActionExecutionRecord, error) {
	rec := state.ActionExecutionRecord{
		IdempotencyKey: plan.IdempotencyKey,
		Source:         source,
		Provider:       plan.Provider,
		ProviderRef:    plan.ProviderRef,
		Action:         plan.Action,
		RiskLevel:      plan.RiskLevel,
		Status:         state.ActionPending,
	}
	if len(plan.Target) > 0 {
		b, err := json.Marshal(plan.Target)
		if err != nil {
			return rec, err
		}
		rec.TargetJSON = string(b)
	}
	if len(plan.Parameters) > 0 {
		b, err := json.Marshal(plan.Parameters)
		if err != nil {
			return rec, err
		}
		rec.ParametersJSON = string(b)
	}
	if plan.Undo != nil {
		b, err := json.Marshal(plan.Undo)
		if err != nil {
			return rec, err
		}
		rec.UndoJSON = string(b)
	}
	return rec, nil
}

// Approve transitions a pending action to approved (operator approval).
func (e *Engine) Approve(id int64, by string) error {
	return e.store.ApproveAction(id, by, e.now().UTC())
}

// Execute is the gated execution path. policy is the ProviderActionPolicy that
// governs this action; mode is "dry-run" or "execute". It evaluates the policy
// gate, enforces approval + idempotency, resolves the executor, runs it, and
// journals the outcome. On any gate failure it returns a clear error WITHOUT
// launching the executor.
func (e *Engine) Execute(ctx context.Context, id int64, mode string, policy api.ProviderActionPolicySpec) error {
	if !validMode(mode) {
		return fmt.Errorf("execute action %d: mode %q must be %q or %q", id, mode, ModeDryRun, ModeExecute)
	}
	rec, found, err := e.store.GetActionByID(id)
	if err != nil {
		return fmt.Errorf("load action %d: %w", id, err)
	}
	if !found {
		return fmt.Errorf("action %d not found", id)
	}

	// Idempotency: an already-succeeded action is never re-executed.
	if rec.Status == state.ActionSucceeded {
		e.logf("provideraction: action %d already succeeded; skipping (idempotent)", id)
		return nil
	}
	if rec.Status == state.ActionRunning {
		e.logf("provideraction: action %d is already running; skipping", id)
		return nil
	}
	if rec.Status == state.ActionRolledBack {
		return fmt.Errorf("execute action %d: action was rolled back", id)
	}

	// Policy gate (do not launch the executor on failure).
	if err := evaluatePolicy(rec, mode, policy); err != nil {
		return fmt.Errorf("policy gate denied action %d: %w", id, err)
	}
	plan, err := planFromRecord(rec)
	if err != nil {
		return err
	}
	stale, err := e.planStaleByCurrentDesired(plan)
	if err != nil {
		return err
	}
	if stale {
		if err := e.store.MarkActionSkippedByIdempotencyKey(rec.IdempotencyKey, "stale mobility desired path", e.now().UTC()); err != nil {
			return fmt.Errorf("mark stale action %q skipped: %w", rec.IdempotencyKey, err)
		}
		e.logf("provideraction: fenced stale capture action %q before execution", rec.IdempotencyKey)
		return nil
	}

	// Approval gate. The action must be approved, unless policy auto-approve
	// applies: requireApproval=false AND enabled AND !dryRunOnly AND allowlists
	// match (the latter two already verified by evaluatePolicy when mode=execute,
	// but we re-check requireApproval/enabled here explicitly).
	autoApprove := policy.RequireApproval != nil && !*policy.RequireApproval &&
		policy.Enabled && !dryRunOnly(policy)
	switch rec.Status {
	case state.ActionApproved:
		// fine
	case state.ActionPending:
		if !autoApprove {
			return fmt.Errorf("execute action %d: action is %s and not approved (approval required)", id, rec.Status)
		}
		// Policy auto-approve: stamp approval before executing.
		if err := e.store.ApproveAction(id, "policy:auto-approve", e.now().UTC()); err != nil {
			return fmt.Errorf("auto-approve action %d: %w", id, err)
		}
	default:
		return fmt.Errorf("execute action %d: action in unexpected status %q", id, rec.Status)
	}

	// Resolve the executor plugin for this provider.
	spec, pluginName, err := e.resolveExecutor(rec.Provider)
	if err != nil {
		return fmt.Errorf("resolve executor for provider %q: %w", rec.Provider, err)
	}
	claimed, err := e.store.BeginActionExecution(id, e.now().UTC())
	if err != nil {
		return fmt.Errorf("claim action %d for execution: %w", id, err)
	}
	if !claimed {
		e.logf("provideraction: action %d was already claimed or completed; skipping", id)
		return nil
	}
	e.logf("provideraction: executing action %d via plugin %q (mode=%s)", id, pluginName, mode)

	req := NewExecuteActionRequest(ExecuteActionRequestSpec{
		Action:         rec.Action,
		Provider:       rec.Provider,
		ProviderRef:    rec.ProviderRef,
		Target:         decodeStringMap(rec.TargetJSON),
		Parameters:     decodeStringMap(rec.ParametersJSON),
		Mode:           mode,
		IdempotencyKey: rec.IdempotencyKey,
	})

	result, _, runErr := e.run(ctx, spec, req)
	executedAt := e.now().UTC()
	if runErr != nil {
		// Executor process error: journal a failure (never succeeded).
		msg := fmt.Sprintf("executor invocation failed (mode=%s)", mode)
		if markErr := e.store.MarkActionResult(id, state.ActionFailed, msg, runErr.Error(), nil, executedAt); markErr != nil {
			return fmt.Errorf("execute action %d: run failed (%v) and journaling failed: %w", id, runErr, markErr)
		}
		return fmt.Errorf("execute action %d: %w", id, runErr)
	}

	// Journal the executor's reported outcome. NEVER mark succeeded when the
	// executor reported failed.
	switch result.Status.Status {
	case ResultSucceeded:
		// Journal the executor's Observed facts (e.g. priorSourceDestCheck) so the
		// undo path can later restore exactly the prior state.
		return e.store.MarkActionResult(id, state.ActionSucceeded, result.Status.Message, "", result.Status.Observed, executedAt)
	case ResultSkipped:
		return e.store.MarkActionResult(id, state.ActionSkipped, result.Status.Message, "", result.Status.Observed, executedAt)
	case ResultFailed:
		return e.store.MarkActionResult(id, state.ActionFailed, result.Status.Message, result.Status.Error, result.Status.Observed, executedAt)
	default:
		errMsg := fmt.Sprintf("executor returned invalid status %q", result.Status.Status)
		if markErr := e.store.MarkActionResult(id, state.ActionFailed, "invalid executor status", errMsg, nil, executedAt); markErr != nil {
			return fmt.Errorf("execute action %d: %s; journaling failed: %w", id, errMsg, markErr)
		}
		return fmt.Errorf("execute action %d: %s", id, errMsg)
	}
}

func planFromRecord(row state.ActionExecutionRecord) (dynamicconfig.ActionPlan, error) {
	plan := dynamicconfig.ActionPlan{
		IdempotencyKey: row.IdempotencyKey,
		Provider:       row.Provider,
		ProviderRef:    row.ProviderRef,
		Action:         row.Action,
		RiskLevel:      row.RiskLevel,
	}
	if strings.TrimSpace(row.TargetJSON) != "" {
		if err := json.Unmarshal([]byte(row.TargetJSON), &plan.Target); err != nil {
			return plan, fmt.Errorf("decode action target for %q: %w", row.IdempotencyKey, err)
		}
	}
	if strings.TrimSpace(row.ParametersJSON) != "" {
		if err := json.Unmarshal([]byte(row.ParametersJSON), &plan.Parameters); err != nil {
			return plan, fmt.Errorf("decode action parameters for %q: %w", row.IdempotencyKey, err)
		}
	}
	return plan, nil
}

// DryRunResult is the outcome of a non-destructive DryRunPreview.
type DryRunResult struct {
	// Status is the executor's reported result status (succeeded/skipped/failed).
	Status string
	// Message is the executor's human-readable summary (for a dry-run typically
	// "would <action>").
	Message string
	// Error is the executor-side error description when Status=="failed".
	Error string
}

// DryRunPreview runs the executor in dry-run mode WITHOUT mutating the journal's
// lifecycle state. It is a non-destructive preview: it evaluates the same policy
// gate as Execute (with mode=dry-run, so DryRunOnly never blocks it) and the same
// approval gate, then launches the executor in dry-run mode and returns what the
// executor reported. It NEVER transitions the action to a terminal status, so a
// preview does not consume the action's approval — a later live Execute on the
// same approved action proceeds normally. This is the semantics the
// `routerctl action execute <id> --dry-run` operator command relies on.
//
// An already-succeeded action is reported back as a no-op preview (the live
// effect already happened; nothing would change).
func (e *Engine) DryRunPreview(ctx context.Context, id int64, policy api.ProviderActionPolicySpec) (DryRunResult, error) {
	rec, found, err := e.store.GetActionByID(id)
	if err != nil {
		return DryRunResult{}, fmt.Errorf("load action %d: %w", id, err)
	}
	if !found {
		return DryRunResult{}, fmt.Errorf("action %d not found", id)
	}
	if rec.Status == state.ActionRolledBack {
		return DryRunResult{}, fmt.Errorf("dry-run action %d: action was rolled back", id)
	}

	// Policy gate (mode=dry-run is always permitted by dryRunOnly, but the
	// allowlists / enabled / maxActionsPerRun still apply). Never launches on
	// failure.
	if err := evaluatePolicy(rec, ModeDryRun, policy); err != nil {
		return DryRunResult{}, fmt.Errorf("policy gate denied dry-run of action %d: %w", id, err)
	}

	// Approval gate mirrors Execute: an action must be approved unless policy
	// auto-approve applies. A dry-run never stamps approval (non-destructive).
	autoApprove := policy.RequireApproval != nil && !*policy.RequireApproval &&
		policy.Enabled && !dryRunOnly(policy)
	switch rec.Status {
	case state.ActionApproved, state.ActionSucceeded, state.ActionFailed, state.ActionSkipped:
		// Approved or already-attempted actions may be previewed.
	case state.ActionPending:
		if !autoApprove {
			return DryRunResult{}, fmt.Errorf("dry-run action %d: action is %s and not approved (approval required)", id, rec.Status)
		}
	default:
		return DryRunResult{}, fmt.Errorf("dry-run action %d: action in unexpected status %q", id, rec.Status)
	}

	spec, pluginName, err := e.resolveExecutor(rec.Provider)
	if err != nil {
		return DryRunResult{}, fmt.Errorf("resolve executor for provider %q: %w", rec.Provider, err)
	}
	e.logf("provideraction: dry-run preview of action %d via plugin %q (no journal mutation)", id, pluginName)

	req := NewExecuteActionRequest(ExecuteActionRequestSpec{
		Action:         rec.Action,
		Provider:       rec.Provider,
		ProviderRef:    rec.ProviderRef,
		Target:         decodeStringMap(rec.TargetJSON),
		Parameters:     decodeStringMap(rec.ParametersJSON),
		Mode:           ModeDryRun,
		IdempotencyKey: rec.IdempotencyKey,
	})

	result, _, runErr := e.run(ctx, spec, req)
	if runErr != nil {
		return DryRunResult{}, fmt.Errorf("dry-run action %d: %w", id, runErr)
	}
	return DryRunResult{
		Status:  result.Status.Status,
		Message: result.Status.Message,
		Error:   result.Status.Error,
	}, nil
}

// Rollback is best-effort undo. It runs only if policy.AllowUndo, the action
// succeeded, and the journal carries an undo. It builds an ExecuteActionRequest
// for the undo action and, on executor success, marks the action rolledBack.
func (e *Engine) Rollback(ctx context.Context, id int64, policy api.ProviderActionPolicySpec) error {
	if !policy.AllowUndo {
		return fmt.Errorf("rollback action %d refused: policy.allowUndo is false", id)
	}
	rec, found, err := e.store.GetActionByID(id)
	if err != nil {
		return fmt.Errorf("load action %d: %w", id, err)
	}
	if !found {
		return fmt.Errorf("action %d not found", id)
	}
	if rec.Status != state.ActionSucceeded {
		return fmt.Errorf("rollback action %d refused: action is %s, not succeeded", id, rec.Status)
	}
	if strings.TrimSpace(rec.UndoJSON) == "" {
		return fmt.Errorf("rollback action %d refused: action has no undo", id)
	}
	var undo dynamicconfig.ActionUndo
	if err := json.Unmarshal([]byte(rec.UndoJSON), &undo); err != nil {
		return fmt.Errorf("rollback action %d: decode undo: %w", id, err)
	}
	if strings.TrimSpace(undo.Action) == "" {
		return fmt.Errorf("rollback action %d refused: undo has no action", id)
	}

	spec, pluginName, err := e.resolveExecutor(rec.Provider)
	if err != nil {
		return fmt.Errorf("rollback action %d: resolve executor: %w", id, err)
	}
	e.logf("provideraction: rolling back action %d via plugin %q (undo=%s)", id, pluginName, undo.Action)

	req := NewExecuteActionRequest(ExecuteActionRequestSpec{
		Action:         undo.Action,
		Provider:       rec.Provider,
		ProviderRef:    rec.ProviderRef,
		Target:         decodeStringMap(rec.TargetJSON),
		Parameters:     undoParameters(undo.Parameters, rec.Observed),
		Mode:           ModeExecute,
		IdempotencyKey: rec.IdempotencyKey + ":undo",
	})

	result, _, runErr := e.run(ctx, spec, req)
	if runErr != nil {
		return fmt.Errorf("rollback action %d: executor failed: %w", id, runErr)
	}
	if result.Status.Status != ResultSucceeded {
		return fmt.Errorf("rollback action %d: executor reported %q: %s", id, result.Status.Status, result.Status.Error)
	}
	msg := result.Status.Message
	if msg == "" {
		msg = "rolled back via undo " + undo.Action
	}
	return e.store.MarkActionRolledBack(id, msg, e.now().UTC())
}

// RollbackPreview runs the undo action in dry-run mode WITHOUT mutating the
// journal. It applies the same refusals as Rollback (policy.AllowUndo, the action
// must be succeeded, an undo must exist) but launches the executor with
// Mode=dry-run and NEVER transitions the action to rolledBack. It is the
// non-destructive preview behind `routerctl action rollback <id> --dry-run`.
func (e *Engine) RollbackPreview(ctx context.Context, id int64, policy api.ProviderActionPolicySpec) (DryRunResult, error) {
	if !policy.AllowUndo {
		return DryRunResult{}, fmt.Errorf("rollback action %d refused: policy.allowUndo is false", id)
	}
	rec, found, err := e.store.GetActionByID(id)
	if err != nil {
		return DryRunResult{}, fmt.Errorf("load action %d: %w", id, err)
	}
	if !found {
		return DryRunResult{}, fmt.Errorf("action %d not found", id)
	}
	if rec.Status != state.ActionSucceeded {
		return DryRunResult{}, fmt.Errorf("rollback action %d refused: action is %s, not succeeded", id, rec.Status)
	}
	if strings.TrimSpace(rec.UndoJSON) == "" {
		return DryRunResult{}, fmt.Errorf("rollback action %d refused: action has no undo", id)
	}
	var undo dynamicconfig.ActionUndo
	if err := json.Unmarshal([]byte(rec.UndoJSON), &undo); err != nil {
		return DryRunResult{}, fmt.Errorf("rollback action %d: decode undo: %w", id, err)
	}
	if strings.TrimSpace(undo.Action) == "" {
		return DryRunResult{}, fmt.Errorf("rollback action %d refused: undo has no action", id)
	}

	spec, pluginName, err := e.resolveExecutor(rec.Provider)
	if err != nil {
		return DryRunResult{}, fmt.Errorf("rollback action %d: resolve executor: %w", id, err)
	}
	e.logf("provideraction: dry-run rollback preview of action %d via plugin %q (undo=%s, no journal mutation)", id, pluginName, undo.Action)

	req := NewExecuteActionRequest(ExecuteActionRequestSpec{
		Action:         undo.Action,
		Provider:       rec.Provider,
		ProviderRef:    rec.ProviderRef,
		Target:         decodeStringMap(rec.TargetJSON),
		Parameters:     undoParameters(undo.Parameters, rec.Observed),
		Mode:           ModeDryRun,
		IdempotencyKey: rec.IdempotencyKey + ":undo",
	})

	result, _, runErr := e.run(ctx, spec, req)
	if runErr != nil {
		return DryRunResult{}, fmt.Errorf("rollback action %d: %w", id, runErr)
	}
	return DryRunResult{
		Status:  result.Status.Status,
		Message: result.Status.Message,
		Error:   result.Status.Error,
	}, nil
}

// resolveExecutor finds the executor Plugin for a provider. RESOLUTION RULE
// (Phase 5.0 convention): among the configured Plugin resources, select the one
// declaring capability execute.providerAction whose metadata.name equals
// "<provider>-executor" (e.g. aws-executor) OR, if none matches that name, the
// single Plugin that declares the capability. It errors if zero or (for the
// fallback) more than one candidate is found.
func (e *Engine) resolveExecutor(provider string) (api.PluginSpec, string, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	wantName := provider + "-executor"
	var candidates []api.Resource
	for _, res := range e.plugins {
		if res.Kind != "Plugin" {
			continue
		}
		spec, err := res.PluginSpec()
		if err != nil {
			continue
		}
		if !hasCapability(spec.Capabilities, CapabilityExecuteProviderAction) {
			continue
		}
		if strings.EqualFold(res.Metadata.Name, wantName) {
			return spec, res.Metadata.Name, nil
		}
		candidates = append(candidates, res)
	}
	switch len(candidates) {
	case 0:
		return api.PluginSpec{}, "", fmt.Errorf("no Plugin with capability %q named %q (or sole executor)", CapabilityExecuteProviderAction, wantName)
	case 1:
		spec, err := candidates[0].PluginSpec()
		if err != nil {
			return api.PluginSpec{}, "", err
		}
		return spec, candidates[0].Metadata.Name, nil
	default:
		return api.PluginSpec{}, "", fmt.Errorf("ambiguous executor for provider %q: %d executors found, none named %q", provider, len(candidates), wantName)
	}
}

// dryRunOnly reports the effective DryRunOnly value (nil defaults to true).
func dryRunOnly(policy api.ProviderActionPolicySpec) bool {
	if policy.DryRunOnly == nil {
		return true
	}
	return *policy.DryRunOnly
}

// evaluatePolicy verifies the allowlists and execution constraints. It never
// launches anything. For mode=="execute" it additionally requires that live
// mutation is permitted (Enabled && !DryRunOnly).
func evaluatePolicy(rec state.ActionExecutionRecord, mode string, policy api.ProviderActionPolicySpec) error {
	if !policy.Enabled {
		return fmt.Errorf("execution is disabled (policy.enabled=false)")
	}
	if mode == ModeExecute && dryRunOnly(policy) {
		return fmt.Errorf("live execution rejected: policy.dryRunOnly is true (mode=execute not permitted)")
	}
	if policy.MaxActionsPerRun <= 0 {
		return fmt.Errorf("maxActionsPerRun is %d: no actions permitted (operator must set a positive bound)", policy.MaxActionsPerRun)
	}
	if !inList(policy.AllowedProviders, rec.Provider) {
		return fmt.Errorf("provider %q is not in allowedProviders", rec.Provider)
	}
	if len(policy.AllowedProviderRefs) > 0 && !inList(policy.AllowedProviderRefs, rec.ProviderRef) {
		return fmt.Errorf("providerRef %q is not in allowedProviderRefs", rec.ProviderRef)
	}
	if !inList(policy.AllowedActions, rec.Action) {
		return fmt.Errorf("action %q is not in allowedActions", rec.Action)
	}
	if len(policy.AllowedCIDRs) > 0 {
		if err := targetAddressInCIDRs(rec, policy.AllowedCIDRs); err != nil {
			return err
		}
	}
	if err := checkExecutionWindow(policy.ExecutionWindow); err != nil {
		return err
	}
	return nil
}

// targetAddressInCIDRs verifies the action target.address falls within one of
// the allowed CIDRs. A missing target address is a denial when CIDRs are set.
func targetAddressInCIDRs(rec state.ActionExecutionRecord, cidrs []string) error {
	target := decodeStringMap(rec.TargetJSON)
	addr := strings.TrimSpace(target["address"])
	if addr == "" {
		return fmt.Errorf("target has no address but allowedCIDRs is set")
	}
	ip := parseAddr(addr)
	if ip == nil {
		return fmt.Errorf("target address %q is not a valid IP/CIDR", addr)
	}
	for _, c := range cidrs {
		_, ipnet, err := net.ParseCIDR(strings.TrimSpace(c))
		if err != nil {
			continue
		}
		if ipnet.Contains(ip) {
			return nil
		}
	}
	return fmt.Errorf("target address %q is not within any allowedCIDR", addr)
}

// parseAddr parses a bare IP or the IP part of a CIDR ("10.0.0.5" or
// "10.0.0.5/32").
func parseAddr(addr string) net.IP {
	if ip := net.ParseIP(addr); ip != nil {
		return ip
	}
	if ip, _, err := net.ParseCIDR(addr); err == nil {
		return ip
	}
	return nil
}

// checkExecutionWindow is intentionally lenient (ADR 0007): it only rejects a
// window that is set and clearly violated. The Phase 5.0 implementation accepts
// any non-empty window (richer scheduling arrives in Phase 5.x), so it never
// rejects here. The hook exists so the engine signature is window-aware.
func checkExecutionWindow(window string) error {
	// Lenient: any free-form window is allowed in Phase 5.0.
	_ = strings.TrimSpace(window)
	return nil
}

func inList(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// undoParameters merges the planned undo parameters with ALL the prior facts the
// engine read back from the journal's Observed map. The FULL Observed map is
// injected (provider-agnostic): each provider executor reads only its own prior
// key from the undo request Parameters — AWS reads priorSourceDestCheck, OCI
// reads priorSkipSourceDestCheck, Azure reads priorIpForwarding. The undo MUST
// NOT blindly revert; it reverts only what was actually changed using its
// captured prior fact (see docs/how-to/aws-provider-action-execution.md). This
// is why the engine injects every Observed fact rather than an AWS-specific
// allowlist: the executor, not the engine, knows which key it captured.
//
// Planned parameters win on a key collision (the planner is explicit); otherwise
// the journaled prior fact is injected so the executor restores the captured
// state. Returns a fresh map so the stored record is never mutated.
func undoParameters(planned, observed map[string]string) map[string]string {
	if len(planned) == 0 && len(observed) == 0 {
		return nil
	}
	out := make(map[string]string, len(planned)+len(observed))
	for k, v := range observed {
		out[k] = v
	}
	for k, v := range planned {
		out[k] = v
	}
	return out
}

func decodeStringMap(s string) map[string]string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}
