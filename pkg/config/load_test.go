// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadRejectsRemovedSystemdUnitResource(t *testing.T) {
	path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: SystemdUnit
      metadata:
        name: routerd.service
      spec:
        execStart:
          - /usr/local/sbin/routerd
          - serve
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected SystemdUnit resource to be rejected")
	}
	if !strings.Contains(err.Error(), "SystemdUnit") || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %v, want unsupported SystemdUnit", err)
	}
}

func TestLoadRejectsExplicitSocketSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "healthcheck",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: HealthCheck
      metadata:
        name: internet
      spec:
        daemon: routerd-healthcheck
        socketSource: /run/routerd/healthcheck/internet.sock
`,
		},
		{
			name: "pppoe",
			body: `
    - apiVersion: net.routerd.net/v1alpha1
      kind: PPPoESession
      metadata:
        name: wan
      spec:
        interface: wan
        username: example
        password: secret
        socketSource: /run/routerd/pppoe-client/wan.sock
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
`+tc.body)
			_, err := Load(path)
			if err == nil {
				t.Fatal("expected socketSource to be rejected")
			}
			if !strings.Contains(err.Error(), "socketSource") || !strings.Contains(err.Error(), "derives daemon sockets automatically") {
				t.Fatalf("error = %v, want unsupported socketSource", err)
			}
		})
	}
}

func TestLoadRejectsUnknownResourceKind(t *testing.T) {
	path := writeConfig(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: TypoedKind
      metadata:
        name: bad
      spec: {}
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected unknown kind to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported resource kind TypoedKind") {
		t.Fatalf("error = %v, want unsupported kind", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/router.yaml"
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
