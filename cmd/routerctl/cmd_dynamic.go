// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func dynamicCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		dynamicUsage(stderr)
		return errors.New("dynamic requires subcommand")
	}
	switch args[0] {
	case "list":
		return dynamicListCommand(args[1:], stdout)
	case "describe":
		return dynamicDescribeCommand(args[1:], stdout)
	case "render":
		return dynamicRenderCommand(args[1:], stdout)
	case "diff":
		return dynamicDiffCommand(args[1:], stdout)
	case "help", "-h", "--help":
		dynamicUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown dynamic subcommand %q", args[0])
	}
}

func dynamicRenderCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("dynamic render", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Render startup config merged with active DynamicConfigPart records.",
			"routerctl dynamic render --config /usr/local/etc/routerd/router.yaml\n"+
				"routerctl dynamic render --state-file /var/lib/routerd/routerd.db -o json")
	}
	configPath := fs.String("config", defaultConfigPath(), "startup config path")
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	output := "yaml"
	fs.StringVar(&output, "o", "yaml", "output format: yaml, json")
	fs.StringVar(&output, "output", "yaml", "output format: yaml, json")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected dynamic render argument %q", fs.Arg(0))
	}
	effective, _, err := loadEffectiveDynamicConfig(*configPath, *statePath, time.Now().UTC())
	if err != nil {
		return err
	}
	switch output {
	case "", "yaml":
		return writeYAML(stdout, effective)
	case "json":
		return writeJSON(stdout, effective)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func dynamicDiffCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("dynamic diff", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show active dynamic resources added to startup config and startup resources suppressed by masks.",
			"routerctl dynamic diff --config /usr/local/etc/routerd/router.yaml\n"+
				"routerctl dynamic diff --state-file /var/lib/routerd/routerd.db -o json")
	}
	configPath := fs.String("config", defaultConfigPath(), "startup config path")
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	output := "text"
	fs.StringVar(&output, "o", "text", "output format: text, json")
	fs.StringVar(&output, "output", "text", "output format: text, json")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected dynamic diff argument %q", fs.Arg(0))
	}
	_, result, err := loadEffectiveDynamicConfig(*configPath, *statePath, time.Now().UTC())
	if err != nil {
		return err
	}
	switch output {
	case "", "text", "table":
		return writeDynamicDiff(stdout, result)
	case "json":
		return writeJSON(stdout, result)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func loadEffectiveDynamicConfig(configPath, statePath string, now time.Time) (api.Router, dynamicconfig.EffectiveResult, error) {
	router, err := config.Load(configPath)
	if err != nil {
		return api.Router{}, dynamicconfig.EffectiveResult{}, err
	}
	store, err := openLedgerStateReadOnly(statePath)
	if err != nil {
		return api.Router{}, dynamicconfig.EffectiveResult{}, err
	}
	defer store.Close()
	records, err := store.ListDynamicConfigParts()
	if err != nil {
		return api.Router{}, dynamicconfig.EffectiveResult{}, err
	}
	parts, err := dynamicPartsFromRecords(records)
	if err != nil {
		return api.Router{}, dynamicconfig.EffectiveResult{}, err
	}
	policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*router)
	if err != nil {
		return api.Router{}, dynamicconfig.EffectiveResult{}, err
	}
	return dynamicconfig.BuildEffectiveConfig(*router, parts, policies, now)
}

func dynamicListCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("dynamic list", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show stored DynamicConfigPart generations from the routerd state database.",
			"routerctl dynamic list\n"+
				"routerctl dynamic list --state-file /var/lib/routerd/state.db -o json")
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
		return fmt.Errorf("unexpected dynamic list argument %q", fs.Arg(0))
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	parts, err := store.ListDynamicConfigParts()
	if err != nil {
		return err
	}
	rows, err := dynamicListRows(parts, time.Now().UTC())
	if err != nil {
		return err
	}
	switch output {
	case "", "table":
		return writeDynamicListTable(stdout, rows)
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func dynamicDescribeCommand(args []string, stdout io.Writer) error {
	opts, err := parseDynamicDescribeOptions(args, stdout)
	if err != nil {
		return err
	}
	if opts.help {
		return nil
	}
	store, err := openLedgerStateReadOnly(opts.statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	parts, err := store.GetDynamicConfigPartsBySource(opts.source)
	if err != nil {
		return err
	}
	details, err := dynamicPartDetails(parts, time.Now().UTC())
	if err != nil {
		return err
	}
	switch opts.output {
	case "", "table":
		return writeDynamicDescribeTable(stdout, details)
	case "json":
		return writeJSON(stdout, details)
	case "yaml":
		return writeYAML(stdout, details)
	default:
		return fmt.Errorf("unsupported output %q", opts.output)
	}
}

type dynamicDescribeOptions struct {
	source    string
	statePath string
	output    string
	help      bool
}

func parseDynamicDescribeOptions(args []string, stdout io.Writer) (dynamicDescribeOptions, error) {
	opts := dynamicDescribeOptions{statePath: defaultStatePath(), output: "table"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help", "help":
			printDynamicDescribeHelp(stdout)
			opts.help = true
			return opts, nil
		case "-o", "--output":
			i++
			if i >= len(args) {
				return opts, errors.New("-o requires a value")
			}
			opts.output = args[i]
		case "--state-file":
			i++
			if i >= len(args) {
				return opts, errors.New("--state-file requires a value")
			}
			opts.statePath = args[i]
		default:
			if strings.HasPrefix(arg, "-o=") {
				opts.output = strings.TrimPrefix(arg, "-o=")
				continue
			}
			if strings.HasPrefix(arg, "--output=") {
				opts.output = strings.TrimPrefix(arg, "--output=")
				continue
			}
			if strings.HasPrefix(arg, "--state-file=") {
				opts.statePath = strings.TrimPrefix(arg, "--state-file=")
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown dynamic describe option %q", arg)
			}
			if opts.source != "" {
				return opts, fmt.Errorf("unexpected dynamic describe argument %q", arg)
			}
			opts.source = arg
		}
	}
	opts.source = strings.TrimSpace(opts.source)
	if opts.source == "" {
		return opts, errors.New("dynamic describe requires <source>")
	}
	return opts, nil
}

func printDynamicDescribeHelp(stdout io.Writer) {
	fs := flag.NewFlagSet("dynamic describe", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.String("state-file", defaultStatePath(), "routerd state database file")
	fs.String("o", "table", "output format: table, json, yaml")
	fs.String("output", "table", "output format: table, json, yaml")
	printSubcommandHelp(fs,
		"Show detailed stored DynamicConfigPart generations for one source.",
		"routerctl dynamic describe cloudedge\n"+
			"routerctl dynamic describe cloudedge --state-file /var/lib/routerd/state.db -o yaml")
}

type dynamicListRow struct {
	Source      string    `json:"source" yaml:"source"`
	Generation  int64     `json:"generation" yaml:"generation"`
	Status      string    `json:"status" yaml:"status"`
	Resources   int       `json:"resources" yaml:"resources"`
	Directives  int       `json:"directives" yaml:"directives"`
	ActionPlans int       `json:"actionPlans" yaml:"actionPlans"`
	ExpiresAt   time.Time `json:"expiresAt" yaml:"expiresAt"`
}

type dynamicPartDetail struct {
	ID           int64                                  `json:"id" yaml:"id"`
	Source       string                                 `json:"source" yaml:"source"`
	Generation   int64                                  `json:"generation" yaml:"generation"`
	Status       string                                 `json:"status" yaml:"status"`
	StoredStatus string                                 `json:"storedStatus" yaml:"storedStatus"`
	ObservedAt   time.Time                              `json:"observedAt" yaml:"observedAt"`
	ExpiresAt    time.Time                              `json:"expiresAt" yaml:"expiresAt"`
	Digest       string                                 `json:"digest" yaml:"digest"`
	Resources    []api.Resource                         `json:"resources" yaml:"resources"`
	Directives   []dynamicconfig.DynamicConfigDirective `json:"directives" yaml:"directives"`
	ActionPlans  []dynamicconfig.ActionPlan             `json:"actionPlans,omitempty" yaml:"actionPlans,omitempty"`
	Error        string                                 `json:"error,omitempty" yaml:"error,omitempty"`
	CreatedAt    time.Time                              `json:"createdAt" yaml:"createdAt"`
	UpdatedAt    time.Time                              `json:"updatedAt" yaml:"updatedAt"`
}

func dynamicListRows(parts []routerstate.DynamicConfigPartRecord, now time.Time) ([]dynamicListRow, error) {
	rows := make([]dynamicListRow, 0, len(parts))
	for _, part := range parts {
		resources, err := countJSONArray(part.ResourcesJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d resources: %w", part.Source, part.Generation, err)
		}
		directives, err := countJSONArray(part.DirectivesJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d directives: %w", part.Source, part.Generation, err)
		}
		actionPlans, err := countJSONArray(part.ActionPlansJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d actionPlans: %w", part.Source, part.Generation, err)
		}
		rows = append(rows, dynamicListRow{
			Source:      part.Source,
			Generation:  part.Generation,
			Status:      part.EffectiveStatus(now),
			Resources:   resources,
			Directives:  directives,
			ActionPlans: actionPlans,
			ExpiresAt:   part.ExpiresAt,
		})
	}
	return rows, nil
}

func dynamicPartDetails(parts []routerstate.DynamicConfigPartRecord, now time.Time) ([]dynamicPartDetail, error) {
	details := make([]dynamicPartDetail, 0, len(parts))
	for _, part := range parts {
		resources, err := decodeDynamicResources(part.ResourcesJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d resources: %w", part.Source, part.Generation, err)
		}
		directives, err := decodeDynamicDirectives(part.DirectivesJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d directives: %w", part.Source, part.Generation, err)
		}
		actionPlans, err := decodeDynamicActionPlans(part.ActionPlansJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d actionPlans: %w", part.Source, part.Generation, err)
		}
		details = append(details, dynamicPartDetail{
			ID:           part.ID,
			Source:       part.Source,
			Generation:   part.Generation,
			Status:       part.EffectiveStatus(now),
			StoredStatus: part.Status,
			ObservedAt:   part.ObservedAt,
			ExpiresAt:    part.ExpiresAt,
			Digest:       part.Digest,
			Resources:    resources,
			Directives:   directives,
			ActionPlans:  actionPlans,
			Error:        part.Error,
			CreatedAt:    part.CreatedAt,
			UpdatedAt:    part.UpdatedAt,
		})
	}
	return details, nil
}

func dynamicPartsFromRecords(records []routerstate.DynamicConfigPartRecord) ([]dynamicconfig.DynamicConfigPart, error) {
	parts := make([]dynamicconfig.DynamicConfigPart, 0, len(records))
	for _, record := range records {
		resources, err := decodeDynamicResources(record.ResourcesJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d resources: %w", record.Source, record.Generation, err)
		}
		directives, err := decodeDynamicDirectives(record.DirectivesJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d directives: %w", record.Source, record.Generation, err)
		}
		parts = append(parts, dynamicconfig.DynamicConfigPart{
			TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
			Metadata: api.ObjectMeta{
				Name: fmt.Sprintf("%s-%d", record.Source, record.Generation),
			},
			Spec: dynamicconfig.DynamicConfigPartSpec{
				Source:     record.Source,
				Generation: record.Generation,
				ObservedAt: record.ObservedAt,
				ExpiresAt:  record.ExpiresAt,
				Digest:     record.Digest,
				Resources:  resources,
				Directives: directives,
			},
		})
	}
	return parts, nil
}

func countJSONArray(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return 0, err
	}
	return len(items), nil
}

func decodeDynamicResources(raw string) ([]api.Resource, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var resources []api.Resource
	if err := yaml.Unmarshal([]byte(raw), &resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func decodeDynamicDirectives(raw string) ([]dynamicconfig.DynamicConfigDirective, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var directives []dynamicconfig.DynamicConfigDirective
	if err := json.Unmarshal([]byte(raw), &directives); err != nil {
		return nil, err
	}
	return directives, nil
}

func decodeDynamicActionPlans(raw string) ([]dynamicconfig.ActionPlan, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var plans []dynamicconfig.ActionPlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil {
		return nil, err
	}
	return plans, nil
}

func writeDynamicListTable(stdout io.Writer, rows []dynamicListRow) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SOURCE\tGEN\tSTATUS\tRESOURCES\tDIRECTIVES\tACTIONPLANS\tEXPIRES")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%d\t%s\t%d\t%d\t%d\t%s\n",
			row.Source,
			row.Generation,
			row.Status,
			row.Resources,
			row.Directives,
			row.ActionPlans,
			formatDynamicTime(row.ExpiresAt),
		)
	}
	return w.Flush()
}

