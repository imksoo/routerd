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

	"routerd/pkg/dhcpfingerprint"
	"routerd/pkg/logstore"
	"routerd/pkg/platform"
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
	cmd := commandContext(ctx, "journalctl", journalArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	ingestErr := ingest(ctx, stdout, store)
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

type fingerprintIngester struct {
	store        *logstore.DHCPFingerprintLog
	pending      dhcpfingerprint.Event
	currentMAC   string
	macByIface   map[string]string
	fingerprints map[string]dhcpfingerprint.Fingerprint
}

func newFingerprintIngester(store *logstore.DHCPFingerprintLog) *fingerprintIngester {
	return &fingerprintIngester{
		store:        store,
		macByIface:   map[string]string{},
		fingerprints: map[string]dhcpfingerprint.Fingerprint{},
	}
}

func (i *fingerprintIngester) line(ctx context.Context, line string, now time.Time) error {
	event, ok := dhcpfingerprint.ParseDnsmasqLine(line, now)
	if !ok {
		return nil
	}
	mac := event.MAC
	if mac == "" && event.Interface != "" {
		mac = i.macByIface[event.Interface]
	}
	if mac == "" {
		mac = i.currentMAC
	}
	if mac == "" {
		i.pending = mergeEvents(i.pending, event)
		return nil
	}
	if event.MAC != "" {
		i.currentMAC = event.MAC
		if event.Interface != "" {
			i.macByIface[event.Interface] = event.MAC
		}
		event = mergeEvents(i.pending, event)
		i.pending = dhcpfingerprint.Event{}
	}
	fp := i.fingerprints[mac]
	fp.MAC = mac
	fp.ObservedAt = now.UTC()
	fp.Source = "dnsmasq-log-dhcp"
	if event.Hostname != "" {
		fp.Hostname = event.Hostname
	}
	if event.VendorClass != "" {
		fp.VendorClass = event.VendorClass
	}
	if len(event.RequestedOptions) > 0 {
		fp.RequestedOptions = event.RequestedOptions
	}
	i.fingerprints[mac] = fp
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
		base.RequestedOptions = next.RequestedOptions
	}
	if !next.ObservedAt.IsZero() {
		base.ObservedAt = next.ObservedAt
	}
	if next.Source != "" {
		base.Source = next.Source
	}
	return base
}
