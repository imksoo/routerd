// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestConsoleUsesCurrentDeclarationAfterApply(t *testing.T) {
	router := func(name string) *api.Router {
		return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: name},
			Spec:     api.InterfaceSpec{IfName: name},
		}}}}
	}
	initial := router("old-lan")
	current := initial
	reads := 0
	handler := New(Options{Router: initial, CurrentRouter: func() *api.Router {
		reads++
		return current
	}})
	for _, name := range []string{"old-lan", "new-lan"} {
		current = router(name)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil))
		var resources []routerstate.ObjectStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &resources); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK || len(resources) != 1 || resources[0].Name != name {
			t.Fatalf("current resource %q: status=%d body=%s", name, rec.Code, rec.Body.String())
		}
	}
	if reads != 2 {
		t.Fatalf("declaration reads = %d, want one per request", reads)
	}
	snapshot := handler.Snapshot(context.Background(), SnapshotOptions{
		EventLimit: -1, ConnectionsLimit: -1, FirewallLimit: -1,
		DNSQueryLimit: -1, TrafficFlowLimit: -1, SkipDHCPLeases: true,
	})
	if reads != 3 || len(snapshot.Interfaces) != 1 || snapshot.Interfaces[0].Name != "new-lan" {
		t.Fatalf("snapshot declaration reads=%d interfaces=%+v", reads, snapshot.Interfaces)
	}
}
