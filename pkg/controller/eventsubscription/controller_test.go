// SPDX-License-Identifier: BSD-3-Clause

package eventsubscription

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// fakePluginSource is a self-contained Go program compiled into a real
// executable by the test. It reads the PluginRequest from stdin and emits a
// PluginResult containing one RemoteAddressClaim per matched event, with the
// claim address taken from the event subject. EVENTSUB_FAIL=1 makes it exit
// non-zero to exercise the failure/retry path.
const fakePluginSource = `package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func main() {
	if os.Getenv("EVENTSUB_FAIL") == "1" {
		fmt.Fprintln(os.Stderr, "forced failure")
		os.Exit(3)
	}
	data, _ := io.ReadAll(os.Stdin)
	var req struct {
		Spec struct {
			Events []struct {
				ID      string ` + "`json:\"id\"`" + `
				Subject string ` + "`json:\"subject\"`" + `
			} ` + "`json:\"events\"`" + `
		} ` + "`json:\"spec\"`" + `
	}
	_ = json.Unmarshal(data, &req)
	var resources []map[string]any
	for _, ev := range req.Spec.Events {
		addr := ev.Subject
		resources = append(resources, map[string]any{
			"apiVersion": "hybrid.routerd.net/v1alpha1",
			"kind":       "RemoteAddressClaim",
			"metadata":   map[string]any{"name": "claim-" + ev.ID},
			"spec": map[string]any{
				"domainRef": "amd-1",
				"address":   addr,
				"ownerSide": "cloud",
				"capture":   map[string]any{"type": "proxy-arp", "interface": "eth0"},
				"delivery":  map[string]any{"mode": "route", "peerRef": "peer-1"},
			},
		})
	}
	out := map[string]any{
		"apiVersion": "plugin.routerd.net/v1alpha1",
		"kind":       "PluginResult",
		"metadata":   map[string]any{"name": "fake"},
		"status": map[string]any{
			"ttl":       "10m",
			"resources": resources,
		},
	}
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(out)
}
`

func buildFakePlugin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(fakePluginSource), 0o644); err != nil {
		t.Fatalf("write plugin source: %v", err)
	}
	bin := filepath.Join(dir, "fakeplugin")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake plugin: %v\n%s", err, out)
	}
	return bin
}

func mustStore(t *testing.T) *routerstate.SQLiteStore {
	t.Helper()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func subscriptionResource(pluginRef string, match api.EventSubscriptionMatch) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventSubscription"},
		Metadata: api.ObjectMeta{Name: "claim-sub"},
		Spec: api.EventSubscriptionSpec{
			GroupRef: "cloudedge",
			Match:    match,
			Trigger:  api.EventSubscriptionTrigger{PluginRef: pluginRef},
		},
	}
}

func pluginResource(name, executable string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.PluginSpec{Executable: executable},
	}
}

func newController(t *testing.T, store Store, router *api.Router) Controller {
	t.Helper()
	return Controller{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC) },
	}
}

func recordEvent(t *testing.T, store *routerstate.SQLiteStore, rec routerstate.EventRecord) {
	t.Helper()
	if rec.ObservedAt.IsZero() {
		rec.ObservedAt = time.Date(2026, 5, 30, 11, 0, 0, 0, time.UTC)
	}
	if err := store.RecordFederationEvent(rec); err != nil {
		t.Fatalf("record event %s: %v", rec.ID, err)
	}
}

func TestReconcileMatchInvokesPluginAndStoresPart(t *testing.T) {
	store := mustStore(t)
	bin := buildFakePlugin(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		pluginResource("claim-plugin", bin),
		subscriptionResource("claim-plugin", api.EventSubscriptionMatch{Types: []string{"routerd.client.ipv4.observed"}}),
	}}}
	recordEvent(t, store, routerstate.EventRecord{ID: "e1", Group: "cloudedge", Type: "routerd.client.ipv4.observed", Subject: "10.88.60.9/32", SourceNode: "onprem"})

	c := newController(t, store, router)
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	parts, err := store.ListDynamicConfigParts()
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("want 1 dynamic part, got %d", len(parts))
	}
	part := decodePart(t, parts[0])
	if len(part.Spec.Resources) != 1 {
		t.Fatalf("want 1 resource in part, got %d", len(part.Spec.Resources))
	}
	res := part.Spec.Resources[0]
	if res.Kind != "RemoteAddressClaim" {
		t.Fatalf("want RemoteAddressClaim, got %s", res.Kind)
	}
	ann := res.Metadata.Annotations
	if ann["routerd.net/dynamic-source"] != "EventSubscription/claim-sub" {
		t.Fatalf("dynamic-source annotation = %q", ann["routerd.net/dynamic-source"])
	}
	if ann["routerd.net/event-group"] != "cloudedge" {
		t.Fatalf("event-group annotation = %q", ann["routerd.net/event-group"])
	}
	if ann["routerd.net/event-id"] != "e1" {
		t.Fatalf("event-id annotation = %q", ann["routerd.net/event-id"])
	}
	if ann["routerd.net/event-subject"] != "10.88.60.9/32" {
		t.Fatalf("event-subject annotation = %q", ann["routerd.net/event-subject"])
	}

	runs, err := store.ListSubscriptionRuns("EventSubscription/claim-sub")
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != "succeeded" {
		t.Fatalf("want 1 succeeded run, got %+v", runs)
	}
}

