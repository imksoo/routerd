// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func pluginCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		pluginUsage(stderr)
		return errors.New("plugin requires subcommand")
	}
	switch args[0] {
	case "list":
		return pluginListCommand(args[1:], stdout)
	case "run":
		return pluginRunCommand(args[1:], stdout)
	case "help", "-h", "--help":
		pluginUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown plugin subcommand %q", args[0])
	}
}

func pluginListCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plugin list", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"List Plugin resources from startup config.",
			"routerctl plugin list --config /usr/local/etc/routerd/router.yaml\n"+
				"routerctl plugin list -o yaml")
	}
	configPath := fs.String("config", defaultConfigPath(), "startup config path")
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
		return fmt.Errorf("unexpected plugin list argument %q", fs.Arg(0))
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	rows, err := pluginListRows(router)
	if err != nil {
		return err
	}
	switch output {
	case "", "table":
		return writePluginListTable(stdout, rows)
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func pluginRunCommand(args []string, stdout io.Writer) error {
	opts, err := parsePluginRunOptions(args, stdout)
	if err != nil {
		return err
	}
	if opts.help {
		return nil
	}
	name := opts.name
	router, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	pluginSpec, err := findPluginSpec(router, name)
	if err != nil {
		return err
	}
	source := "Plugin/" + name
	previousGeneration, err := previousPluginGeneration(opts.statePath, source)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	runOpts := routerplugin.RunOptions{
		Now:                 now,
		PreviousGeneration:  previousGeneration,
		StartupConfigHash:   startupConfigHash(router),
		EffectiveGeneration: 0,
		Trigger:             routerplugin.TriggerRef{Type: "manual"},
	}

	var store *routerstate.SQLiteStore
	var runID int64
	if !opts.dryRun {
		store, err = routerstate.OpenSQLite(opts.statePath)
		if err != nil {
			return fmt.Errorf("open state database %s: %w", opts.statePath, err)
		}
		defer store.Close()
		runID, err = store.RecordPluginRun(routerstate.PluginRunRecord{
			Plugin:       name,
			TriggerType:  runOpts.Trigger.Type,
			TriggerTopic: runOpts.Trigger.Topic,
			StartedAt:    now,
			Status:       "running",
		})
		if err != nil {
			return err
		}
	}

	result, outcome, runErr := routerplugin.Run(context.Background(), pluginSpec, name, runOpts)
	if runErr != nil {
		if store != nil {
			_ = completePluginRun(store, runID, outcome, "failed", runErr.Error())
		}
		return runErr
	}
	part, err := routerplugin.DynamicConfigPartFromResult(source, previousGeneration+1, result, now)
	if err != nil {
		if store != nil {
			_ = completePluginRun(store, runID, outcome, "failed", err.Error())
		}
		return err
	}

	report := pluginRunReport{
		Plugin:      name,
		Source:      source,
		DryRun:      opts.dryRun,
		Stored:      !opts.dryRun,
		Outcome:     outcome,
		Part:        part,
		Summary:     pluginResultSummary(result),
		ActionPlans: result.Status.ActionPlans,
	}
	if store != nil {
		record, err := dynamicPartRecord(part)
		if err != nil {
			_ = completePluginRun(store, runID, outcome, "failed", err.Error())
			return err
		}
		if err := store.UpsertDynamicConfigPart(record); err != nil {
			_ = completePluginRun(store, runID, outcome, "failed", err.Error())
			return err
		}
		if err := completePluginRun(store, runID, outcome, "succeeded", ""); err != nil {
			return err
		}
	}
	switch opts.output {
	case "", "table":
		return writePluginRunTable(stdout, report)
	case "json":
		return writeJSON(stdout, report)
	case "yaml":
		return writeYAML(stdout, report)
	default:
		return fmt.Errorf("unsupported output %q", opts.output)
	}
}

type pluginRunOptions struct {
	name       string
	configPath string
	statePath  string
	dryRun     bool
	output     string
	help       bool
}

type pluginListRow struct {
	Name         string `json:"name" yaml:"name"`
	Executable   string `json:"executable" yaml:"executable"`
	Capabilities string `json:"capabilities" yaml:"capabilities"`
	Triggers     string `json:"triggers" yaml:"triggers"`
}

type pluginRunReport struct {
	Plugin      string                          `json:"plugin" yaml:"plugin"`
	Source      string                          `json:"source" yaml:"source"`
	DryRun      bool                            `json:"dryRun" yaml:"dryRun"`
	Stored      bool                            `json:"stored" yaml:"stored"`
	Outcome     routerplugin.RunOutcome         `json:"outcome" yaml:"outcome"`
	Part        dynamicconfig.DynamicConfigPart `json:"part" yaml:"part"`
	Summary     pluginRunSummary                `json:"summary" yaml:"summary"`
	ActionPlans []routerplugin.ActionPlan       `json:"actionPlans,omitempty" yaml:"actionPlans,omitempty"`
}

type pluginRunSummary struct {
	Resources   int  `json:"resources" yaml:"resources"`
	Directives  int  `json:"directives" yaml:"directives"`
	ActionPlans int  `json:"actionPlans" yaml:"actionPlans"`
	Events      int  `json:"events" yaml:"events"`
	DisplayOnly bool `json:"actionPlansDisplayOnly" yaml:"actionPlansDisplayOnly"`
}

func parsePluginRunOptions(args []string, stdout io.Writer) (pluginRunOptions, error) {
	opts := pluginRunOptions{
		configPath: defaultConfigPath(),
		statePath:  defaultStatePath(),
		output:     "table",
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help", "help":
			printPluginRunHelp(stdout)
			opts.help = true
			return opts, nil
		case "--dry-run":
			opts.dryRun = true
		case "--config":
			i++
			if i >= len(args) {
				return opts, errors.New("--config requires a value")
			}
			opts.configPath = args[i]
		case "--state-file":
			i++
			if i >= len(args) {
				return opts, errors.New("--state-file requires a value")
			}
			opts.statePath = args[i]
		case "-o", "--output":
			i++
			if i >= len(args) {
				return opts, errors.New("-o requires a value")
			}
			opts.output = args[i]
		default:
			if strings.HasPrefix(arg, "--config=") {
				opts.configPath = strings.TrimPrefix(arg, "--config=")
				continue
			}
			if strings.HasPrefix(arg, "--state-file=") {
				opts.statePath = strings.TrimPrefix(arg, "--state-file=")
				continue
			}
			if strings.HasPrefix(arg, "-o=") {
				opts.output = strings.TrimPrefix(arg, "-o=")
				continue
			}
			if strings.HasPrefix(arg, "--output=") {
				opts.output = strings.TrimPrefix(arg, "--output=")
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown plugin run option %q", arg)
			}
			if opts.name != "" {
				return opts, fmt.Errorf("unexpected plugin run argument %q", arg)
			}
			opts.name = arg
		}
	}
	if strings.TrimSpace(opts.name) == "" {
		return opts, errors.New("plugin run requires <name>")
	}
	return opts, nil
}

