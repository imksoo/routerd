// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func ledgerCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		ledgerUsage(stderr)
		return errors.New("ledger requires subcommand")
	}
	switch args[0] {
	case "integrity-check":
		return ledgerIntegrityCheckCommand(args[1:], stdout)
	case "vacuum":
		return ledgerVacuumCommand(args[1:], stdout)
	case "backup":
		return ledgerBackupCommand(args[1:], stdout)
	case "prune-events":
		return ledgerPruneEventsCommand(args[1:], stdout)
	case "help", "-h", "--help":
		ledgerUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown ledger subcommand %q", args[0])
	}
}

func ledgerIntegrityCheckCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ledger integrity-check", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"state ledger (SQLite) の PRAGMA integrity_check を実行する。",
			"routerctl ledger integrity-check\n"+
				"routerctl ledger integrity-check -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json")
	fs.StringVar(&output, "output", "table", "output format: table, json")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected integrity-check argument %q", fs.Arg(0))
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	result, err := store.IntegrityCheck()
	if err != nil {
		return err
	}
	report := ledgerIntegrityReport{Result: result}
	switch output {
	case "", "table":
		if err := writeLedgerIntegrityTable(stdout, report); err != nil {
			return err
		}
	case "json":
		if err := writeJSON(stdout, report); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
	if result != "ok" {
		return fmt.Errorf("ledger integrity check failed: %s", result)
	}
	return nil
}

func ledgerVacuumCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ledger vacuum", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"state ledger (SQLite) を VACUUM して断片化を解消する。",
			"routerctl ledger vacuum\n"+
				"routerctl ledger vacuum --state-file /var/lib/routerd/state.db")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected vacuum argument %q", fs.Arg(0))
	}
	before, err := fileSize(*statePath)
	if err != nil {
		return err
	}
	store, err := openLedgerState(*statePath)
	if err != nil {
		return err
	}
	if err := store.Vacuum(); err != nil {
		_ = store.Close()
		return err
	}
	if err := store.Close(); err != nil {
		return err
	}
	after, err := fileSize(*statePath)
	if err != nil {
		return err
	}
	return writeLedgerVacuumTable(stdout, ledgerVacuumReport{StateFile: *statePath, BeforeBytes: before, AfterBytes: after})
}

func ledgerBackupCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ledger backup", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"state ledger (SQLite) を <dest> へオンラインバックアップする。\n"+
				"位置引数: <dest> = バックアップ先のファイルパス (必須)",
			"routerctl ledger backup /var/backups/routerd-state.db\n"+
				"routerctl ledger backup --state-file /var/lib/routerd/state.db /tmp/state.db")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("ledger backup requires <dest-path>")
	}
	dest := fs.Arg(0)
	store, err := openLedgerState(*statePath)
	if err != nil {
		return err
	}
	if err := store.BackupTo(dest); err != nil {
		_ = store.Close()
		return err
	}
	if err := store.Close(); err != nil {
		return err
	}
	size, err := fileSize(dest)
	if err != nil {
		return err
	}
	return writeLedgerBackupTable(stdout, ledgerBackupReport{StateFile: *statePath, Destination: dest, Bytes: size})
}

func ledgerPruneEventsCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ledger prune-events", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"--older-than より古い event ledger レコードを削除する。\n"+
				"--older-than は duration 形式 (例: 24h, 720h, 30d)。\n"+
				"--dry-run を付けると削除はせずに件数だけ返す。",
			"routerctl ledger prune-events --older-than 720h --dry-run\n"+
				"routerctl ledger prune-events --older-than 30d\n"+
				"routerctl ledger prune-events --older-than 24h --state-file /var/lib/routerd/state.db")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	olderThan := fs.String("older-than", "", "delete events older than duration, for example 24h or 30d")
	dryRun := fs.Bool("dry-run", false, "count matching events without deleting")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected prune-events argument %q", fs.Arg(0))
	}
	if strings.TrimSpace(*olderThan) == "" {
		return errors.New("ledger prune-events requires --older-than <duration>")
	}
	duration, err := parseHumanDuration(*olderThan)
	if err != nil {
		return fmt.Errorf("invalid --older-than: %w", err)
	}
	if duration <= 0 {
		return errors.New("--older-than must be positive")
	}
	cutoff := time.Now().Add(-duration).UTC()
	store, err := openLedgerState(*statePath)
	if err != nil {
		return err
	}
	var count int64
	deleted := int64(0)
	if *dryRun {
		count, err = store.CountEventsOlderThan(cutoff)
	} else {
		count, err = store.PruneEventsOlderThan(cutoff)
		deleted = count
		if err == nil {
			err = recordPruneEventsAuditEvent(store, cutoff, deleted)
		}
	}
	if closeErr := store.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return writeLedgerPruneEventsTable(stdout, ledgerPruneEventsReport{StateFile: *statePath, Cutoff: cutoff, Matched: count, Deleted: deleted, DryRun: *dryRun})
}