func TestReconcileIdempotentNoRerun(t *testing.T) {
	store := mustStore(t)
	bin := buildFakePlugin(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		pluginResource("claim-plugin", bin),
		subscriptionResource("claim-plugin", api.EventSubscriptionMatch{Types: []string{"routerd.client.ipv4.observed"}}),
	}}}
	recordEvent(t, store, routerstate.EventRecord{ID: "e1", Group: "cloudedge", Type: "routerd.client.ipv4.observed", Subject: "10.88.60.9/32"})

	c := newController(t, store, router)
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}

	runs, _ := store.ListSubscriptionRuns("EventSubscription/claim-sub")
	if len(runs) != 1 {
		t.Fatalf("want 1 run row after two reconciles, got %d", len(runs))
	}
	if runs[0].Attempts != 1 {
		t.Fatalf("attempts changed on idempotent re-run = %d, want 1", runs[0].Attempts)
	}
	parts, _ := store.ListDynamicConfigParts()
	if len(parts) != 1 {
		t.Fatalf("want 1 part (no churn), got %d", len(parts))
	}
}

func TestReconcileNonMatchIgnored(t *testing.T) {
	store := mustStore(t)
	bin := buildFakePlugin(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		pluginResource("claim-plugin", bin),
		subscriptionResource("claim-plugin", api.EventSubscriptionMatch{
			Types:           []string{"routerd.client.ipv4.observed"},
			SubjectPrefixes: []string{"10.88."},
			Payload:         map[string]string{"role": "db"},
			SourceNodes:     []string{"onprem"},
		}),
	}}}
	// Wrong type.
	recordEvent(t, store, routerstate.EventRecord{ID: "wrong-type", Group: "cloudedge", Type: "other.event", Subject: "10.88.1.1/32", SourceNode: "onprem", Payload: map[string]string{"role": "db"}})
	// Wrong subject prefix.
	recordEvent(t, store, routerstate.EventRecord{ID: "wrong-subj", Group: "cloudedge", Type: "routerd.client.ipv4.observed", Subject: "192.0.2.1/32", SourceNode: "onprem", Payload: map[string]string{"role": "db"}})
	// Wrong payload value.
	recordEvent(t, store, routerstate.EventRecord{ID: "wrong-pl", Group: "cloudedge", Type: "routerd.client.ipv4.observed", Subject: "10.88.1.1/32", SourceNode: "onprem", Payload: map[string]string{"role": "web"}})
	// Wrong source node.
	recordEvent(t, store, routerstate.EventRecord{ID: "wrong-node", Group: "cloudedge", Type: "routerd.client.ipv4.observed", Subject: "10.88.1.1/32", SourceNode: "other", Payload: map[string]string{"role": "db"}})

	c := newController(t, store, router)
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if runs, _ := store.ListSubscriptionRuns("EventSubscription/claim-sub"); len(runs) != 0 {
		t.Fatalf("want no runs for non-matching events, got %+v", runs)
	}
	if parts, _ := store.ListDynamicConfigParts(); len(parts) != 0 {
		t.Fatalf("want no parts, got %d", len(parts))
	}
}

func TestReconcileTwoDistinctEventsCoexistInEffectiveConfig(t *testing.T) {
	store := mustStore(t)
	bin := buildFakePlugin(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		pluginResource("claim-plugin", bin),
		subscriptionResource("claim-plugin", api.EventSubscriptionMatch{Types: []string{"routerd.client.ipv4.observed"}}),
	}}}
	recordEvent(t, store, routerstate.EventRecord{ID: "e1", Group: "cloudedge", Type: "routerd.client.ipv4.observed", Subject: "10.88.60.9/32", ObservedAt: time.Date(2026, 5, 30, 11, 0, 0, 0, time.UTC)})

	c := newController(t, store, router)
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	// A second, distinct event arrives later (separate batch).
	recordEvent(t, store, routerstate.EventRecord{ID: "e2", Group: "cloudedge", Type: "routerd.client.ipv4.observed", Subject: "10.88.60.10/32", ObservedAt: time.Date(2026, 5, 30, 11, 30, 0, 0, time.UTC)})
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}

	// Both claims must appear in the effective config (proves part keying).
	startup := api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "AddressMobilityDomain"},
			Metadata: api.ObjectMeta{Name: "amd-1"},
			Spec:     api.AddressMobilityDomainSpec{Prefix: "10.88.60.0/24", Mode: "selective-address"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"},
			Metadata: api.ObjectMeta{Name: "peer-1"},
			Spec: api.OverlayPeerSpec{
				Role:     "cloud",
				NodeID:   "cloud-1",
				Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg0"},
			},
		},
	}}}
	parts := loadParts(t, store)
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	effective, result, err := dynamicconfig.BuildEffectiveConfig(startup, parts, nil, now)
	if err != nil {
		t.Fatalf("build effective: %v", err)
	}
	addrs := map[string]bool{}
	for _, res := range effective.Spec.Resources {
		if res.Kind != "RemoteAddressClaim" {
			continue
		}
		spec, err := res.RemoteAddressClaimSpec()
		if err != nil {
			t.Fatalf("claim spec: %v", err)
		}
		addrs[spec.Address] = true
	}
	if !addrs["10.88.60.9/32"] || !addrs["10.88.60.10/32"] {
		t.Fatalf("both claims must coexist, got addrs=%v active=%v", addrs, result.ActiveParts)
	}
}