func writeDynamicDescribeTable(stdout io.Writer, details []dynamicPartDetail) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for i, detail := range details {
		if i > 0 {
			fmt.Fprintln(w, "")
		}
		fmt.Fprintf(w, "Source:\t%s\n", detail.Source)
		fmt.Fprintf(w, "Generation:\t%d\n", detail.Generation)
		fmt.Fprintf(w, "Status:\t%s\n", detail.Status)
		fmt.Fprintf(w, "Stored Status:\t%s\n", detail.StoredStatus)
		fmt.Fprintf(w, "Observed At:\t%s\n", formatDynamicTime(detail.ObservedAt))
		fmt.Fprintf(w, "Expires:\t%s\n", formatDynamicTime(detail.ExpiresAt))
		fmt.Fprintf(w, "Digest:\t%s\n", detail.Digest)
		if detail.Error != "" {
			fmt.Fprintf(w, "Error:\t%s\n", detail.Error)
		}
		fmt.Fprintln(w, "Resources:")
		if len(detail.Resources) == 0 {
			fmt.Fprintln(w, "  <none>")
		} else {
			for _, resource := range detail.Resources {
				fmt.Fprintf(w, "  - %s/%s\t%s\n", resource.APIVersion, resource.Kind, resource.Metadata.Name)
				if resource.Spec != nil {
					data, _ := json.Marshal(resource.Spec)
					fmt.Fprintf(w, "    spec:\t%s\n", string(data))
				}
			}
		}
		fmt.Fprintln(w, "Directives:")
		if len(detail.Directives) == 0 {
			fmt.Fprintln(w, "  <none>")
		} else {
			for _, directive := range detail.Directives {
				target := directive.Target.APIVersion + "/" + directive.Target.Kind + "/" + directive.Target.Name
				if directive.Reason == "" {
					fmt.Fprintf(w, "  - %s\t%s\n", directive.Op, target)
				} else {
					fmt.Fprintf(w, "  - %s\t%s\t%s\n", directive.Op, target, directive.Reason)
				}
			}
		}
		fmt.Fprintln(w, "Action Plans (dry-run / not executed):")
		if len(detail.ActionPlans) == 0 {
			fmt.Fprintln(w, "  <none>")
		} else {
			for _, plan := range detail.ActionPlans {
				mode := plan.Mode
				if mode == "" {
					mode = "dry-run"
				}
				fmt.Fprintf(w, "  - %s\tprovider=%s\taction=%s\tmode=%s\n", plan.Name, plan.Provider, plan.Action, mode)
				if plan.RiskLevel != "" {
					fmt.Fprintf(w, "    riskLevel:\t%s\n", plan.RiskLevel)
				}
				if addr := plan.Target["address"]; addr != "" {
					fmt.Fprintf(w, "    target.address:\t%s\n", addr)
				}
				if nic := plan.Target["nicRef"]; nic != "" {
					fmt.Fprintf(w, "    target.nicRef:\t%s\n", nic)
				}
				if len(plan.ExpectedEffects) > 0 {
					fmt.Fprintf(w, "    expectedEffects:\t%s\n", strings.Join(plan.ExpectedEffects, "; "))
				}
			}
		}
		fmt.Fprintf(w, "Created At:\t%s\n", formatDynamicTime(detail.CreatedAt))
		fmt.Fprintf(w, "Updated At:\t%s\n", formatDynamicTime(detail.UpdatedAt))
	}
	return w.Flush()
}

