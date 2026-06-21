// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type mobilityPathRow struct {
	Router   string   `json:"router" yaml:"router"`
	Prefix   string   `json:"prefix" yaml:"prefix"`
	NextHops []string `json:"nextHops" yaml:"nextHops"`
}

type mobilityTrapRow struct {
	Source         string `json:"source" yaml:"source"`
	Action         string `json:"action" yaml:"action"`
	Provider       string `json:"provider,omitempty" yaml:"provider,omitempty"`
	ProviderRef    string `json:"providerRef,omitempty" yaml:"providerRef,omitempty"`
	NICRef         string `json:"nicRef,omitempty" yaml:"nicRef,omitempty"`
	Address        string `json:"address,omitempty" yaml:"address,omitempty"`
	IdempotencyKey string `json:"idempotencyKey" yaml:"idempotencyKey"`
}

type mobilityOwnerRow struct {
	Pool                  string `json:"pool" yaml:"pool"`
	Address               string `json:"address" yaml:"address"`
	State                 string `json:"state" yaml:"state"`
	Class                 string `json:"class" yaml:"class"`
	OwnerNode             string `json:"ownerNode,omitempty" yaml:"ownerNode,omitempty"`
	OwnerProviderRef      string `json:"ownerProviderRef,omitempty" yaml:"ownerProviderRef,omitempty"`
	OwnerNICRef           string `json:"ownerNICRef,omitempty" yaml:"ownerNICRef,omitempty"`
	OwnerResourceRef      string `json:"ownerResourceRef,omitempty" yaml:"ownerResourceRef,omitempty"`
	LocalEvidenceNode     string `json:"localEvidenceNode,omitempty" yaml:"localEvidenceNode,omitempty"`
	LocalEvidenceSource   string `json:"localEvidenceSource,omitempty" yaml:"localEvidenceSource,omitempty"`
	LocalEvidenceNICRef   string `json:"localEvidenceNICRef,omitempty" yaml:"localEvidenceNICRef,omitempty"`
	LocalEvidenceResource string `json:"localEvidenceResourceRef,omitempty" yaml:"localEvidenceResourceRef,omitempty"`
	AdvertiseOwnerNode    string `json:"advertiseOwnerNode,omitempty" yaml:"advertiseOwnerNode,omitempty"`
	SuppressionReason     string `json:"suppressionReason,omitempty" yaml:"suppressionReason,omitempty"`
	ConflictReason        string `json:"conflictReason,omitempty" yaml:"conflictReason,omitempty"`
}

type mobilityDistributionRow struct {
	Pool           string         `json:"pool" yaml:"pool"`
	Mode           string         `json:"mode" yaml:"mode"`
	Node           string         `json:"node" yaml:"node"`
	Captures       int            `json:"captures" yaml:"captures"`
	Target         int            `json:"target,omitempty" yaml:"target,omitempty"`
	TotalAssigned  int            `json:"totalAssigned,omitempty" yaml:"totalAssigned,omitempty"`
	ReasonCounts   map[string]int `json:"reasonCounts,omitempty" yaml:"reasonCounts,omitempty"`
	RebalancePhase string         `json:"rebalancePhase,omitempty" yaml:"rebalancePhase,omitempty"`
	RebalanceID    string         `json:"rebalanceID,omitempty" yaml:"rebalanceID,omitempty"`
	RequestedAt    string         `json:"requestedAt,omitempty" yaml:"requestedAt,omitempty"`
	RequestedBy    string         `json:"requestedBy,omitempty" yaml:"requestedBy,omitempty"`
}

