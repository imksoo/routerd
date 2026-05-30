// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/provideraction"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// actionCommand is the `routerctl action` operator surface for the gated
// provider-action EXECUTION path (ADR 0007, Phase 5.0). Every subcommand opens
// the action_executions journal; execute/rollback additionally load the startup
// config for the ProviderActionPolicy + executor Plugin resources and construct
// an Engine wired to the REAL RunExecutor.
//
// SAFETY: `action execute <id>` with neither --dry-run nor --approved REFUSES so
// a bare execute never mutates. A dry-run is a non-destructive preview that does
// not consume the action's approval; a live `--approved` execute still goes
// through the engine's approval + ProviderActionPolicy gate (an explicit
// operator intent flag is not itself approval).
func actionCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		actionUsage(stderr)
		return errors.New("action requires subcommand")
	}
	switch args[0] {
	case "import":
		return actionImportCommand(args[1:], stdout)
	case "list":
		return actionListCommand(args[1:], stdout)
	case "show":
		return actionShowCommand(args[1:], stdout)
	case "approve":
		return actionApproveCommand(args[1:], stdout)
	case "execute":
		return actionExecuteCommand(args[1:], stdout)
	case "journal":
		return actionJournalCommand(args[1:], stdout)
	case "rollback":
		return actionRollbackCommand(args[1:], stdout)
	case "help", "-h", "--help":
		actionUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown action subcommand %q", args[0])
	}
}

// actionImportCommand imports actionPlans from stored DynamicConfigParts into the
// journal as pending.
func actionImportCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("action import", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Import planned provider actions from stored DynamicConfigParts into the journal as pending.",
			"routerctl action import\n"+
				"routerctl action import --state-file /var/lib/routerd/routerd.db")
	}
	configPath := fs.String("config", defaultConfigPath(), "startup config path (unused by import; accepted for symmetry)")
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected action import argument %q", fs.Arg(0))
	}
	_ = configPath // import reads only the journal + dynamic parts.

	store, err := routerstate.OpenSQLite(*statePath)
	if err != nil {
		return fmt.Errorf("open state database %s: %w", *statePath, err)
	}
	defer store.Close()
	engine, err := provideraction.NewEngine(provideraction.Config{
		Store:  store,
		Runner: provideraction.RunExecutor,
		Now:    func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return err
	}
	res, err := engine.ImportFromDynamicParts()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "imported %d action(s): %d inserted, %d duplicate, %d skipped (missing idempotencyKey)\n",
		res.Inserted+res.Duplicates, res.Inserted, res.Duplicates, res.Skipped)
	return nil
}

// actionListCommand lists journaled actions, optionally filtered.
func actionListCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("action list", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"List journaled provider actions, optionally filtered by status and/or provider.",
			"routerctl action list\n"+
				"routerctl action list --status pending -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	status := fs.String("status", "", "filter by status (pending|approved|succeeded|failed|skipped|rolledBack)")
	provider := fs.String("provider", "", "filter by provider")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected action list argument %q", fs.Arg(0))
	}
	return listActions(stdout, *statePath, routerstate.ActionExecutionFilter{Status: *status, Provider: *provider}, output)
}

// actionJournalCommand prints the full action history (all statuses, newest
// first).
func actionJournalCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("action journal", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show the full provider-action journal history (all statuses, newest first).",
			"routerctl action journal\n"+
				"routerctl action journal -o yaml")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected action journal argument %q", fs.Arg(0))
	}
	return listActions(stdout, *statePath, routerstate.ActionExecutionFilter{}, output)
}

