// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestSystemdTailscaleRuntimeLossAndRecovery(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	dir := t.TempDir()
	store := mapStore{}
	var response string
	var observationError error
	c := SystemdUnitController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode"}, Metadata: api.ObjectMeta{Name: "node"}, Spec: api.TailscaleNodeSpec{Hostname: "test-router"}},
		}}},
		Store: store, SystemdSystemDir: dir, BGPStatePath: filepath.Join(dir, "bgp", "applied.json"),
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if strings.HasSuffix(name, "tailscale") && strings.Join(args, " ") == "status --json" {
				return []byte(response), observationError
			}
			return []byte("ok"), nil
		},
	}
	for _, step := range []struct {
		name, body, phase, reason string
		err                       error
	}{
		{name: "running", body: `{"BackendState":"Running","Self":{"Online":true}}`, phase: "Running"},
		{name: "expired authentication", body: `{"BackendState":"NeedsLogin"}`, phase: "Pending", reason: "AuthenticationRequired"},
		{name: "machine approval", body: `{"BackendState":"NeedsMachineAuth"}`, phase: "Pending", reason: "MachineApprovalRequired"},
		{name: "starting", body: `{"BackendState":"Starting"}`, phase: "Pending", reason: "BackendNotRunning"},
		{name: "observation unavailable", err: errors.New("local daemon socket unavailable"), phase: "Error", reason: "RuntimeObservationFailed"},
		{name: "invalid observation", body: `{`, phase: "Error", reason: "RuntimeObservationFailed"},
		// Control-plane connectivity is not proof that cached peer traffic failed.
		{name: "running control offline", body: `{"BackendState":"Running","Self":{"Online":false}}`, phase: "Running"},
		{name: "recovered", body: `{"BackendState":"Running","Self":{"Online":true}}`, phase: "Running"},
	} {
		t.Run(step.name, func(t *testing.T) {
			response, observationError = step.body, step.err
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			status := store.ObjectStatus(api.NetAPIVersion, "TailscaleNode", "node")
			if status["phase"] != step.phase {
				t.Fatalf("phase=%v, want %s; status=%v", status["phase"], step.phase, status)
			}
			if step.reason != "" && status["reason"] != step.reason {
				t.Fatalf("reason=%v, want %s", status["reason"], step.reason)
			}
			if step.reason == "" && (status["reason"] != nil || status["runtimeError"] != nil) {
				t.Fatalf("recovery retained an obsolete error: %v", status)
			}
		})
	}
}
