// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func writeEventsTable(stdout io.Writer, events []routerstate.StoredEvent) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTIME\tSEVERITY\tTOPIC\tRESOURCE\tREASON\tMESSAGE")
	for _, event := range events {
		resource := event.Kind + "/" + event.Name
		if event.ResourceKind != "" && event.ResourceName != "" {
			resource = event.ResourceKind + "/" + event.ResourceName
		}
		topic := stringutil.FirstNonEmpty(event.Topic, event.Type)
		severity := stringutil.FirstNonEmpty(event.Severity, event.Type)
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