type mobilityRebalanceResult struct {
	Pool        string `json:"pool" yaml:"pool"`
	RequestID   string `json:"requestID" yaml:"requestID"`
	Phase       string `json:"phase" yaml:"phase"`
	RequestedAt string `json:"requestedAt" yaml:"requestedAt"`
	RequestedBy string `json:"requestedBy" yaml:"requestedBy"`
	Reason      string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

func mobilityCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		mobilityUsage(stderr)
		return errors.New("mobility requires subcommand")
	}
	switch args[0] {
	case "paths":
		return mobilityPathsCommand(args[1:], stdout)
	case "traps":
		return mobilityTrapsCommand(args[1:], stdout)
	case "owners":
		return mobilityOwnersCommand(args[1:], stdout)
	case "distribution":
		return mobilityDistributionCommand(args[1:], stdout)
	case "rebalance-captures":
		return mobilityRebalanceCapturesCommand(args[1:], stdout)
	case "leases", "list", "ownership", "show":
		return fmt.Errorf("mobility %s was removed with BGP mobility; use `routerctl mobility owners`, `routerctl mobility paths`, or `routerctl mobility traps`", args[0])
	case "help", "-h", "--help":
		mobilityUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown mobility subcommand %q", args[0])
	}
}

func mobilityOwnersCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility owners", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show SAM control-plane owner table state from MobilityPool status.",
			"routerctl mobility owners\n"+
				"routerctl mobility owners --pool cloudedge --address 10.77.60.10/32 -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	poolFilter := fs.String("pool", "", "filter by MobilityPool name")
	addressFilter := fs.String("address", "", "filter by mobility address")
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
		return fmt.Errorf("unexpected mobility owners argument %q", fs.Arg(0))
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		return err
	}
	rows := mobilityOwnerRows(statuses, *poolFilter, *addressFilter)
	return writeMobilityOwners(stdout, rows, output)
}

func mobilityPathsCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility paths", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show BGP-installed mobility /32 next-hop state.",
			"routerctl mobility paths\n"+
				"routerctl mobility paths --prefix 10.77.60.10/32 -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	prefixFilter := fs.String("prefix", "", "filter by prefix")
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
		return fmt.Errorf("unexpected mobility paths argument %q", fs.Arg(0))
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		return err
	}
	rows := mobilityPathRows(statuses, *prefixFilter)
	return writeMobilityPaths(stdout, rows, output)
}

func mobilityTrapsCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility traps", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show provider trap ActionPlans generated by MobilityPool.",
			"routerctl mobility traps\n"+
				"routerctl mobility traps --address 10.77.60.10/32 -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	addressFilter := fs.String("address", "", "filter by captured address")
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
		return fmt.Errorf("unexpected mobility traps argument %q", fs.Arg(0))
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
	rows := mobilityTrapRows(parts, *addressFilter)
	return writeMobilityTraps(stdout, rows, output)
}

func mobilityUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl mobility <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  owners [--pool <name>] [--address <ipv4/32>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  distribution [--pool <name>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  rebalance-captures --pool <name> [--state-file <path>] [--by <name>] [--reason <text>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  paths [--prefix <prefix>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  traps [--address <ipv4/32>] [--state-file <path>] [-o table|json|yaml]")
}

func mobilityDistributionCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility distribution", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show SAM capture distribution counts from MobilityPool status.",
			"routerctl mobility distribution\n"+
				"routerctl mobility distribution --pool cloudedge -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	poolFilter := fs.String("pool", "", "filter by MobilityPool name")
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
		return fmt.Errorf("unexpected mobility distribution argument %q", fs.Arg(0))
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		return err
	}
	return writeMobilityDistribution(stdout, mobilityDistributionRows(statuses, *poolFilter), output)
}

func mobilityRebalanceCapturesCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility rebalance-captures", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Request one forced SAM capture rebalance for a MobilityPool.",
			"routerctl mobility rebalance-captures --pool cloudedge\n"+
				"routerctl mobility rebalance-captures --pool cloudedge --by operator --reason rejoin -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	poolName := fs.String("pool", "", "MobilityPool name")
	requestedBy := fs.String("by", "routerctl", "operator identity recorded in status")
	reason := fs.String("reason", "operator-forced-rebalance", "reason recorded in status")
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
		return fmt.Errorf("unexpected mobility rebalance-captures argument %q", fs.Arg(0))
	}
	pool := strings.TrimSpace(*poolName)
	if pool == "" {
		return errors.New("mobility rebalance-captures requires --pool")
	}
	store, err := openLedgerState(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", pool)
	if len(status) == 0 {
		return fmt.Errorf("MobilityPool/%s status not found", pool)
	}
	now := time.Now().UTC()
	requestID := fmt.Sprintf("rebalance-%s-%d", pool, now.UnixNano())
	next := map[string]any{}
	for key, value := range status {
		next[key] = value
	}
	by := strings.TrimSpace(*requestedBy)
	if by == "" {
		by = "routerctl"
	}
	why := strings.TrimSpace(*reason)
	next["captureRebalanceRequestID"] = requestID
	next["captureRebalanceRequestedAt"] = now.Format(time.RFC3339Nano)
	next["captureRebalanceRequestedBy"] = by
	next["captureRebalanceReason"] = why
	next["captureRebalancePhase"] = "Pending"
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", pool, next); err != nil {
		return err
	}
	result := mobilityRebalanceResult{Pool: pool, RequestID: requestID, Phase: "Pending", RequestedAt: now.Format(time.RFC3339Nano), RequestedBy: by, Reason: why}
	return writeMobilityRebalanceResult(stdout, result, output)
}