func pluginListRows(router *api.Router) ([]pluginListRow, error) {
	rows := []pluginListRow{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Plugin" {
			continue
		}
		spec, err := res.PluginSpec()
		if err != nil {
			return nil, err
		}
		rows = append(rows, pluginListRow{
			Name:         res.Metadata.Name,
			Executable:   spec.Executable,
			Capabilities: strings.Join(spec.Capabilities, ","),
			Triggers:     formatPluginTriggers(spec.Triggers),
		})
	}
	return rows, nil
}

func findPluginSpec(router *api.Router, name string) (api.PluginSpec, error) {
	for _, res := range router.Spec.Resources {
		if res.Kind == "Plugin" && res.Metadata.Name == name {
			return res.PluginSpec()
		}
	}
	return api.PluginSpec{}, fmt.Errorf("Plugin %q not found in startup config", name)
}

func previousPluginGeneration(statePath, source string) (int64, error) {
	if _, err := os.Stat(statePath); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	store, err := routerstate.OpenSQLiteReadOnly(statePath)
	if err != nil {
		return 0, fmt.Errorf("open state database %s: %w", statePath, err)
	}
	defer store.Close()
	records, err := store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return 0, err
	}
	var maxGeneration int64
	for _, record := range records {
		if record.Generation > maxGeneration {
			maxGeneration = record.Generation
		}
	}
	return maxGeneration, nil
}

