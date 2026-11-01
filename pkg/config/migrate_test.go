// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"
)

func TestMigrateLegacyYAMLMovesInterfaceRoleToFirewallZone(t *testing.T) {
	input := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: eth0
        role: untrust
        managed: false
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallZone
      metadata:
        name: wan
      spec:
        role: untrust
        interfaces: [Interface/wan]
`)
	output, changes, err := MigrateLegacyYAML(input, "legacy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Resource != "Interface/wan" || changes[0].Field != "spec.role" {
		t.Fatalf("changes = %#v", changes)
	}
	if strings.Contains(string(output), "role: untrust\n        managed") {
		t.Fatalf("legacy role remained in output:\n%s", output)
	}
	if _, err := LoadBytes(output, "migrated.yaml"); err != nil {
		t.Fatalf("migrated YAML is not admitted: %v", err)
	}
}

func TestMigrateLegacyYAMLRejectsUnrepresentedInterfaceRole(t *testing.T) {
	input := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: eth0
        role: untrust
        managed: false
`)
	_, _, err := MigrateLegacyYAML(input, "legacy.yaml")
	if err == nil || !strings.Contains(err.Error(), "no matching FirewallZone") {
		t.Fatalf("error = %v, want missing FirewallZone rejection", err)
	}
}

func TestMigrateLegacyYAMLRenamesFirewallSourceCIDRsWithoutBroadening(t *testing.T) {
	input := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallRule
      metadata:
        name: restricted
      spec:
        fromZone: lan
        toZone: management
        sourceCIDRs: [192.0.2.0/24]
        action: accept
`)
	output, changes, err := MigrateLegacyYAML(input, "legacy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Resource != "FirewallRule/restricted" || changes[0].Field != "spec.sourceCIDRs" {
		t.Fatalf("changes = %#v", changes)
	}
	if strings.Contains(string(output), "sourceCIDRs") || !strings.Contains(string(output), "srcCIDRs: [192.0.2.0/24]") {
		t.Fatalf("source restriction was not preserved:\n%s", output)
	}
	if _, err := LoadBytes(output, "migrated.yaml"); err != nil {
		t.Fatalf("migrated YAML is not admitted: %v", err)
	}
}

func TestMigrateLegacyYAMLMovesOTLPFirewallLogsToTelemetry(t *testing.T) {
	input := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: observability.routerd.net/v1alpha1
      kind: Telemetry
      metadata:
        name: otlp
      spec:
        otlp: {endpoint: http://collector.example:4317}
        signals: [logs]
    - apiVersion: system.routerd.net/v1alpha1
      kind: LogSink
      metadata:
        name: otlp
      spec:
        type: otlp
        otlp: {telemetryRef: Telemetry/otlp}
    - apiVersion: firewall.routerd.net/v1alpha1
      kind: FirewallEventLog
      metadata:
        name: events
      spec:
        enabled: true
        sinks: [LogSink/otlp]
`)
	output, changes, err := MigrateLegacyYAML(input, "legacy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Resource != "LogSink/otlp" {
		t.Fatalf("changes = %#v", changes)
	}
	if strings.Contains(string(output), "LogSink/otlp") || strings.Contains(string(output), "type: otlp") {
		t.Fatalf("obsolete OTLP sink remained in output:\n%s", output)
	}
	if _, err := LoadBytes(output, "migrated.yaml"); err != nil {
		t.Fatalf("migrated YAML is not admitted: %v", err)
	}
}