func writeDynamicDiff(stdout io.Writer, result dynamicconfig.EffectiveResult) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, added := range result.AddedResources {
		fmt.Fprintf(w, "+ %s/%s/%s\tsource=%s\tgeneration=%d\n",
			added.APIVersion,
			added.Kind,
			added.Name,
			added.Source,
			added.Generation,
		)
	}
	for _, suppressed := range result.Suppressed {
		fmt.Fprintf(w, "- %s/%s/%s\tmaskedBy=%s\tmaskedUntil=%s",
			suppressed.Target.APIVersion,
			suppressed.Target.Kind,
			suppressed.Target.Name,
			suppressed.MaskedBy,
			formatDynamicTime(suppressed.MaskedUntil),
		)
		if suppressed.Reason != "" {
			fmt.Fprintf(w, "\treason=%s", suppressed.Reason)
		}
		fmt.Fprintln(w)
	}
	if len(result.AddedResources) == 0 && len(result.Suppressed) == 0 {
		fmt.Fprintln(w, "no dynamic config changes")
	}
	return w.Flush()
}

func formatDynamicTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}

func dynamicUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl dynamic <subcommand> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "subcommands:")
	fmt.Fprintln(w, "  list [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  describe <source> [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  render [--config <path>] [--state-file <path>] [-o yaml|json]")
	fmt.Fprintln(w, "  diff [--config <path>] [--state-file <path>] [-o text|json]")
}
