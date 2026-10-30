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

func TestResourceHTTPDistinguishesDeclarationFromRetainedWithdrawalStatus(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	status := map[string]any{"phase": "Pending", "reason": "WithdrawalFailed", "interface": "wg0", "configured": true}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "WireGuardPeer", "removed-peer", status); err != nil {
		t.Fatal(err)
	}
	declaration := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0"},
	}}}}
	server := httptest.NewServer(New(Options{Store: store, Router: declaration}))
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/resources")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var rows []ResourceStatus
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("retained withdrawal status hidden: %+v", rows)
	}
	for _, row := range rows {
		if row.Declared == nil {
			t.Fatalf("known current declaration has unknown row membership: %+v", row)
		}
		if row.Kind == "WireGuardPeer" && (*row.Declared || row.Status["phase"] != "Pending" || row.Status["reason"] != "WithdrawalFailed" || row.Status["interface"] != "wg0") {
			t.Fatalf("retained receipt became a current declaration or lost withdrawal evidence: %+v", row)
		}
		if row.Kind == "Interface" && (!*row.Declared || row.Status["phase"] != "NotObserved") {
			t.Fatalf("current declaration missing: %+v", row)
		}
	}
	persisted, err := store.ReadObjectStatus(api.NetAPIVersion, "WireGuardPeer", "removed-peer")
	if err != nil {
		t.Fatal(err)
	}
	if persisted["configured"] != true || persisted["owner"] != nil || persisted["phase"] != "Pending" {
		t.Fatalf("read-only web annotation mutated the stored receipt: %+v", persisted)
	}
}
