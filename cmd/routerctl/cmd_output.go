// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/observe"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func writeShowTable(stdout io.Writer, rows []showResource, opts showOptions) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	switch {
	case opts.AdoptOnly:
		fmt.Fprintln(w, "KIND\tNAME\tADOPT")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%d candidates\n", row.Kind, row.Name, len(row.Adopt))
		}
	case opts.Diff:
		fmt.Fprintln(w, "KIND\tNAME\tDIFF")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%d fields\n", row.Kind, row.Name, len(row.Diff))
		}
	case opts.LedgerOnly:
		fmt.Fprintln(w, "KIND\tNAME\tLEDGER")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%d artifacts\n", row.Kind, row.Name, len(row.Ledger))
		}
	default:
		header := "KIND\tNAME\tSPEC\tOBSERVED\tLEDGER\tSTATE"
		if opts.Events {
			header += "\tEVENTS"
		}
		fmt.Fprintln(w, header)
		for _, row := range rows {
			line := fmt.Sprintf("%s\t%s\t%s\t%s\t%d artifacts\t%s",
				row.Kind,
				row.Name,
				specSummary(row.Spec),
				observedSummary(row.Observed),
				len(row.Ledger),
				stateSummary(row.State),
			)
			if opts.Events {
				line += fmt.Sprintf("\t%d events", len(row.Events))
			}
			fmt.Fprintln(w, line)
		}
	}
	return w.Flush()
}

func writeGetTable(stdout io.Writer, resources []api.Resource) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tNAME\tSPEC")
	for _, res := range resources {
		fmt.Fprintf(w, "%s\t%s\t%s\n", res.Kind, res.Metadata.Name, specSummary(res.Spec))
	}
	return w.Flush()
}

func writeGetKinds(stdout io.Writer, resources []api.Resource, output string) error {
	counts := map[string]int{}
	for _, res := range resources {
		counts[res.Kind]++
	}
	var kinds []string
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	type kindRow struct {
		Kind  string `json:"kind" yaml:"kind"`
		Count int    `json:"count" yaml:"count"`
	}
	var rows []kindRow
	for _, kind := range kinds {
		rows = append(rows, kindRow{Kind: kind, Count: counts[kind]})
	}
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KIND\tCOUNT")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%d\n", row.Kind, row.Count)
		}
		return w.Flush()
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeDescribe(stdout io.Writer, row showResource, store routerstate.Store) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", row.Name)
	fmt.Fprintf(w, "Kind:\t%s\n", row.Kind)
	fmt.Fprintf(w, "API Version:\t%s\n", row.APIVersion)
	if generationReader, ok := store.(routerstate.ObjectGenerationReader); ok {
		if generation := generationReader.ObjectGeneration(row.APIVersion, row.Kind, row.Name); generation != 0 {
			fmt.Fprintf(w, "Last Apply Generation:\t%d\n", generation)
		}
	}
	writeDescribeStatus(w, row)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Spec:")
	writeDescribeMap(w, row.Spec, "  ")
	fmt.Fprintln(w, "Observed:")
	writeDescribeMap(w, row.Observed, "  ")
	fmt.Fprintln(w, "Ledger:")
	if len(row.Ledger) == 0 {
		fmt.Fprintln(w, "  <none>")
	} else {
		for _, artifact := range row.Ledger {
			fmt.Fprintf(w, "  %s/%s\n", artifact.Kind, artifact.Name)
		}
	}
	fmt.Fprintln(w, "Events:")
	if len(row.Events) == 0 {
		fmt.Fprintln(w, "  <none>")
	} else {
		for _, event := range row.Events {
			fmt.Fprintf(w, "  %s\t%s\t%s\tgeneration=%d\t%s\n", event.CreatedAt.Format(time.RFC3339), event.Type, event.Reason, event.Generation, event.Message)
		}
	}
	return w.Flush()
}

