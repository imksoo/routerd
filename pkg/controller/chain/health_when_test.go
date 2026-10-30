// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestHealthWhenTrueDoesNotPromoteRetainedSQLiteHistory(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	resource := whenFalseHealthCheck("internet")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
	sample := api.HealthObservation{Phase: "Healthy", CheckedAt: time.Now().UTC()}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "internet", map[string]any{
		"phase": "Pending", "reason": "WhenFalse", "routing": sample,
		"observed": map[string]any{"phase": "Healthy", "lastResult": "passed", "lastCheckedAt": sample.CheckedAt.Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Router: router}
	for _, role := range []string{"backup", "master"} {
		if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "lan", map[string]any{"role": role}); err != nil {
			t.Fatal(err)
		}
		if err := runner.saveWhenFalseStatuses(eventedStore{Store: store, Router: router}); err != nil {
			t.Fatal(err)
		}
		status, err := store.ReadObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
		if err != nil || status["phase"] != "Pending" || status["reason"] != "WhenFalse" || status["observed"] == nil || status["routing"] == nil {
			t.Fatalf("role=%s changed retained history to current status: %#v %v", role, status, err)
		}
		current, err := store.HealthObservation("internet")
		if err != nil || current != (api.HealthObservation{}) {
			t.Fatalf("role=%s history authorizes routing: %+v %v", role, current, err)
		}
	}
	// A new actual observation, rather than a when transition, restores authority.
	if err := store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "internet", map[string]any{"phase": "Healthy", "routing": sample}); err != nil {
		t.Fatal(err)
	}
	current, err := store.HealthObservation("internet")
	if err != nil || !current.Usable(time.Now(), api.HealthCheckSpec{}) {
		t.Fatalf("new observation did not restore authority: %+v %v", current, err)
	}
}
