// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderFreeBSDUsesFreeBSDValidationTarget(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "carp.yaml")
	config := `apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: carp}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: lan}
      spec: {ifname: em0, managed: false, owner: external}
    - apiVersion: net.routerd.net/v1alpha1
      kind: VirtualAddress
      metadata: {name: vip}
      spec:
        family: ipv4
        interface: lan
        address: 198.18.232.100/32
        mode: vrrp
        vrrp: {virtualRouterID: 232, priority: 150}
`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	if err := renderFreeBSDCommand([]string{"--config", configPath}, &strings.Builder{}); err != nil {
		t.Fatalf("renderFreeBSDCommand rejected FreeBSD-valid CARP: %v", err)
	}
}