func mobilityOwnerRows(statuses []routerstate.ObjectStatus, poolFilter, addressFilter string) []mobilityOwnerRow {
	poolFilter = strings.TrimSpace(poolFilter)
	addressFilter = strings.TrimSpace(addressFilter)
	var rows []mobilityOwnerRow
	for _, status := range statuses {
		if status.APIVersion != api.MobilityAPIVersion || status.Kind != "MobilityPool" {
			continue
		}
		if poolFilter != "" && status.Name != poolFilter {
			continue
		}
		table := statusMaps(status.Status["ownershipResolverControlPlaneOwnerTable"])
		if len(table) == 0 {
			table = statusMaps(status.Status["ownershipResolverOwnerTable"])
		}
		for _, item := range table {
			address := stringStatus(item, "address")
			if addressFilter != "" && address != addressFilter {
				continue
			}
			rows = append(rows, mobilityOwnerRow{
				Pool:                  status.Name,
				Address:               address,
				State:                 firstNonEmpty(stringStatus(item, "state"), "Unknown"),
				Class:                 stringStatus(item, "class"),
				OwnerNode:             firstNonEmpty(stringStatus(item, "ownerNode"), stringStatus(item, "homeOwnerNode")),
				OwnerProviderRef:      stringStatus(item, "ownerProviderRef"),
				OwnerNICRef:           stringStatus(item, "ownerNICRef"),
				OwnerResourceRef:      stringStatus(item, "ownerResourceRef"),
				LocalEvidenceNode:     firstNonEmpty(stringStatus(item, "localEvidenceNode"), stringStatus(item, "localNode")),
				LocalEvidenceSource:   firstNonEmpty(stringStatus(item, "localEvidenceSource"), stringStatus(item, "localSource")),
				LocalEvidenceNICRef:   firstNonEmpty(stringStatus(item, "localEvidenceNICRef"), stringStatus(item, "localNICRef")),
				LocalEvidenceResource: firstNonEmpty(stringStatus(item, "localEvidenceResourceRef"), stringStatus(item, "localResourceRef")),
				AdvertiseOwnerNode:    stringStatus(item, "advertiseOwnerNode"),
				SuppressionReason:     stringStatus(item, "suppressionReason"),
				ConflictReason:        stringStatus(item, "conflictReason"),
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Pool == rows[j].Pool {
			return rows[i].Address < rows[j].Address
		}
		return rows[i].Pool < rows[j].Pool
	})
	return rows
}

func mobilityDistributionRows(statuses []routerstate.ObjectStatus, poolFilter string) []mobilityDistributionRow {
	poolFilter = strings.TrimSpace(poolFilter)
	var rows []mobilityDistributionRow
	for _, status := range statuses {
		if status.APIVersion != api.MobilityAPIVersion || status.Kind != "MobilityPool" {
			continue
		}
		if poolFilter != "" && status.Name != poolFilter {
			continue
		}
		counts := mobilityIntMap(status.Status["captureDistributionNodeCounts"])
		if len(counts) == 0 {
			continue
		}
		nodes := make([]string, 0, len(counts))
		for node := range counts {
			nodes = append(nodes, node)
		}
		sort.Strings(nodes)
		reasons := mobilityIntMap(status.Status["captureDistributionReasonCounts"])
		for _, node := range nodes {
			rows = append(rows, mobilityDistributionRow{
				Pool:           status.Name,
				Mode:           firstNonEmpty(stringStatus(status.Status, "captureDistributionMode"), "unknown"),
				Node:           node,
				Captures:       counts[node],
				Target:         mobilityInt(status.Status["captureDistributionTargetPerNode"]),
				TotalAssigned:  mobilityInt(status.Status["captureDistributionTotalAssigned"]),
				ReasonCounts:   reasons,
				RebalancePhase: stringStatus(status.Status, "captureRebalancePhase"),
				RebalanceID:    stringStatus(status.Status, "captureRebalanceRequestID"),
				RequestedAt:    stringStatus(status.Status, "captureRebalanceRequestedAt"),
				RequestedBy:    stringStatus(status.Status, "captureRebalanceRequestedBy"),
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Pool == rows[j].Pool {
			return rows[i].Node < rows[j].Node
		}
		return rows[i].Pool < rows[j].Pool
	})
	return rows
}

func mobilityPathRows(statuses []routerstate.ObjectStatus, prefixFilter string) []mobilityPathRow {
	prefixFilter = strings.TrimSpace(prefixFilter)
	var rows []mobilityPathRow
	for _, status := range statuses {
		if status.Kind != "BGPRouter" {
			continue
		}
		for prefix, nextHops := range mobilityInstalledNextHops(status.Status["installedNextHops"]) {
			if prefixFilter != "" && prefix != prefixFilter {
				continue
			}
			rows = append(rows, mobilityPathRow{Router: status.Name, Prefix: prefix, NextHops: nextHops})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Router == rows[j].Router {
			return rows[i].Prefix < rows[j].Prefix
		}
		return rows[i].Router < rows[j].Router
	})
	return rows
}

func writeMobilityDistribution(stdout io.Writer, rows []mobilityDistributionRow, output string) error {
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "POOL\tMODE\tNODE\tCAPTURES\tTARGET\tTOTAL\tREASONS\tREBALANCE")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
				row.Pool,
				displayCell(row.Mode),
				row.Node,
				row.Captures,
				row.Target,
				row.TotalAssigned,
				displayCell(mobilityReasonCountsString(row.ReasonCounts)),
				displayCell(firstNonEmpty(row.RebalancePhase, "Idle")),
			)
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

func writeMobilityRebalanceResult(stdout io.Writer, result mobilityRebalanceResult, output string) error {
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "POOL\tREQUEST_ID\tPHASE\tREQUESTED_AT\tREQUESTED_BY\tREASON")
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", result.Pool, result.RequestID, result.Phase, result.RequestedAt, displayCell(result.RequestedBy), displayCell(result.Reason))
		return w.Flush()
	case "json":
		return writeJSON(stdout, result)
	case "yaml":
		return writeYAML(stdout, result)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func mobilityInstalledNextHops(value any) map[string][]string {
	out := map[string][]string{}
	switch typed := value.(type) {
	case map[string][]string:
		for prefix, hops := range typed {
			out[strings.TrimSpace(prefix)] = cleanMobilityStrings(hops)
		}
	case map[string]any:
		for prefix, raw := range typed {
			out[strings.TrimSpace(prefix)] = cleanMobilityStrings(mobilityStringSlice(raw))
		}
	}
	return out
}

func mobilityTrapRows(parts []routerstate.DynamicConfigPartRecord, addressFilter string) []mobilityTrapRow {
	addressFilter = strings.TrimSpace(addressFilter)
	var rows []mobilityTrapRow
	for _, part := range parts {
		if strings.TrimSpace(part.ActionPlansJSON) == "" {
			continue
		}
		var plans []dynamicconfig.ActionPlan
		if err := json.Unmarshal([]byte(part.ActionPlansJSON), &plans); err != nil {
			continue
		}
		for _, plan := range plans {
			if !mobilityTrapAction(plan.Action) {
				continue
			}
			address := strings.TrimSpace(plan.Target["address"])
			if addressFilter != "" && address != addressFilter {
				continue
			}
			rows = append(rows, mobilityTrapRow{
				Source:         part.Source,
				Action:         plan.Action,
				Provider:       plan.Provider,
				ProviderRef:    firstNonEmpty(plan.ProviderRef, plan.Target["providerRef"]),
				NICRef:         plan.Target["nicRef"],
				Address:        address,
				IdempotencyKey: strings.TrimSpace(plan.IdempotencyKey),
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Source == rows[j].Source {
			if rows[i].Address == rows[j].Address {
				return rows[i].Action < rows[j].Action
			}
			return rows[i].Address < rows[j].Address
		}
		return rows[i].Source < rows[j].Source
	})
	return rows
}

func mobilityTrapAction(action string) bool {
	switch strings.TrimSpace(action) {
	case "assign-secondary-ip", "unassign-secondary-ip", "ensure-forwarding-enabled", "ensure-forwarding-disabled":
		return true
	default:
		return false
	}
}

func writeMobilityPaths(stdout io.Writer, rows []mobilityPathRow, output string) error {
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ROUTER\tPREFIX\tNEXT_HOPS")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\n", row.Router, row.Prefix, displayCell(strings.Join(row.NextHops, ",")))
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

func writeMobilityTraps(stdout io.Writer, rows []mobilityTrapRow, output string) error {
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SOURCE\tACTION\tPROVIDER\tPROVIDER_REF\tNIC\tADDRESS\tIDEMPOTENCY_KEY")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				row.Source,
				row.Action,
				displayCell(row.Provider),
				displayCell(row.ProviderRef),
				displayCell(row.NICRef),
				displayCell(row.Address),
				displayCell(row.IdempotencyKey),
			)
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

func writeMobilityOwners(stdout io.Writer, rows []mobilityOwnerRow, output string) error {
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "POOL\tADDRESS\tSTATE\tCLASS\tOWNER\tOWNER_PROVIDER\tOWNER_NIC\tLOCAL_EVIDENCE\tLOCAL_SOURCE\tADVERTISE\tSUPPRESSION\tCONFLICT")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				row.Pool,
				row.Address,
				displayCell(row.State),
				displayCell(row.Class),
				displayCell(row.OwnerNode),
				displayCell(row.OwnerProviderRef),
				displayCell(row.OwnerNICRef),
				displayCell(row.LocalEvidenceNode),
				displayCell(row.LocalEvidenceSource),
				displayCell(row.AdvertiseOwnerNode),
				displayCell(row.SuppressionReason),
				displayCell(row.ConflictReason),
			)
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

func mobilityStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" {
				out = append(out, value)
			}
		}
		return out
	default:
		if value := strings.TrimSpace(fmt.Sprint(value)); value != "" && value != "<nil>" {
			return []string{value}
		}
	}
	return nil
}

func cleanMobilityStrings(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = true
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func mobilityIntMap(value any) map[string]int {
	out := map[string]int{}
	switch typed := value.(type) {
	case map[string]int:
		for key, value := range typed {
			if key = strings.TrimSpace(key); key != "" {
				out[key] = value
			}
		}
	case map[string]any:
		for key, raw := range typed {
			if key = strings.TrimSpace(key); key != "" {
				out[key] = mobilityInt(raw)
			}
		}
	}
	return out
}

func mobilityInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if n, err := typed.Int64(); err == nil {
			return int(n)
		}
	default:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(fmt.Sprint(value)), "%d", &n); err == nil {
			return n
		}
	}
	return 0
}

func mobilityReasonCountsString(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
}
