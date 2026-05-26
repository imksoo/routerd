// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/eventlog"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func rollbackCommand(args []string, stdout, _ io.Writer) (err error) {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	applyFlags := registerApplyFlags(fs, false, false)
	list := fs.Bool("list", false, "list stored config generations")
	to := fs.Int64("to", 0, "generation to re-apply")
	limit := fs.Int("limit", 20, "maximum generations to list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *list && *to != 0 {
		return errors.New("rollback accepts either --list or --to, not both")
	}
	if *to == 0 {
		return listRollbackGenerations(*applyFlags.StatePath, *limit, stdout)
	}
	if *to < 0 {
		return fmt.Errorf("invalid --to %d", *to)
	}
	if err := applyFlags.validateOverrides(); err != nil {
		return err
	}
	return rollbackToGeneration(*applyFlags.StatePath, *to, applyFlags, stdout)
}

func openRollbackState(path string) (*routerstate.SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultStatePath
	}
	store, err := routerstate.OpenSQLiteReadOnly(path)
	if err != nil {
		return nil, fmt.Errorf("open state database %s: %w", path, err)
	}
	return store, nil
}

func listRollbackGenerations(statePath string, limit int, stdout io.Writer) error {
	store, err := openRollbackState(statePath)
	if err != nil {
		return err
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

func rollbackToGeneration(statePath string, generation int64, applyFlags applyFlagValues, stdout io.Writer) (err error) {
	configYAML, err := rollbackGenerationConfig(statePath, generation)
	if err != nil {
		return err
	}
	configFile, cleanup, err := writeRollbackConfigTemp(configYAML)
	if err != nil {
		return err
	}
	defer cleanup()
	router, err := config.Load(configFile)
	if err != nil {
		return err
	}
	logger, err := eventlog.New(router)
	if err != nil {
		return err
	}
	defer closeLogger(logger, "rollback", &err)
	logger.Emit(eventlog.LevelInfo, "rollback", "routerd rollback command started", map[string]string{
		"generation": fmt.Sprintf("%d", generation),
		"dryRun":     fmt.Sprintf("%t", *applyFlags.DryRun),
	})
	opts := applyFlags.applyOptions(configFile)
	_, err = runApplyOnce(router, opts, stdout, logger)
	return err
}

func rollbackGenerationConfig(statePath string, generation int64) (string, error) {
	store, err := openRollbackState(statePath)
	if err != nil {
		return "", err
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

func writeRollbackConfigTemp(configYAML string) (string, func(), error) {
	file, err := os.CreateTemp("", "routerd-rollback-*.yaml")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(file.Name()) }
	if _, err := io.WriteString(file, configYAML); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return file.Name(), cleanup, nil
}

func formatGenerationTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func yesNo(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}
