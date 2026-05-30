// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/federation"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func federationCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		federationUsage(stderr)
		return errors.New("federation requires subcommand")
	}
	switch args[0] {
	case "event":
		return federationEventCommand(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		federationUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown federation subcommand %q", args[0])
	}
}

func federationEventCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		federationUsage(stderr)
		return errors.New("federation event requires subcommand")
	}
	switch args[0] {
	case "emit":
		return federationEventEmitCommand(args[1:], stdout)
	case "list":
		return federationEventListCommand(args[1:], stdout)
	case "deliveries":
		return federationEventDeliveriesCommand(args[1:], stdout)
	case "help", "-h", "--help":
		federationUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown federation event subcommand %q", args[0])
	}
}

// payloadFlag collects repeated --payload key=value pairs.
type payloadFlag map[string]string

func (p payloadFlag) String() string {
	pairs := make([]string, 0, len(p))
	for k, v := range p {
		pairs = append(pairs, k+"="+v)
	}
	return strings.Join(pairs, ",")
}

func (p payloadFlag) Set(value string) error {
	key, val, ok := strings.Cut(value, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return fmt.Errorf("payload must be key=value, got %q", value)
	}
	p[key] = val
	return nil
}

func federationEventEmitCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("federation event emit", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Emit a CloudEdge federation event (observed fact) into the local store (ADR 0006).\n"+
				"This records the event locally only; Phase 2 peer delivery is separate.",
			"routerctl federation event emit --group cloudedge --type routerd.client.ipv4.observed \\\n"+
				"  --subject 10.88.60.9/32 --source-node onprem --payload mac=aa:bb:cc:dd:ee:ff --ttl 30m\n"+
				"routerctl fed event emit --group cloudedge --type routerd.client.ipv4.expired --subject 10.88.60.9/32")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	group := fs.String("group", "", "EventGroup (bus) name (required)")
	eventType := fs.String("type", "", "typed event topic, e.g. routerd.client.ipv4.observed (required)")
	subject := fs.String("subject", "", "entity the event is about, e.g. 10.88.60.9/32")
	sourceNode := fs.String("source-node", "", "emitting node identity")
	id := fs.String("id", "", "idempotency key (auto-generated when empty)")
	dedupeKey := fs.String("dedupe-key", "", "stable grouping key (defaults to id)")
	payload := payloadFlag{}
	fs.Var(payload, "payload", "additional attribute as key=value (repeatable)")
	ttl := fs.Duration("ttl", 0, "time-to-live; sets expiresAt = now + ttl when > 0")
	observedAt := fs.String("observed-at", "", "RFC3339 observation time (defaults to now)")
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
		return fmt.Errorf("unexpected federation event emit argument %q", fs.Arg(0))
	}
	if strings.TrimSpace(*group) == "" {
		return errors.New("--group is required")
	}
	if strings.TrimSpace(*eventType) == "" {
		return errors.New("--type is required")
	}

	now := time.Now().UTC()
	observed := now
	if strings.TrimSpace(*observedAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*observedAt))
		if err != nil {
			return fmt.Errorf("invalid --observed-at: %w", err)
		}
		observed = parsed.UTC()
	}

	eventID := strings.TrimSpace(*id)
	if eventID == "" {
		eventID = generateEventID()
	}

	ev := federation.Event{
		ID:         eventID,
		Group:      *group,
		SourceNode: *sourceNode,
		Type:       *eventType,
		Subject:    *subject,
		DedupeKey:  *dedupeKey,
		Payload:    map[string]string(payload),
		ObservedAt: observed,
	}
	if *ttl > 0 {
		ev.ExpiresAt = observed.Add(*ttl)
	}
	if err := ev.Normalize(); err != nil {
		return err
	}

	store, err := routerstate.Open(*statePath)
	if err != nil {
		return err
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	fedStore, ok := store.(routerstate.FederationEventStore)
	if !ok {
		return fmt.Errorf("state file %s does not support federation events", *statePath)
	}

	rec := routerstate.EventRecord{
		ID:         ev.ID,
		Group:      ev.Group,
		SourceNode: ev.SourceNode,
		Type:       ev.Type,
		Subject:    ev.Subject,
		DedupeKey:  ev.DedupeKey,
		Payload:    ev.Payload,
		ObservedAt: ev.ObservedAt,
		ExpiresAt:  ev.ExpiresAt,
	}
	if err := fedStore.RecordFederationEvent(rec); err != nil {
		return err
	}

	stored, err := fedStore.ListFederationEvents(ev.Group, true, now.Unix())
	if err != nil {
		return err
	}
	emitted := rec
	for _, candidate := range stored {
		if candidate.ID == ev.ID {
			emitted = candidate
			break
		}
	}
	switch output {
	case "", "table":
		return writeFederationEventsTable(stdout, []routerstate.EventRecord{emitted})
	case "json":
		return writeJSON(stdout, emitted)
	case "yaml":
		return writeYAML(stdout, emitted)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func federationEventListCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("federation event list", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"List CloudEdge federation events from the local store (ADR 0006).",
			"routerctl federation event list\n"+
				"routerctl fed event list --group cloudedge --include-expired -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	group := fs.String("group", "", "filter to a single EventGroup (bus) name")
	includeExpired := fs.Bool("include-expired", false, "include events past their expiresAt")
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
		return fmt.Errorf("unexpected federation event list argument %q", fs.Arg(0))
	}

	store, err := routerstate.Open(*statePath)
	if err != nil {
		return err
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	fedStore, ok := store.(routerstate.FederationEventStore)
	if !ok {
		return fmt.Errorf("state file %s does not support federation events", *statePath)
	}
	events, err := fedStore.ListFederationEvents(*group, *includeExpired, time.Now().Unix())
	if err != nil {
		return err
	}
	switch output {
	case "", "table":
		return writeFederationEventsTable(stdout, events)
	case "json":
		return writeJSON(stdout, events)
	case "yaml":
		return writeYAML(stdout, events)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func federationEventDeliveriesCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("federation event deliveries", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"List CloudEdge federation peer delivery attempts from the local store (ADR 0006, Phase 2).",
			"routerctl federation event deliveries\n"+
				"routerctl fed event deliveries --event-id evt-123 --peer cloud-a -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	eventID := fs.String("event-id", "", "filter to a single event id")
	peer := fs.String("peer", "", "filter to a single peer node name")
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
		return fmt.Errorf("unexpected federation event deliveries argument %q", fs.Arg(0))
	}

	store, err := routerstate.Open(*statePath)
	if err != nil {
		return err
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	deliveryStore, ok := store.(routerstate.FederationDeliveryStore)
	if !ok {
		return fmt.Errorf("state file %s does not support federation deliveries", *statePath)
	}
	deliveries, err := deliveryStore.ListDeliveries(*eventID, *peer)
	if err != nil {
		return err
	}
	switch output {
	case "", "table":
		return writeFederationDeliveriesTable(stdout, deliveries)
	case "json":
		return writeJSON(stdout, deliveries)
	case "yaml":
		return writeYAML(stdout, deliveries)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeFederationDeliveriesTable(stdout io.Writer, deliveries []routerstate.DeliveryRecord) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "EVENT_ID\tPEER\tSTATUS\tATTEMPTS\tLAST_ATTEMPT\tLAST_ERROR\tDELIVERED")
	for _, d := range deliveries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			d.EventID,
			d.Peer,
			d.Status,
			d.Attempts,
			formatDynamicTime(d.LastAttemptAt),
			displayCell(d.LastError),
			formatDynamicTime(d.DeliveredAt),
		)
	}
	return w.Flush()
}

func writeFederationEventsTable(stdout io.Writer, events []routerstate.EventRecord) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tGROUP\tSOURCE\tTYPE\tSUBJECT\tOBSERVED\tEXPIRES")
	for _, event := range events {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			event.ID,
			event.Group,
			displayCell(event.SourceNode),
			event.Type,
			displayCell(event.Subject),
			formatDynamicTime(event.ObservedAt),
			formatDynamicTime(event.ExpiresAt),
		)
	}
	return w.Flush()
}

// generateEventID builds a unique-enough idempotency key for CLI-emitted events.
func generateEventID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("evt-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("evt-%d-%s", time.Now().UnixNano(), hex.EncodeToString(buf[:]))
}

func federationUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl federation <subcommand> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "subcommands:")
	fmt.Fprintln(w, "  event emit --group <name> --type <topic> [--subject <s>] [--source-node <n>] [--id <id>] [--dedupe-key <k>] [--payload k=v ...] [--ttl <dur>] [--observed-at <rfc3339>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  event list [--group <name>] [--include-expired] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  event deliveries [--event-id <id>] [--peer <name>] [--state-file <path>] [-o table|json|yaml]")
}