func writeDescribeOutput(stdout io.Writer, row showResource, store routerstate.Store, output string) error {
	switch output {
	case "", "table":
		return writeDescribe(stdout, row, store)
	case "json":
		return writeJSON(stdout, row)
	case "yaml":
		return writeYAML(stdout, row)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeDescribeStatus(w io.Writer, row showResource) {
	if row.Kind == "Inventory" {
		fmt.Fprintf(w, "Currently observable:\t%s\n", yesNo(len(row.State) > 0))
		fmt.Fprintf(w, "OS:\t%s\n", displayCell(nestedString(row.State, "os", "goos")))
		fmt.Fprintf(w, "Kernel:\t%s %s\n", displayCell(nestedString(row.State, "os", "kernelName")), displayCell(nestedString(row.State, "os", "kernelRelease")))
		fmt.Fprintf(w, "Virtualization:\t%s\n", displayCell(nestedString(row.State, "virtualization", "type")))
		fmt.Fprintf(w, "Service Manager:\t%s\n", displayCell(stringValue(row.State["serviceManager"])))
		return
	}
	lease, ok := describePDLease(row.State)
	if ok {
		fmt.Fprintf(w, "Currently observable:\t%s\n", yesNo(lease.CurrentPrefix != ""))
		fmt.Fprintf(w, "Current delegated prefix:\t%s\n", displayCell(lease.CurrentPrefix))
		fmt.Fprintf(w, "Last delegated prefix:\t%s\n", displayCell(lease.LastPrefix))
		fmt.Fprintf(w, "Client DUID:\t%s\n", displayCell(firstNonEmpty(lease.DUIDText, lease.DUID)))
		fmt.Fprintf(w, "Expected DUID:\t%s\n", displayCell(lease.ExpectedDUID))
		fmt.Fprintf(w, "IAID:\t%s\n", displayCell(lease.IAID))
		fmt.Fprintf(w, "Last Reply at:\t%s\n", displayCell(lease.LastReplyAt))
		fmt.Fprintf(w, "Last observed at:\t%s\n", displayCell(lease.LastObservedAt))
		fmt.Fprintf(w, "Last Solicit at:\t%s\n", displayCell(lease.LastSolicitAt))
		fmt.Fprintf(w, "Last Request at:\t%s\n", displayCell(lease.LastRequestAt))
		fmt.Fprintf(w, "Last Renew at:\t%s\n", displayCell(lease.LastRenewAt))
		fmt.Fprintf(w, "Last Rebind at:\t%s\n", displayCell(lease.LastRebindAt))
		fmt.Fprintf(w, "Last Release at:\t%s\n", displayCell(lease.LastReleaseAt))
		fmt.Fprintf(w, "T1:\t%s\n", displayLeaseSeconds(lease.T1))
		fmt.Fprintf(w, "T2:\t%s\n", displayLeaseSeconds(lease.T2))
		fmt.Fprintf(w, "Preferred lifetime:\t%s\n", displayLeaseSeconds(lease.PLTime))
		fmt.Fprintf(w, "Valid lifetime:\t%s\n", displayLeaseSeconds(lease.VLTime))
		if timing := pdLeaseTiming(lease, time.Now().UTC()); len(timing) > 0 {
			fmt.Fprintf(w, "Next T1 at:\t%s\n", displayCell(timing["t1At"]))
			fmt.Fprintf(w, "Next T2 at:\t%s\n", displayCell(timing["t2At"]))
			fmt.Fprintf(w, "Valid lifetime expires at:\t%s\n", displayCell(timing["expiresAt"]))
			fmt.Fprintf(w, "Valid lifetime remaining:\t%s\n", displayCell(timing["remaining"]))
		}
		return
	}
	observable := false
	if exists, ok := row.Observed["exists"].(bool); ok {
		observable = exists
	} else if len(row.Observed) > 0 {
		observable = true
	}
	phase := strings.TrimSpace(stringValue(row.State["phase"]))
	if phase != "" {
		fmt.Fprintf(w, "Phase:\t%s\n", phase)
	}
	if reason := strings.TrimSpace(stringValue(row.State["reason"])); reason != "" {
		fmt.Fprintf(w, "Reason:\t%s\n", reason)
	}
	if message := strings.TrimSpace(stringValue(row.State["message"])); message != "" {
		fmt.Fprintf(w, "Message:\t%s\n", message)
	}
	if remediation := describePhaseRemediation(phase); remediation != "" {
		fmt.Fprintf(w, "Remediation:\t%s\n", remediation)
	}
	fmt.Fprintf(w, "Currently observable:\t%s\n", yesNo(observable))
	fmt.Fprintf(w, "Last observed at:\t-\n")
}

func describePhaseRemediation(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "drifted":
		return "run `routerd apply` to reconcile this resource"
	case "blocked":
		return "unmet dependency or conflict; check Events above and dependent resources"
	case "failing", "unhealthy", "error":
		return "check Events above and routerd logs"
	default:
		return ""
	}
}

func nestedString(values map[string]any, keys ...string) string {
	var current any = values
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	return stringValue(current)
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func describePDLease(state map[string]any) (routerstate.PDLease, bool) {
	if state == nil {
		return routerstate.PDLease{}, false
	}
	lease, ok := state["lease"].(routerstate.PDLease)
	return lease, ok
}

func pdLeaseTiming(lease routerstate.PDLease, now time.Time) map[string]string {
	base, ok := parseRFC3339Time(lease.LastReplyAt)
	if !ok {
		return nil
	}
	out := map[string]string{}
	if seconds, ok := parseLeaseSeconds(lease.T1); ok {
		out["t1At"] = base.Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339)
	}
	if seconds, ok := parseLeaseSeconds(lease.T2); ok {
		out["t2At"] = base.Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339)
	}
	if seconds, ok := parseLeaseSeconds(lease.VLTime); ok {
		expiresAt := base.Add(time.Duration(seconds) * time.Second).UTC()
		out["expiresAt"] = expiresAt.Format(time.RFC3339)
		if !now.IsZero() {
			remaining := expiresAt.Sub(now).Round(time.Second)
			if remaining <= 0 {
				out["remaining"] = "expired"
			} else {
				out["remaining"] = remaining.String()
			}
		}
	}
	return out
}

