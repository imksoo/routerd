// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"text/tabwriter"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func mobilityCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		mobilityUsage(stderr)
		return errors.New("mobility requires subcommand")
	}
	switch args[0] {
	case "leases", "list":
		return mobilityLeasesCommand(args[1:], stdout)
	case "ownership", "owners":
		return mobilityOwnershipCommand(args[1:], stdout)
	case "show":
		return mobilityShowCommand(args[1:], stdout)
	case "help", "-h", "--help":
		mobilityUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown mobility subcommand %q", args[0])
	}
}

func mobilityLeasesCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility leases", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show AddressLease records projected from MobilityPool federation events.",
			"routerctl mobility leases --pool cloudedge\n"+
				"routerctl mobility leases --include-expired -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	pool := fs.String("pool", "", "MobilityPool name")
	includeExpired := fs.Bool("include-expired", false, "include expired leases")
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
		return fmt.Errorf("unexpected mobility leases argument %q", fs.Arg(0))
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	leases, err := store.ListAddressLeases(*pool, *includeExpired, time.Now().UTC())
	if err != nil {
		return err
	}
	return writeMobilityLeases(stdout, leases, output)
}

func mobilityOwnershipCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility ownership", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show MobilityPool deterministic address ownership epochs.",
			"routerctl mobility ownership --pool cloudedge\n"+
				"routerctl mobility ownership -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	pool := fs.String("pool", "", "MobilityPool name")
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
		return fmt.Errorf("unexpected mobility ownership argument %q", fs.Arg(0))
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	records, err := store.ListMobilityOwnershipEpochs(*pool)
	if err != nil {
		return err
	}
	return writeMobilityOwnership(stdout, records, output)
}

func mobilityShowCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility show", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show one AddressLease projected from MobilityPool federation events.",
			"routerctl mobility show --pool cloudedge --address 10.88.60.9/32\n"+
				"routerctl mobility show cloudedge 10.88.60.9/32 -o yaml")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	pool := fs.String("pool", "", "MobilityPool name")
	address := fs.String("address", "", "IPv4 address or /32")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		if strings.TrimSpace(*pool) == "" {
			*pool = fs.Arg(0)
		} else if strings.TrimSpace(*address) == "" {
			*address = fs.Arg(0)
		} else {
			return fmt.Errorf("unexpected mobility show argument %q", fs.Arg(0))
		}
	}
	if fs.NArg() > 1 {
		if strings.TrimSpace(*address) == "" {
			*address = fs.Arg(1)
		} else {
			return fmt.Errorf("unexpected mobility show argument %q", fs.Arg(1))
		}
	}
	if fs.NArg() > 2 {
		return fmt.Errorf("unexpected mobility show argument %q", fs.Arg(2))
	}
	if strings.TrimSpace(*pool) == "" {
		return fmt.Errorf("--pool is required")
	}
	canonicalAddress, err := canonicalLeaseAddress(*address)
	if err != nil {
		return err
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	lease, found, err := store.GetAddressLease(*pool, canonicalAddress)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("address lease %s/%s not found", *pool, canonicalAddress)
	}
	return writeMobilityLeases(stdout, []routerstate.AddressLeaseRecord{lease}, output)
}

func mobilityUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl mobility <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  leases [--pool <name>] [--include-expired] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  ownership [--pool <name>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  show --pool <name> --address <ipv4/32> [--state-file <path>] [-o table|json|yaml]")
}

func writeMobilityLeases(stdout io.Writer, leases []routerstate.AddressLeaseRecord, output string) error {
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "POOL\tADDRESS\tSTATUS\tOWNER\tSITE\tROLE\tEPOCH\tEXPIRES\tCANDIDATE")
		for _, lease := range leases {
			candidate := "-"
			if strings.TrimSpace(lease.CandidateOwnerNode) != "" {
				candidate = lease.CandidateOwnerNode
				if !lease.CandidateObservedAt.IsZero() {
					candidate += "@" + lease.CandidateObservedAt.Format(time.RFC3339)
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
				lease.Pool,
				lease.Address,
				lease.Status,
				displayCell(lease.OwnerNode),
				displayCell(lease.OwnerSite),
				displayCell(lease.OwnerRole),
				lease.Epoch,
				displayTime(lease.ExpiresAt),
				candidate,
			)
		}
		return w.Flush()
	case "json":
		return writeJSON(stdout, leases)
	case "yaml":
		return writeYAML(stdout, leases)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeMobilityOwnership(stdout io.Writer, records []routerstate.MobilityOwnershipEpochRecord, output string) error {
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "POOL\tADDRESS\tOWNER\tOWNERSHIP_EPOCH\tUPDATED")
		for _, rec := range records {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
				rec.Pool,
				rec.Address,
				displayCell(rec.OwnerNode),
				rec.Epoch,
				displayTime(rec.UpdatedAt),
			)
		}
		return w.Flush()
	case "json":
		return writeJSON(stdout, records)
	case "yaml":
		return writeYAML(stdout, records)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func canonicalLeaseAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("--address is required")
	}
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		prefix = prefix.Masked()
		if !prefix.Addr().Is4() || prefix.Bits() != 32 {
			return "", fmt.Errorf("address must be an IPv4 /32")
		}
		return prefix.String(), nil
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil || !addr.Is4() {
		return "", fmt.Errorf("address must be an IPv4 address or /32")
	}
	return netip.PrefixFrom(addr, 32).String(), nil
}

func displayTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}
