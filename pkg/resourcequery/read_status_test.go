// SPDX-License-Identifier: BSD-3-Clause

package resourcequery

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/state"
)

type failedStatusReader struct{ err error }

func (s failedStatusReader) ReadObjectStatus(string, string, string) (map[string]any, error) {
	return nil, s.err
}

func TestReadStatusSourcePreservesStorageFailure(t *testing.T) {
	failure := errors.New("status database unavailable")
	source := api.StatusValueSourceSpec{Resource: "Interface/wan", Field: "ipv4Addresses", Optional: true}
	if _, err := ReadValues(failedStatusReader{failure}, source); !errors.Is(err, failure) {
		t.Fatalf("optional source suppressed read failure: %v", err)
	}
	if _, err := ReadValue(failedStatusReader{failure}, source); !errors.Is(err, failure) {
		t.Fatalf("single source suppressed read failure: %v", err)
	}
	if ready, err := ReadDependenciesReady(failedStatusReader{failure}, []api.ResourceDependencySpec{{Resource: source.Resource, Phase: "Ready", Optional: true}}); ready || !errors.Is(err, failure) {
		t.Fatalf("optional dependency suppressed read failure: %v, %v", ready, err)
	}
	if ready, err := ReadSourceReady(failedStatusReader{failure}, source.Resource); ready || !errors.Is(err, failure) {
		t.Fatalf("readiness suppressed read failure: %v, %v", ready, err)
	}
}

func TestReadStatusSourceUsesCanonicalGroupAndNormalization(t *testing.T) {
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SaveObjectStatus(api.FederationAPIVersion, "EventGroup", "site", map[string]any{"nodes": []string{"one", "two", "one"}}); err != nil {
		t.Fatal(err)
	}
	values, err := ReadValues(store, api.StatusValueSourceSpec{Resource: "EventGroup/site", Field: "nodes"})
	if err != nil || !reflect.DeepEqual(values, []string{"one", "two"}) {
		t.Fatalf("read status source = %v, %v", values, err)
	}
	values, err = ReadValues(store, api.StatusValueSourceSpec{Resource: "Interface/missing", Field: "ipv4Addresses"})
	if err != nil || len(values) != 0 {
		t.Fatalf("genuine absence = %v, %v", values, err)
	}
}