func parseRFC3339Time(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseLeaseSeconds(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return seconds, true
}

func displayLeaseSeconds(value string) string {
	seconds, ok := parseLeaseSeconds(value)
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%ds", seconds)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func writeDescribeMap(w io.Writer, value any, indent string) {
	values := flattenAny(value)
	if len(values) == 0 {
		fmt.Fprintln(w, indent+"<none>")
		return
	}
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(w, "%s%s:\t%v\n", indent, key, values[key])
	}
}

func specSummary(spec any) string {
	values := flattenAny(spec)
	if len(values) == 0 {
		return "-"
	}
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		parts = append(parts, key+"="+fmt.Sprint(values[key]))
		if len(parts) >= 3 {
			break
		}
	}
	return strings.Join(parts, ",")
}

func observedSummary(observed map[string]any) string {
	if len(observed) == 0 {
		return "-"
	}
	if exists, ok := observed["exists"]; ok {
		return "exists=" + fmt.Sprint(exists)
	}
	if hostname, ok := observed["hostname"]; ok {
		return "hostname=" + fmt.Sprint(hostname)
	}
	if addrs, ok := observed["addresses"]; ok {
		return "addresses=" + fmt.Sprint(addrs)
	}
	if connections, ok := observed["connections"]; ok {
		if table, ok := connections.(*observe.ConnectionTable); ok {
			return fmt.Sprintf("conntrack=%d", table.Count)
		}
	}
	if err, ok := observed["connectionsError"]; ok {
		return "error=" + fmt.Sprint(err)
	}
	return "observed"
}

func stateSummary(state map[string]any) string {
	if len(state) == 0 {
		return "-"
	}
	if leaseValue, ok := state["lease"]; ok {
		if lease, ok := leaseValue.(routerstate.PDLease); ok {
			return "current=" + displayCell(lease.CurrentPrefix) + ",last=" + displayCell(lease.LastPrefix)
		}
	}
	return fmt.Sprintf("%d values", len(state))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func displayCell(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func writeJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeYAML(stdout io.Writer, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	_, err = stdout.Write(data)
	return err
}
