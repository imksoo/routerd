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