func listActions(stdout io.Writer, statePath string, filter routerstate.ActionExecutionFilter, output string) error {
	store, err := openLedgerStateReadOnly(statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	rows, err := store.ListActions(filter)
	if err != nil {
		return err
	}
	switch output {
	case "", "table":
		return writeActionListTable(stdout, rows)
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

// actionShowCommand shows one full journal record.
func actionShowCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("action show", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show a single journaled provider action in full (target, parameters, undo, result, error).",
			"routerctl action show 1\n"+
				"routerctl action show 1 -o yaml")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	id, rest, err := splitActionID(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected action show argument %q", fs.Arg(0))
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	rec, ok, err := store.GetActionByID(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("action %d not found", id)
	}
	switch output {
	case "", "table":
		return writeActionShowTable(stdout, rec)
	case "json":
		return writeJSON(stdout, rec)
	case "yaml":
		return writeYAML(stdout, rec)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

// actionApproveCommand transitions a pending action to approved.
func actionApproveCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("action approve", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Approve a pending provider action (pending -> approved).",
			"routerctl action approve 1 --by alice")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	by := fs.String("by", "operator", "operator name recorded as the approver")
	id, rest, err := splitActionID(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected action approve argument %q", fs.Arg(0))
	}
	store, err := routerstate.OpenSQLite(*statePath)
	if err != nil {
		return fmt.Errorf("open state database %s: %w", *statePath, err)
	}
	defer store.Close()
	engine, err := provideraction.NewEngine(provideraction.Config{
		Store:  store,
		Runner: provideraction.RunExecutor,
		Now:    func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return err
	}
	if err := engine.Approve(id, *by); err != nil {
		return err
	}
	rec, _, err := store.GetActionByID(id)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "action %d approved by %s (status now %s)\n", id, displayCell(rec.ApprovedBy), rec.Status)
	return nil
}

// actionExecuteCommand runs a journaled action. It REQUIRES an explicit mode:
// --dry-run (non-destructive preview) or --approved (operator intent to execute
// live, still gated by the engine's approval + ProviderActionPolicy gate). A
// bare execute REFUSES so it can never mutate.
func actionExecuteCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("action execute", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Execute a journaled provider action. Requires --dry-run (preview) or --approved (live, gated).",
			"routerctl action execute 1 --dry-run\n"+
				"routerctl action execute 1 --approved --config /usr/local/etc/routerd/router.yaml")
	}
	configPath := fs.String("config", defaultConfigPath(), "startup config path (ProviderActionPolicy + executor Plugin)")
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	dryRun := fs.Bool("dry-run", false, "non-destructive preview: run the executor in dry-run mode, do not mutate the journal")
	approved := fs.Bool("approved", false, "operator intent to execute live (still gated by approval + policy)")
	id, rest, err := splitActionID(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected action execute argument %q", fs.Arg(0))
	}
	if *dryRun && *approved {
		return errors.New("specify only one of --dry-run or --approved")
	}
	// SAFETY: a bare `execute` never mutates.
	if !*dryRun && !*approved {
		return errors.New("action execute requires an explicit mode: pass --dry-run (preview) or --approved (live execute)")
	}

	policy, plugins, err := loadActionPolicyAndPlugins(*configPath)
	if err != nil {
		return err
	}
	store, err := routerstate.OpenSQLite(*statePath)
	if err != nil {
		return fmt.Errorf("open state database %s: %w", *statePath, err)
	}
	defer store.Close()
	engine, err := provideraction.NewEngine(provideraction.Config{
		Store:   store,
		Runner:  provideraction.RunExecutor,
		Now:     func() time.Time { return time.Now().UTC() },
		Plugins: plugins,
	})
	if err != nil {
		return err
	}

	if *dryRun {
		res, err := engine.DryRunPreview(context.Background(), id, policy)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "action %d dry-run: %s", id, res.Status)
		if res.Message != "" {
			fmt.Fprintf(stdout, " (%s)", res.Message)
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "(preview only — journal lifecycle unchanged; approval not consumed)")
		return nil
	}

	if err := engine.Execute(context.Background(), id, provideraction.ModeExecute, policy); err != nil {
		return err
	}
	rec, _, err := store.GetActionByID(id)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "action %d executed: status now %s", id, rec.Status)
	if rec.ResultMessage != "" {
		fmt.Fprintf(stdout, " (%s)", rec.ResultMessage)
	}
	fmt.Fprintln(stdout)
	return nil
}

