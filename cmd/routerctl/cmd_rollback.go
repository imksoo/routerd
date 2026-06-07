// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func rollbackCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	fs.SetOutput(stdout)
	list := fs.Bool("list", false, "list stored config generations")
	to := fs.Int64("to", 0, "generation to restore")
	limit := fs.Int("limit", 20, "maximum generations to list")
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	noReconcile := fs.Bool("no-reconcile", false, "write canonical config without immediate reconcile")
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"保存済み generation を一覧または canonical config へ restore する。",
			"routerctl rollback --list\n"+
				"routerctl rollback --to 12\n"+
				"routerctl rollback --to 12 --no-reconcile")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *list && *to != 0 {
		return errors.New("rollback accepts either --list or --to, not both")
	}
	if *to == 0 {
		return listRollbackGenerations(*statePath, *limit, stdout)
	}
	if *to < 0 {
		return fmt.Errorf("invalid --to %d", *to)
	}
	configYAML, err := rollbackGenerationConfig(*statePath, *to)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).Apply(ctx, controlapi.ApplyRequest{
		CandidateYAML: configYAML,
		Replace:       true,
		NoReconcile:   *noReconcile,
	})
	if err != nil {
		return fmt.Errorf("routerd serve is not reachable for rollback; start routerd serve or check --socket: %w", err)
	}
	return writeJSON(stdout, result)
}

func listRollbackGenerations(statePath string, limit int, stdout io.Writer) error {
	store, err := routerstate.OpenSQLiteReadOnly(strings.TrimSpace(defaultString(statePath, defaultStatePath())))
	if err != nil {
		return fmt.Errorf("open state database %s: %w", statePath, err)
	}
	defer func() { _ = store.Close() }()
	records, err := store.ListGenerations(limit)
	if err != nil {
		return err
	}
	current := store.LatestGeneration()
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "generation\tstarted_at\tfinished_at\tphase\tconfig\tcurrent")
	for _, rec := range records {
		currentMark := ""
		if rec.Generation == current {
			currentMark = "(current)"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			rec.Generation,
			formatGenerationTime(rec.StartedAt),
			formatGenerationTime(rec.FinishedAt),
			defaultString(rec.Phase, "-"),
			yesNo(rec.HasYAML),
			currentMark,
		)
	}
	return tw.Flush()
}

func rollbackGenerationConfig(statePath string, generation int64) (string, error) {
	store, err := routerstate.OpenSQLiteReadOnly(strings.TrimSpace(defaultString(statePath, defaultStatePath())))
	if err != nil {
		return "", fmt.Errorf("open state database %s: %w", statePath, err)
	}
	defer func() { _ = store.Close() }()
	latest := store.LatestGeneration()
	if generation <= 0 || generation > latest {
		return "", fmt.Errorf("generation %d not found", generation)
	}
	configYAML, ok, err := store.GenerationConfig(generation)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("generation %d has no saved config", generation)
	}
	return configYAML, nil
}

func formatGenerationTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}
