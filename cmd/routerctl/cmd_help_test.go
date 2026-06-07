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
			name:        "get",
			args:        []string{"get", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl get", "status", "events", "connections", "dns-queries", "traffic-flows", "firewall-logs", "--limit", "--resource"},
		},
		{
			name:        "apply",
			args:        []string{"apply", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl apply", "-f", "-replace", "-no-reconcile"},
		},
		{
			name:        "delete",
			args:        []string{"delete", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl delete", "<kind>/<name>"},
		},
		{
			name:        "log-level",
			args:        []string{"log-level", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl log-level", "debug"},
		},
		{
			name:        "restart dns-resolver",
			args:        []string{"restart", "dns-resolver", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl restart dns-resolver"},
		},
		{
			name:        "ingress",
			args:        []string{"ingress", "--help"},
			mustContain: []string{"Usage:", "Examples:", "routerctl ingress drain", "routerctl ingress undrain"},
		},
		{
			name:        "ingress drain",
			args:        []string{"ingress", "drain", "--help"},
			mustContain: []string{"Usage:", "Examples:", "routerctl ingress drain", "routerctl ingress undrain"},
		},
		{
			name:        "vpn",
			args:        []string{"vpn", "--help"},
			mustContain: []string{"Usage:", "Examples:", "routerctl vpn wireguard", "routerctl vpn tailscale"},
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
			args:        []string{"vpn", "tailscale", "peers", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "vpn tailscale peers"},
		},
		{
			name:        "wireguard list",
			args:        []string{"vpn", "wireguard", "list", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "vpn wireguard list"},
		},
		{
			name:        "doctor",
			args:        []string{"doctor", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "doctor", "--probe"},
		},
		{
			name:        "describe",
			args:        []string{"describe", "--help"},
			mustContain: []string{"Usage:", "Flags:", "Examples:", "routerctl describe", "--events-limit"},
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

func TestGetHelpMentionsRuntimeSubjects(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"get", "--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	combined := stdout.String() + stderr.String()
	for _, want := range []string{"get events", "get connections", "get dns-queries", "get traffic-flows", "get firewall-logs", "--topic", "--resource"} {
		if !strings.Contains(combined, want) {
			t.Errorf("get --help should mention %s, got:\n%s", want, combined)
		}
	}
}

func TestTopLevelUsageListsDomainControlCommands(t *testing.T) {
	var stdout bytes.Buffer
	usage(&stdout)
	out := stdout.String()
	for _, want := range []string{
		"vpn wireguard list",
		"vpn tailscale peers",
		"ingress drain",
		"ingress undrain",
		"log-level",
		"restart dns-resolver",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage is missing %q:\n%s", want, out)
		}
	}
	for _, old := range []string{
		"\n  wireguard list",
		"\n  tailscale peers",
		"set-log-level",
		"restart-dns-resolver",
		"\n  drain ",
		"\n  undrain ",
	} {
		if strings.Contains(out, old) {
			t.Fatalf("usage still lists removed command %q:\n%s", old, out)
		}
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