func recordPruneEventsAuditEvent(store *routerstate.SQLiteStore, cutoff time.Time, deleted int64) error {
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerctl", Kind: "routerctl", Instance: "ledger"}, "routerd.ledger.events.pruned", daemonapi.SeverityInfo)
	event.Reason = "EventsPruned"
	event.Message = "ledger events pruned"
	event.Attributes = map[string]string{
		"cutoff":      cutoff.UTC().Format(time.RFC3339Nano),
		"deletedRows": strconv.FormatInt(deleted, 10),
		"dryRun":      "false",
	}
	if uid := os.Getuid(); uid >= 0 {
		event.Attributes["invokedBy"] = fmt.Sprintf("uid=%d,gid=%d", uid, os.Getgid())
	}
	_, err := store.RecordBusEvent(context.Background(), event)
	return err
}

type ledgerIntegrityReport struct {
	Result string `json:"result"`
}

type ledgerVacuumReport struct {
	StateFile   string `json:"stateFile"`
	BeforeBytes int64  `json:"beforeBytes"`
	AfterBytes  int64  `json:"afterBytes"`
}

type ledgerBackupReport struct {
	StateFile   string `json:"stateFile"`
	Destination string `json:"destination"`
	Bytes       int64  `json:"bytes"`
}

type ledgerPruneEventsReport struct {
	StateFile string    `json:"stateFile"`
	Cutoff    time.Time `json:"cutoff"`
	Matched   int64     `json:"matched"`
	Deleted   int64     `json:"deleted"`
	DryRun    bool      `json:"dryRun"`
}

func openLedgerState(path string) (*routerstate.SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultStatePath()
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("open state database %s: %w", path, err)
	}
	return store, nil
}

func openLedgerStateReadOnly(path string) (*routerstate.SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultStatePath()
	}
	store, err := routerstate.OpenSQLiteReadOnly(path)
	if err != nil {
		return nil, fmt.Errorf("open state database %s: %w", path, err)
	}
	return store, nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func writeLedgerIntegrityTable(stdout io.Writer, report ledgerIntegrityReport) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "RESULT")
	fmt.Fprintln(w, report.Result)
	return w.Flush()
}

func writeLedgerVacuumTable(stdout io.Writer, report ledgerVacuumReport) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATE FILE\tBEFORE BYTES\tAFTER BYTES")
	fmt.Fprintf(w, "%s\t%d\t%d\n", report.StateFile, report.BeforeBytes, report.AfterBytes)
	return w.Flush()
}

func writeLedgerBackupTable(stdout io.Writer, report ledgerBackupReport) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATE FILE\tDESTINATION\tBYTES")
	fmt.Fprintf(w, "%s\t%s\t%d\n", report.StateFile, report.Destination, report.Bytes)
	return w.Flush()
}

func writeLedgerPruneEventsTable(stdout io.Writer, report ledgerPruneEventsReport) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATE FILE\tCUTOFF\tMATCHED\tDELETED\tDRY RUN")
	fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%t\n", report.StateFile, report.Cutoff.Format(time.RFC3339), report.Matched, report.Deleted, report.DryRun)
	return w.Flush()
}

func ledgerUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl ledger <subcommand> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "subcommands:")
	fmt.Fprintln(w, "  integrity-check [--state-file <path>] [-o table|json]")
	fmt.Fprintln(w, "  vacuum [--state-file <path>]")
	fmt.Fprintln(w, "  backup <dest-path> [--state-file <path>]")
	fmt.Fprintln(w, "  prune-events --older-than <duration> [--state-file <path>] [--dry-run]")
}
