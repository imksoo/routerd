// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/dhcpfingerprint"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/platform"
)

var commandContext = exec.CommandContext

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	defaults, _ := platform.Current()
	fs := flag.NewFlagSet("routerd-dhcp-fingerprint-watcher", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("db", strings.TrimRight(defaults.StateDir, "/")+"/dhcp-fingerprints.db", "SQLite fingerprint database path")
	unit := fs.String("journal-unit", "routerd-dnsmasq.service", "systemd journal unit to follow")
	inputFile := fs.String("input-file", "", "read dnsmasq log-dhcp lines from a file instead of journalctl")
	once := fs.Bool("once", false, "read finite input and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := logstore.OpenDHCPFingerprintLog(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if strings.TrimSpace(*inputFile) != "" {
		file, err := os.Open(*inputFile)
		if err != nil {
			return err
		}
		defer file.Close()
		return ingest(ctx, file, store)
	}
	journalArgs := []string{"-u", *unit, "-o", "cat"}
	if !*once {
		journalArgs = append(journalArgs, "-f")
	}
	processCtx, stopProcess := context.WithCancel(ctx)
	defer stopProcess()
	cmd := commandContext(processCtx, "journalctl", journalArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	ingestErr := ingest(ctx, stdout, store)
	// Ingest failure must release a still-following journal process before Wait.
	if ingestErr != nil {
		stopProcess()
	}
	waitErr := cmd.Wait()
	if ingestErr != nil {
		return ingestErr
	}
	if waitErr != nil && ctx.Err() == nil {
		return waitErr
	}
	return nil
}

func ingest(ctx context.Context, r io.Reader, store *logstore.DHCPFingerprintLog) error {
	scanner := bufio.NewScanner(r)
	ingester := newFingerprintIngester(store)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := ingester.line(ctx, scanner.Text(), time.Now().UTC()); err != nil {
			return err
		}
	}
	return scanner.Err()
}

const fingerprintTransactionLimit = 2048
const fingerprintTransactionTTL = 5 * time.Minute

type fingerprintTransaction struct {
	event     dhcpfingerprint.Event
	ambiguous bool
}

type fingerprintIngester struct {
	store        *logstore.DHCPFingerprintLog
	transactions map[string]fingerprintTransaction
}

func newFingerprintIngester(store *logstore.DHCPFingerprintLog) *fingerprintIngester {
	return &fingerprintIngester{store: store, transactions: make(map[string]fingerprintTransaction)}
}

func (i *fingerprintIngester) line(ctx context.Context, line string, now time.Time) error {
	event, ok := dhcpfingerprint.ParseDnsmasqLine(line, now)
	if !ok {
		return nil
	}
	for key, txn := range i.transactions {
		if now.Sub(txn.event.ObservedAt) >= fingerprintTransactionTTL {
			delete(i.transactions, key)
		}
	}
	if event.TransactionID != "" {
		txn := i.transactions[event.TransactionID]
		if txn.ambiguous {
			return nil
		}
		if event.MAC != "" && txn.event.MAC != "" && event.MAC != txn.event.MAC {
			// Colliding client xids do not provide a trustworthy metadata identity.
			txn.ambiguous = true
			txn.event.ObservedAt = now
			i.transactions[event.TransactionID] = txn
			return nil
		}
		event = mergeEvents(txn.event, event)
		if len(i.transactions) >= fingerprintTransactionLimit {
			var oldestKey string
			var oldest time.Time
			for key, candidate := range i.transactions {
				if oldestKey == "" || candidate.event.ObservedAt.Before(oldest) {
					oldestKey, oldest = key, candidate.event.ObservedAt
				}
			}
			delete(i.transactions, oldestKey)
		}
		i.transactions[event.TransactionID] = fingerprintTransaction{event: event}
	}
	// A metadata-only line without transaction identity cannot identify a client.
	if event.MAC == "" {
		return nil
	}
	rows, err := i.store.List(ctx, logstore.DHCPFingerprintFilter{MAC: event.MAC, Limit: 1})
	if err != nil {
		return err
	}
	fp := dhcpfingerprint.Fingerprint{MAC: event.MAC, ObservedAt: now.UTC(), Source: "dnsmasq-log-dhcp"}
	if len(rows) > 0 {
		fp.Hostname, fp.VendorClass = rows[0].Hostname, rows[0].VendorClass
		fp.RequestedOptions = rows[0].RequestedOptions
	}
	if event.Hostname != "" {
		fp.Hostname = event.Hostname
	}
	if event.VendorClass != "" {
		fp.VendorClass = event.VendorClass
	}
	if len(event.RequestedOptions) > 0 {
		fp.RequestedOptions = event.RequestedOptions
	}
	return i.persist(ctx, fp)
}

func (i *fingerprintIngester) persist(ctx context.Context, fp dhcpfingerprint.Fingerprint) error {
	match := dhcpfingerprint.Infer(fp)
	return i.store.Upsert(ctx, logstore.DHCPFingerprint{
		MAC:              fp.MAC,
		Hostname:         fp.Hostname,
		VendorClass:      fp.VendorClass,
		RequestedOptions: fp.RequestedOptions,
		OSFamily:         match.OSFamily,
		DeviceClass:      match.DeviceClass,
		DeviceName:       match.DeviceName,
		Confidence:       match.Confidence,
		Signal:           match.Signal,
		ObservedAt:       fp.ObservedAt,
		Source:           fp.Source,
	})
}

func mergeEvents(base, next dhcpfingerprint.Event) dhcpfingerprint.Event {
	if next.TransactionID != "" {
		base.TransactionID = next.TransactionID
	}
	if next.MAC != "" {
		base.MAC = next.MAC
	}
	if next.Interface != "" {
		base.Interface = next.Interface
	}
	if next.Hostname != "" {
		base.Hostname = next.Hostname
	}
	if next.VendorClass != "" {
		base.VendorClass = next.VendorClass
	}
	if len(next.RequestedOptions) > 0 {
		for _, option := range next.RequestedOptions {
			found := false
			for _, existing := range base.RequestedOptions {
				if existing == option {
					found = true
					break
				}
			}
			if !found {
				base.RequestedOptions = append(base.RequestedOptions, option)
			}
		}
	}
	if !next.ObservedAt.IsZero() {
		base.ObservedAt = next.ObservedAt
	}
	if next.Source != "" {
		base.Source = next.Source
	}
	return base
}