func TestReconcileFailureRetriesThenGivesUp(t *testing.T) {
	store := mustStore(t)
	bin := buildFakePlugin(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		pluginResource("claim-plugin", bin),
		subscriptionResource("claim-plugin", api.EventSubscriptionMatch{Types: []string{"routerd.client.ipv4.observed"}}),
	}}}
	recordEvent(t, store, routerstate.EventRecord{ID: "e1", Group: "cloudedge", Type: "routerd.client.ipv4.observed", Subject: "10.88.60.9/32"})

	c := newController(t, store, router)
	// Force the plugin to fail via env (plugin env is not inherited from parent,
	// so set it on the Plugin spec).
	router.Spec.Resources[0].Spec = api.PluginSpec{Executable: bin, Env: map[string]string{"EVENTSUB_FAIL": "1"}}
	c.MaxAttempts = 2

	for i := 0; i < 4; i++ {
		if err := c.Reconcile(context.Background()); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	runs, _ := store.ListSubscriptionRuns("EventSubscription/claim-sub")
	if len(runs) != 1 {
		t.Fatalf("want 1 run row, got %d", len(runs))
	}
	if runs[0].Status != "failed" {
		t.Fatalf("want failed status, got %s", runs[0].Status)
	}
	// Attempts must cap at MaxAttempts (2): two starts, then no more eligible.
	if runs[0].Attempts != 2 {
		t.Fatalf("want attempts capped at 2, got %d", runs[0].Attempts)
	}
	if parts, _ := store.ListDynamicConfigParts(); len(parts) != 0 {
		t.Fatalf("want no parts on failure, got %d", len(parts))
	}
}

func TestReconcileDryRunNoPluginNoPart(t *testing.T) {
	store := mustStore(t)
	bin := buildFakePlugin(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		pluginResource("claim-plugin", bin),
		subscriptionResource("claim-plugin", api.EventSubscriptionMatch{Types: []string{"routerd.client.ipv4.observed"}}),
	}}}
	recordEvent(t, store, routerstate.EventRecord{ID: "e1", Group: "cloudedge", Type: "routerd.client.ipv4.observed", Subject: "10.88.60.9/32"})

	c := newController(t, store, router)
	c.DryRun = true
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if parts, _ := store.ListDynamicConfigParts(); len(parts) != 0 {
		t.Fatalf("dry-run wrote %d parts, want 0", len(parts))
	}
	runs, _ := store.ListSubscriptionRuns("EventSubscription/claim-sub")
	if len(runs) != 1 || runs[0].Status != "pending" {
		t.Fatalf("want 1 pending run in dry-run, got %+v", runs)
	}
}

func decodePart(t *testing.T, rec routerstate.DynamicConfigPartRecord) dynamicconfig.DynamicConfigPart {
	t.Helper()
	parts := loadPartsFromRecords(t, []routerstate.DynamicConfigPartRecord{rec})
	return parts[0]
}

func loadParts(t *testing.T, store *routerstate.SQLiteStore) []dynamicconfig.DynamicConfigPart {
	t.Helper()
	recs, err := store.ListDynamicConfigParts()
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	return loadPartsFromRecords(t, recs)
}

func loadPartsFromRecords(t *testing.T, recs []routerstate.DynamicConfigPartRecord) []dynamicconfig.DynamicConfigPart {
	t.Helper()
	out := make([]dynamicconfig.DynamicConfigPart, 0, len(recs))
	for _, rec := range recs {
		part := dynamicconfig.DynamicConfigPart{
			TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
			Spec: dynamicconfig.DynamicConfigPartSpec{
				Source:     rec.Source,
				Generation: rec.Generation,
				ObservedAt: rec.ObservedAt,
				ExpiresAt:  rec.ExpiresAt,
				Digest:     rec.Digest,
			},
		}
		if rec.ResourcesJSON != "" {
			if err := json.Unmarshal([]byte(rec.ResourcesJSON), &part.Spec.Resources); err != nil {
				t.Fatalf("decode resources: %v", err)
			}
		}
		out = append(out, part)
	}
	return out
}
