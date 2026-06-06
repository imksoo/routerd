// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
)

func statusCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"routerd の現在の status (resource phase / conditions など) を読み取り専用 socket 経由で取得する。",
			"routerctl status -o json\n"+
				"routerctl status -o yaml\n"+
				"routerctl status --show-errors\n"+
				"routerctl status --socket /run/routerd/status.sock")
	}
	socketPath := fs.String("socket", defaultStatusSocketPath(), "routerd read-only status Unix domain socket path")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	output := "json"
	jsonOutput := fs.Bool("json", false, "output JSON")
	showErrors := fs.Bool("show-errors", false, "in table output, list each controller's reconcile error history")
	fs.StringVar(&output, "o", output, "output format: json, yaml, table")
	fs.StringVar(&output, "output", output, "output format: json, yaml, table")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *jsonOutput {
		output = "json"
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	status, err := controlapi.NewUnixClient(*socketPath).Status(ctx)
	if err != nil {
		return err
	}
	switch output {
	case "", "json":
		return writeJSON(stdout, status)
	case "yaml":
		return writeYAML(stdout, status)
	case "table":
		return writeStatusTable(stdout, status, *showErrors)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeStatusTable(stdout io.Writer, status *controlapi.Status, showErrors bool) error {
	if status == nil {
		return nil
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "STATUS\t%s\tgeneration=%d resources=%d\n", strings.ToUpper(status.Status.Phase), status.Status.Generation, status.Status.ResourceCount)
	if len(status.Status.ResourcePhaseIssues) > 0 {
		fmt.Fprintln(w, "RESOURCE\tPHASE\tREASON\tMESSAGE")
		for _, item := range status.Status.ResourcePhaseIssues {
			fmt.Fprintf(w, "%s/%s\t%s\t%s\t%s\n",
				item.Kind,
				item.Name,
				displayCell(item.Phase),
				displayCell(item.Reason),
				displayCell(oneLine(item.Message)),
			)
		}
	}
	if len(status.Status.Controllers) == 0 {
		return w.Flush()
	}
	fmt.Fprintln(w, "CONTROLLER\tMODE\tCURRENT_ERROR\tLAST_SUCCESS\tLAST_ERROR_AT\tMAX_DURATION\tHISTORY")
	controllers := append([]controlapi.ControllerStatus(nil), status.Status.Controllers...)
	sort.Slice(controllers, func(i, j int) bool { return controllers[i].Name < controllers[j].Name })
	for _, controller := range controllers {
		fmt.Fprintf(w, "%s\t%s\t%t\t%s\t%s\t%s\t%d\n",
			controller.Name,
			displayCell(controller.Mode),
			controller.CurrentError,
			formatStatusTime(controller.LastSuccessTime),
			formatStatusTime(controller.LastErrorTime),
			formatMaxDuration(controller.MaxDuration, controller.MaxDurationAt),
			len(controller.ReconcileErrorHistory),
		)
	}
	if !showErrors {
		return w.Flush()
	}
	for _, controller := range controllers {
		if len(controller.ReconcileErrorHistory) == 0 {
			continue
		}
		fmt.Fprintf(w, "\nERRORS controller=%s entries=%d\n", controller.Name, len(controller.ReconcileErrorHistory))
		fmt.Fprintln(w, "STARTED\tDURATION\tTRIGGER\tRESOURCE\tERROR")
		for _, entry := range controller.ReconcileErrorHistory {
			resource := entry.ResourceKind
			if entry.ResourceName != "" {
				if resource != "" {
					resource = resource + "/" + entry.ResourceName
				} else {
					resource = entry.ResourceName
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				entry.StartedAt.Format(time.RFC3339),
				displayCell(entry.Duration),
				displayCell(entry.Trigger),
				displayCell(resource),
				displayCell(oneLine(entry.Error)),
			)
		}
	}
	return w.Flush()
}

func formatStatusTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func formatMaxDuration(duration string, recordedAt *time.Time) string {
	duration = strings.TrimSpace(duration)
	if duration == "" {
		return "-"
	}
	if recordedAt == nil || recordedAt.IsZero() {
		return duration
	}
	return duration + " @ " + recordedAt.Format(time.RFC3339)
}
