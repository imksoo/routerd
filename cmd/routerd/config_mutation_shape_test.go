// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/eventlog"
)

func TestRuntimeShapeChangedRejectsLifecycleResources(t *testing.T) {
	current := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: map[string]any{"listen": []any{"127.0.0.1"}}},
	}}}
	next := &api.Router{Spec: api.RouterSpec{Resources: append([]api.Resource(nil), current.Spec.Resources...)}}
	next.Spec.Resources[0].Spec = map[string]any{"listen": []any{"127.0.0.2"}}

	changed, resources := runtimeShapeChanged(current, next)
	if !changed || len(resources) != 1 {
		t.Fatalf("changed=%v resources=%v", changed, resources)
	}
}

func TestRuntimeShapeChangedAllowsDataplaneOnlyChange(t *testing.T) {
	current := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "allow"}},
	}}}
	next := &api.Router{Spec: api.RouterSpec{Resources: nil}}
	if changed, resources := runtimeShapeChanged(current, next); changed {
		t.Fatalf("changed=%v resources=%v", changed, resources)
	}
}

func TestRuntimeReloadHasFiniteDeadline(t *testing.T) {
	wantErr := errors.New("stop after inspecting deadline")
	mutator := serveConfigMutator{
		reload: func(ctx context.Context, _ *api.Router) error {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("runtime reload context has no deadline")
			}
			remaining := time.Until(deadline)
			if remaining < 59*time.Second || remaining > time.Minute {
				t.Fatalf("runtime reload deadline remaining = %s, want approximately 60s", remaining)
			}
			return wantErr
		},
	}
	if err := mutator.reloadRuntime(&api.Router{}); !errors.Is(err, wantErr) {
		t.Fatalf("reloadRuntime error = %v, want %v", err, wantErr)
	}
}

func TestLifecycleLiveApplyReloadsDNSResolverWithoutProcessRestart(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	statePath := filepath.Join(dir, "routerd.db")
	if err := os.WriteFile(configPath, []byte(dnsRuntimeYAML("127.0.0.1")), 0o644); err != nil {
		t.Fatal(err)
	}
	current, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	gate := &sync.RWMutex{}
	reloads := 0
	mutator := serveConfigMutator{
		configPath: configPath,
		statePath:  statePath,
		baseOpts: applyOptions{
			ConfigPath:         configPath,
			StatePath:          statePath,
			LedgerPath:         filepath.Join(dir, "ledger.db"),
			SkipServiceManager: true,
			Sandbox:            true,
			MutationGate:       gate,
		},
		cache:     &resultCache{},
		logger:    &eventlog.Logger{},
		getRouter: func() *api.Router { return current },
		setRouter: func(next *api.Router) { current = next },
		reload: func(_ context.Context, next *api.Router) error {
			reloads++
			if gate.TryRLock() {
				gate.RUnlock()
				t.Error("runtime reload was called without the exclusive mutation gate")
			}
			spec, err := next.Spec.Resources[0].DNSResolverSpec()
			if err != nil {
				return err
			}
			if got := spec.Listen[0].Addresses[0]; got != "127.0.0.2" {
				t.Errorf("reloaded DNS address = %q, want 127.0.0.2", got)
			}
			return nil
		},
	}
	result, err := mutator.apply(nil, controlapi.ApplyRequest{
		CandidateYAML: dnsRuntimeYAML("127.0.0.2"),
		Replace:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reloads != 1 {
		t.Fatalf("runtime reloads = %d, want 1", reloads)
	}
	if result.Result.Generation == 0 {
		t.Fatalf("apply result = %+v", result.Result)
	}
	spec, err := current.Spec.Resources[0].DNSResolverSpec()
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.Listen[0].Addresses[0]; got != "127.0.0.2" {
		t.Fatalf("active DNS address = %q, want 127.0.0.2", got)
	}
}

func dnsRuntimeYAML(address string) string {
	return `apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: runtime-reload
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSResolver
      metadata:
        name: lan
      spec:
        listen:
          - name: loopback
            addresses: [` + address + `]
            port: 5353
            sources: [default]
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSForwarder
      metadata:
        name: default
      spec:
        resolver: DNSResolver/lan
        match: ["."]
        upstreams: [DNSUpstream/public]
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSUpstream
      metadata:
        name: public
      spec:
        protocol: udp
        address: 1.1.1.1
`
}
