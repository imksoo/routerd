// SPDX-License-Identifier: BSD-3-Clause

package eventfederation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/eventd"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s[apiVersion+"/"+kind+"/"+name]
}

func groupResource() api.Resource {
	r := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"}}
	r.Metadata.Name = "edge"
	r.Spec = mustSpec(api.EventGroupSpec{
		NodeName:     "router06",
		Listen:       api.EventGroupListen{Address: "10.99.0.6", Port: 8787},
		ReplayWindow: "10m",
		Auth:         api.EventGroupAuth{Mode: "hmac", SecretFile: "/var/lib/routerd/eventd/edge/secret"},
		Retention:    api.EventGroupRetention{MaxEvents: 500, MaxAge: "24h"},
	})
	return r
}

func peerResource(name, groupRef, node string) api.Resource {
	r := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventPeer"}}
	r.Metadata.Name = name
	r.Spec = mustSpec(api.EventPeerSpec{
		GroupRef: groupRef,
		NodeName: node,
		Endpoint: "http://" + node + ":8787",
		Types:    []string{"observed"},
	})
	return r
}

func mustSpec(v any) map[string]any {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		panic(err)
	}
	return m
}

func TestReconcileWritesConfigWithMatchingPeerOnly(t *testing.T) {
	dir := t.TempDir()
	store := mapStore{}
	router := &api.Router{}
	router.Spec.Resources = []api.Resource{
		groupResource(),
		peerResource("cloud01", "edge", "10.99.0.7"),
		peerResource("other", "different-group", "10.99.0.9"),
	}
	c := Controller{Router: router, Store: store, RuntimeDir: filepath.Join(dir, "run"), StateDir: filepath.Join(dir, "state")}
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	configPath := filepath.Join(dir, "state", "eventd", "edge", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg, err := eventd.DecodeConfig(func(v any) error { return json.Unmarshal(data, v) })
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.NodeName != "router06" || cfg.Group != "edge" {
		t.Fatalf("unexpected identity: %+v", cfg)
	}
	if cfg.Listen.Address != "10.99.0.6" || cfg.Listen.Port != 8787 {
		t.Fatalf("unexpected listen: %+v", cfg.Listen)
	}
	if cfg.SecretFile != "/var/lib/routerd/eventd/edge/secret" {
		t.Fatalf("unexpected secretFile: %q", cfg.SecretFile)
	}
	if cfg.StatePath != filepath.Join(dir, "state", "routerd.db") {
		t.Fatalf("unexpected statePath: %q", cfg.StatePath)
	}
	if cfg.ReplayWindow.String() != "10m0s" {
		t.Fatalf("unexpected replayWindow: %s", cfg.ReplayWindow)
	}
	if cfg.Retention.MaxEvents != 500 || cfg.Retention.MaxAge.String() != "24h0m0s" {
		t.Fatalf("unexpected retention: %+v", cfg.Retention)
	}
	if len(cfg.Peers) != 1 {
		t.Fatalf("expected exactly 1 matching peer, got %d: %+v", len(cfg.Peers), cfg.Peers)
	}
	if cfg.Peers[0].NodeName != "10.99.0.7" {
		t.Fatalf("unexpected peer: %+v", cfg.Peers[0])
	}
	if status := store.ObjectStatus(api.FederationAPIVersion, "EventGroup", "edge")["phase"]; status != "Applied" {
		t.Fatalf("expected Applied phase, got %v", status)
	}
}

func TestReconcileDryRunWritesNoFile(t *testing.T) {
	dir := t.TempDir()
	store := mapStore{}
	router := &api.Router{}
	router.Spec.Resources = []api.Resource{groupResource()}
	c := Controller{Router: router, Store: store, DryRun: true, StateDir: filepath.Join(dir, "state")}
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state", "eventd", "edge", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no config file in dry-run, stat err=%v", err)
	}
	if status := store.ObjectStatus(api.FederationAPIVersion, "EventGroup", "edge")["phase"]; status != "Pending" {
		t.Fatalf("expected Pending phase in dry-run, got %v", status)
	}
}

func TestReconcileNoEventGroupIsNoOp(t *testing.T) {
	dir := t.TempDir()
	store := mapStore{}
	router := &api.Router{}
	router.Spec.Resources = []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}},
	}
	c := Controller{Router: router, Store: store, StateDir: filepath.Join(dir, "state")}
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "state", "eventd")); len(entries) != 0 {
		t.Fatalf("expected no eventd dir, got %d entries", len(entries))
	}
	if len(store) != 0 {
		t.Fatalf("expected no status writes, got %d", len(store))
	}
}
