// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"strings"
	"testing"
)

// helpInvocation describes a single `routerctl <sub> --help` test case.
type helpInvocation struct {
	name           string
	args           []string
	mustContain    []string
	mustContainAny [][]string // each inner slice = at least one must appear
}

func TestSubcommandHelpRendersUsageFlagsExamples(t *testing.T) {
	cases := []helpInvocation{
		{
			name:        "dns-queries",
			args:        []string{"dns-queries", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl dns-queries", "-since", "-limit"},
		},
		{
			name:        "connections",
			args:        []string{"connections", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl connections", "-limit"},
		},
		{
			name:        "traffic-flows",
			args:        []string{"traffic-flows", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl traffic-flows", "-since", "-client"},
		},
		{
			name:        "firewall-logs",
			args:        []string{"firewall-logs", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl firewall-logs", "-action"},
		},
		{
			name:        "status",
			args:        []string{"status", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl status"},
		},
		{
			name:        "events",
			args:        []string{"events", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl events", "-limit", "-resource"},
		},
		{
			name:        "apply",
			args:        []string{"apply", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl apply", "-dry-run"},
		},
		{
			name:        "delete",
			args:        []string{"delete", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl delete", "<kind>/<name>"},
		},
		{
			name:        "set-log-level",
			args:        []string{"set-log-level", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl set-log-level", "debug"},
		},
		{
			name:        "restart-dns-resolver",
			args:        []string{"restart-dns-resolver", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl restart-dns-resolver"},
		},
		{
			name:        "ledger integrity-check",
			args:        []string{"ledger", "integrity-check", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "ledger integrity-check"},
		},
		{
			name:        "ledger vacuum",
			args:        []string{"ledger", "vacuum", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "ledger vacuum"},
		},
		{
			name:        "ledger backup",
			args:        []string{"ledger", "backup", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "ledger backup", "<dest>"},
		},
		{
			name:        "ledger prune-events",
			args:        []string{"ledger", "prune-events", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "ledger prune-events", "-older-than", "-dry-run"},
		},
		{
			name:        "firewall test",
			args:        []string{"firewall", "test", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "firewall test", "-from", "-to"},
		},
		{
			name:        "tailscale peers",
			args:        []string{"tailscale", "peers", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "tailscale peers"},
		},
		{
			name:        "wireguard list",
			args:        []string{"wireguard", "list", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "wireguard list"},
		},
		{
			name:        "diagnose egress",
			args:        []string{"diagnose", "egress", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "diagnose egress"},
		},
		{
			name:        "diagnose dns",
			args:        []string{"diagnose", "dns", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "diagnose dns"},
		},
		{
			name:        "diagnose lan-client",
			args:        []string{"diagnose", "lan-client", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "diagnose lan-client"},
		},
		{
			name:        "doctor",
			args:        []string{"doctor", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "doctor"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(tc.args, &stdout, &stderr); err != nil {
				t.Fatalf("%v: expected nil error for --help, got %v\nstdout=%q\nstderr=%q", tc.args, err, stdout.String(), stderr.String())
			}
			combined := stdout.String() + "\n" + stderr.String()
			for _, want := range tc.mustContain {
				if !strings.Contains(combined, want) {
					t.Errorf("%v: help output missing %q\n----stdout----\n%s\n----stderr----\n%s", tc.args, want, stdout.String(), stderr.String())
				}
			}
		})
	}
}

func TestDNSQueriesHelpMentionsRelativeTime(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"dns-queries", "--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "duration") {
		t.Errorf("dns-queries --help should mention duration / 相対時間 form, got:\n%s", combined)
	}
	if !strings.Contains(combined, "#36") {
		t.Errorf("dns-queries --help should mention issue #36 for absolute time, got:\n%s", combined)
	}
}

func TestLedgerBackupHelpDocumentsDestArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"ledger", "backup", "--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "<dest>") {
		t.Errorf("ledger backup --help should explicitly document <dest> positional argument, got:\n%s", combined)
	}
}