// actionRollbackCommand previews the undo plan in dry-run mode. Live rollback is
// gated the same way as live execute (it requires --approved); Chunk 3 ships the
// --dry-run preview.
func actionRollbackCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("action rollback", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Preview (or, with --approved, perform) the best-effort undo of a succeeded action.",
			"routerctl action rollback 1 --dry-run")
	}
	configPath := fs.String("config", defaultConfigPath(), "startup config path (ProviderActionPolicy + executor Plugin)")
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	dryRun := fs.Bool("dry-run", false, "non-destructive preview of the undo plan (run the executor in dry-run mode)")
	approved := fs.Bool("approved", false, "operator intent to perform the live rollback (still gated by allowUndo + policy)")
	id, rest, err := splitActionID(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected action rollback argument %q", fs.Arg(0))
	}
	if *dryRun && *approved {
		return errors.New("specify only one of --dry-run or --approved")
	}
	if !*dryRun && !*approved {
		return errors.New("action rollback requires an explicit mode: pass --dry-run (preview) or --approved (live rollback)")
	}

	policy, plugins, err := loadActionPolicyAndPlugins(*configPath)
	if err != nil {
		return err
	}
	store, err := routerstate.OpenSQLite(*statePath)
	if err != nil {
		return fmt.Errorf("open state database %s: %w", *statePath, err)
	}
	defer store.Close()
	engine, err := provideraction.NewEngine(provideraction.Config{
		Store:   store,
		Runner:  provideraction.RunExecutor,
		Now:     func() time.Time { return time.Now().UTC() },
		Plugins: plugins,
	})
	if err != nil {
		return err
	}

	if *dryRun {
		res, err := engine.RollbackPreview(context.Background(), id, policy)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "action %d rollback dry-run: %s", id, res.Status)
		if res.Message != "" {
			fmt.Fprintf(stdout, " (%s)", res.Message)
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "(preview only — journal lifecycle unchanged)")
		return nil
	}

	if err := engine.Rollback(context.Background(), id, policy); err != nil {
		return err
	}
	rec, _, err := store.GetActionByID(id)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "action %d rolled back: status now %s\n", id, rec.Status)
	return nil
}

// loadActionPolicyAndPlugins loads the startup config and extracts the
// ProviderActionPolicy spec (the FIRST one found; absent -> the zero policy =
// disabled = the safe default that rejects execute) and every Plugin resource
// (the executor candidates).
func loadActionPolicyAndPlugins(configPath string) (api.ProviderActionPolicySpec, []api.Resource, error) {
	router, err := config.Load(configPath)
	if err != nil {
		return api.ProviderActionPolicySpec{}, nil, err
	}
	var policy api.ProviderActionPolicySpec
	var foundPolicy bool
	var plugins []api.Resource
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "ProviderActionPolicy":
			if foundPolicy {
				continue
			}
			spec, err := res.ProviderActionPolicySpec()
			if err != nil {
				return api.ProviderActionPolicySpec{}, nil, fmt.Errorf("ProviderActionPolicy %q: %w", res.Metadata.Name, err)
			}
			policy = spec
			foundPolicy = true
		case "Plugin":
			plugins = append(plugins, res)
		}
	}
	// Absent policy -> zero value (disabled); the engine rejects execute, which
	// is the safe default. No error so dry-run/show still work against a config
	// without a policy.
	return policy, plugins, nil
}

