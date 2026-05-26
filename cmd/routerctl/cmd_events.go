// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func eventsCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	limit := fs.Int("limit", 50, "maximum number of events")
	since := fs.Int64("since", 0, "show events with id greater than this value")
	topic := fs.String("topic", "", "event topic")
	resource := fs.String("resource", "", "resource filter as <kind>/<name>")
	kind := fs.String("kind", "", "legacy event kind filter")
	name := fs.String("name", "", "legacy event name filter")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := routerstate.Open(*statePath)
	if err != nil {
		return err
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	lister, ok := store.(routerstate.EventLister)
	if !ok {
		return fmt.Errorf("state file %s does not support event listing", *statePath)
	}
	events, err := lister.ListEvents(routerstate.EventQuery{
		Limit:    *limit,
		SinceID:  *since,
		Topic:    *topic,
		Kind:     *kind,
		Name:     *name,
		Resource: *resource,
	})
	if err != nil {
		return err
	}
	switch output {
	case "", "table":
		return writeEventsTable(stdout, events)
	case "json":
		return writeJSON(stdout, events)
	case "yaml":
		return writeYAML(stdout, events)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeEventsTable(stdout io.Writer, events []routerstate.StoredEvent) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTIME\tSEVERITY\tTOPIC\tRESOURCE\tREASON\tMESSAGE")
	for _, event := range events {
		resource := event.Kind + "/" + event.Name
		if event.ResourceKind != "" && event.ResourceName != "" {
			resource = event.ResourceKind + "/" + event.ResourceName
		}
		topic := firstNonEmpty(event.Topic, event.Type)
		severity := firstNonEmpty(event.Severity, event.Type)
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			event.ID,
			event.CreatedAt.Format(time.RFC3339),
			severity,
			topic,
			resource,
			event.Reason,
			event.Message,
		)
	}
	return w.Flush()
}