func startupConfigHash(router *api.Router) string {
	data, _ := json.Marshal(router)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func dynamicPartRecord(part dynamicconfig.DynamicConfigPart) (routerstate.DynamicConfigPartRecord, error) {
	resources, err := json.Marshal(part.Spec.Resources)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	directives, err := json.Marshal(part.Spec.Directives)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	return routerstate.DynamicConfigPartRecord{
		Source:         part.Spec.Source,
		Generation:     part.Spec.Generation,
		ObservedAt:     part.Spec.ObservedAt,
		ExpiresAt:      part.Spec.ExpiresAt,
		Digest:         part.Spec.Digest,
		ResourcesJSON:  string(resources),
		DirectivesJSON: string(directives),
		Status:         "active",
	}, nil
}

func completePluginRun(store *routerstate.SQLiteStore, id int64, outcome routerplugin.RunOutcome, status, runError string) error {
	var exitCode *int
	if outcome.HasExitCode {
		exitCode = &outcome.ExitCode
	}
	if runError == "" {
		runError = outcome.Error
	}
	return store.CompletePluginRun(id, time.Now().UTC(), exitCode, status, outcome.StdoutDigest, outcome.Stderr, runError)
}

func pluginResultSummary(result routerplugin.PluginResult) pluginRunSummary {
	return pluginRunSummary{
		Resources:   len(result.Status.Resources),
		Directives:  len(result.Status.Directives),
		ActionPlans: len(result.Status.ActionPlans),
		Events:      len(result.Status.Events),
		DisplayOnly: true,
	}
}

func writePluginListTable(stdout io.Writer, rows []pluginListRow) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tEXECUTABLE\tCAPABILITIES\tTRIGGERS")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", row.Name, displayCell(row.Executable), displayCell(row.Capabilities), displayCell(row.Triggers))
	}
	return w.Flush()
}

func writePluginRunTable(stdout io.Writer, report pluginRunReport) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	mode := "stored"
	if report.DryRun {
		mode = "dry-run"
	}
	fmt.Fprintf(w, "Plugin:\t%s\n", report.Plugin)
	fmt.Fprintf(w, "Mode:\t%s\n", mode)
	fmt.Fprintf(w, "Source:\t%s\n", report.Source)
	fmt.Fprintf(w, "Generation:\t%d\n", report.Part.Spec.Generation)
	fmt.Fprintf(w, "Observed At:\t%s\n", formatDynamicTime(report.Part.Spec.ObservedAt))
	fmt.Fprintf(w, "Expires:\t%s\n", formatDynamicTime(report.Part.Spec.ExpiresAt))
	fmt.Fprintf(w, "Digest:\t%s\n", report.Part.Spec.Digest)
	fmt.Fprintf(w, "Resources:\t%d\n", report.Summary.Resources)
	for _, resource := range report.Part.Spec.Resources {
		fmt.Fprintf(w, "  -\t%s/%s/%s\n", resource.APIVersion, resource.Kind, resource.Metadata.Name)
	}
	fmt.Fprintf(w, "Directives:\t%d\n", report.Summary.Directives)
	for _, directive := range report.Part.Spec.Directives {
		fmt.Fprintf(w, "  -\t%s %s/%s/%s\n", directive.Op, directive.Target.APIVersion, directive.Target.Kind, directive.Target.Name)
	}
	fmt.Fprintf(w, "Action Plans:\t%d display-only; not executed\n", report.Summary.ActionPlans)
	for _, plan := range report.ActionPlans {
		fmt.Fprintf(w, "  -\t%s %s/%s\n", plan.Name, plan.Provider, plan.Action)
	}
	fmt.Fprintf(w, "Events:\t%d\n", report.Summary.Events)
	return w.Flush()
}

func formatPluginTriggers(triggers []api.PluginTrigger) string {
	if len(triggers) == 0 {
		return ""
	}
	items := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		switch trigger.Type {
		case "interval":
			items = append(items, "interval:"+trigger.Every)
		case "event":
			if trigger.Topic == "" {
				items = append(items, "event")
			} else {
				items = append(items, "event:"+trigger.Topic)
			}
		default:
			items = append(items, trigger.Type)
		}
	}
	return strings.Join(items, ",")
}

func printPluginRunHelp(stdout io.Writer) {
	fs := flag.NewFlagSet("plugin run", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.String("config", defaultConfigPath(), "startup config path")
	fs.String("state-file", defaultStatePath(), "routerd state database file")
	fs.Bool("dry-run", false, "print candidate DynamicConfigPart without writing state")
	fs.String("o", "table", "output format: table, json, yaml")
	fs.String("output", "table", "output format: table, json, yaml")
	printSubcommandHelp(fs,
		"Run one Plugin resource and optionally store its DynamicConfigPart.",
		"routerctl plugin run oci-inventory --dry-run\n"+
			"routerctl plugin run oci-inventory --state-file /var/lib/routerd/routerd.db -o yaml")
}

func pluginUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl plugin <subcommand> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "subcommands:")
	fmt.Fprintln(w, "  list [--config <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  run <name> [--dry-run] [--config <path>] [--state-file <path>] [-o table|json|yaml]")
}