// splitActionID extracts the first positional <id> token from args (regardless
// of its position relative to flags) and returns the id plus the remaining args
// (the flags) for fs.Parse. This lets the operator write the id before OR after
// flags (e.g. `action execute 1 --approved`), which the stdlib flag package does
// not allow when the id precedes the flags.
func splitActionID(args []string) (int64, []string, error) {
	rest := make([]string, 0, len(args))
	var idStr string
	var found bool
	for _, a := range args {
		if !found && !strings.HasPrefix(a, "-") {
			idStr = a
			found = true
			continue
		}
		rest = append(rest, a)
	}
	if !found {
		return 0, nil, errors.New("requires <id>")
	}
	id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid action id %q: %w", idStr, err)
	}
	return id, rest, nil
}

func writeActionListTable(stdout io.Writer, rows []routerstate.ActionExecutionRecord) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tIDEMPOTENCY_KEY\tPROVIDER\tACTION\tSTATUS\tTARGET\tAPPROVED_BY")
	for _, rec := range rows {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			rec.ID,
			displayCell(rec.IdempotencyKey),
			displayCell(rec.Provider),
			displayCell(rec.Action),
			displayCell(rec.Status),
			displayCell(actionTargetAddress(rec.TargetJSON)),
			displayCell(rec.ApprovedBy),
		)
	}
	return w.Flush()
}

func writeActionShowTable(stdout io.Writer, rec routerstate.ActionExecutionRecord) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "ID:\t%d\n", rec.ID)
	fmt.Fprintf(w, "Idempotency Key:\t%s\n", displayCell(rec.IdempotencyKey))
	fmt.Fprintf(w, "Source:\t%s\n", displayCell(rec.Source))
	fmt.Fprintf(w, "Provider:\t%s\n", displayCell(rec.Provider))
	fmt.Fprintf(w, "Provider Ref:\t%s\n", displayCell(rec.ProviderRef))
	fmt.Fprintf(w, "Action:\t%s\n", displayCell(rec.Action))
	fmt.Fprintf(w, "Risk Level:\t%s\n", displayCell(rec.RiskLevel))
	fmt.Fprintf(w, "Status:\t%s\n", displayCell(rec.Status))
	fmt.Fprintf(w, "Target:\t%s\n", displayCell(rec.TargetJSON))
	fmt.Fprintf(w, "Parameters:\t%s\n", displayCell(rec.ParametersJSON))
	fmt.Fprintf(w, "Undo:\t%s\n", displayCell(rec.UndoJSON))
	fmt.Fprintf(w, "Approved By:\t%s\n", displayCell(rec.ApprovedBy))
	fmt.Fprintf(w, "Approved At:\t%s\n", formatDynamicTime(rec.ApprovedAt))
	fmt.Fprintf(w, "Executed At:\t%s\n", formatDynamicTime(rec.ExecutedAt))
	fmt.Fprintf(w, "Result Message:\t%s\n", displayCell(rec.ResultMessage))
	fmt.Fprintf(w, "Error:\t%s\n", displayCell(rec.Error))
	fmt.Fprintf(w, "Created At:\t%s\n", formatDynamicTime(rec.CreatedAt))
	fmt.Fprintf(w, "Updated At:\t%s\n", formatDynamicTime(rec.UpdatedAt))
	return w.Flush()
}

// actionTargetAddress extracts target.address from the JSON target for the list
// table without failing the whole render on a malformed value.
func actionTargetAddress(targetJSON string) string {
	m := decodeActionStringMap(targetJSON)
	return m["address"]
}

func decodeActionStringMap(s string) map[string]string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

func actionUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl action <subcommand> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "subcommands:")
	fmt.Fprintln(w, "  import [--config <path>] [--state-file <path>]")
	fmt.Fprintln(w, "  list [--status <s>] [--provider <p>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  show <id> [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  approve <id> [--by <name>] [--state-file <path>]")
	fmt.Fprintln(w, "  execute <id> --dry-run|--approved [--config <path>] [--state-file <path>]")
	fmt.Fprintln(w, "  journal [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  rollback <id> --dry-run|--approved [--config <path>] [--state-file <path>]")
}
