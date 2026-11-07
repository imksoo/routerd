// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestSAMHTTPDistinguishesMissingCountersFromObservedZero(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveObjectStatus(api.HybridAPIVersion, "MobilityPool", "unknown", map[string]any{"phase": "Pending"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.HybridAPIVersion, "MobilityPool", "observed-empty", map[string]any{"phase": "Ready", "generatedBGPPaths": 0, "resolvedMemberCount": 0}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Options{Store: store}))
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/sam")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var status SAMStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if len(status.Pools) != 2 {
		t.Fatalf("pool observations missing: %+v", status)
	}
	for _, pool := range status.Pools {
		if pool.Name == "unknown" && (pool.GeneratedBGPPaths != nil || pool.ResolvedMemberCount != nil) {
			t.Fatalf("missing counters became observed zero: %+v", pool)
		}
		if pool.Name == "observed-empty" && (pool.GeneratedBGPPaths == nil || *pool.GeneratedBGPPaths != 0 || pool.ResolvedMemberCount == nil || *pool.ResolvedMemberCount != 0) {
			t.Fatalf("observed zero became missing: %+v", pool)
		}
	}
}
