// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestEventsHTTPKeepsCursorSeparateFromRowLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, name := range []string{"before", "cursor", "after"} {
		if err := store.RecordEvent("test.routerd.net/v1", "Test", name, "Normal", "Observed", name); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE events SET id=id+50000"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Options{Store: store}))
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/events?sinceID=50002&limit=100")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var rows []routerstate.StoredEvent
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(rows) != 1 || rows[0].ID != 50003 || rows[0].Name != "after" {
		t.Fatalf("cursor was clamped as a row limit: status=%d rows=%+v", response.StatusCode, rows)
	}
	response, err = server.Client().Get(server.URL + "/api/v1/events?sinceID=9223372036854775808")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("overflowed cursor became a successful request: %d", response.StatusCode)
	}
}
