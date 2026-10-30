// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func actionResolveCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("action resolve", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs, "Record an independently verified provider outcome; this does not call a provider.", "routerctl action resolve <id> --outcome succeeded --by <operator> --evidence <inspection>")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	outcome := fs.String("outcome", "", "confirmed outcome: succeeded, failed, rolledBack, rollbackFailed")
	by := fs.String("by", "", "operator identity")
	evidence := fs.String("evidence", "", "independent provider inspection confirming no operation remains in flight and the stated outcome")
	id, rest, err := splitActionID(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
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
		return fmt.Errorf("unexpected action resolve argument %q", fs.Arg(0))
	}
	if strings.TrimSpace(*by) == "" || strings.TrimSpace(*evidence) == "" || *outcome == "" {
		return fmt.Errorf("action resolve requires --outcome, --by and --evidence after independent provider inspection")
	}
	switch *outcome {
	case routerstate.ActionSucceeded, routerstate.ActionFailed, routerstate.ActionRolledBack, routerstate.ActionRollbackFailed:
	default:
		return fmt.Errorf("invalid confirmed outcome %q", *outcome)
	}
	store, err := routerstate.OpenSQLite(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Classify abandoned invocation owners first; this never steals a live
	// guard and never resets an unknown operation to executable approval.
	if _, err := store.RecoverInterruptedActionOperations(ctx, time.Time{}); err != nil {
		return err
	}
	if err := store.ResolveActionOutcome(ctx, id, *outcome, *by, *evidence, time.Time{}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "action %d outcome recorded as %s from operator evidence\n", id, *outcome)
	return err
}
