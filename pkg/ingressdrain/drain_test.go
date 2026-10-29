// SPDX-License-Identifier: BSD-3-Clause

package ingressdrain

import (
	"database/sql"
	"encoding/json"
	"errors"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"path/filepath"
	"testing"
	"time"
)

func TestDrainDoesNotAcknowledgeAnUncommittedChange(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Drain(store, "service", "backend", time.Minute); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed drain reported success: %v", err)
	}
	if err := Undrain(store, "service", "backend"); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed undrain reported success: %v", err)
	}
}

func TestExpiredDrainCanBeObservedWithoutMutatingState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	writer, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	expired := State{Service: "service", Backend: "backend", DrainedUntil: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)}
	payload, err := json.Marshal(expired)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Set(Key("service", "backend"), string(payload), "fixture"); err != nil {
		t.Fatal(err)
	}
	reader, err := routerstate.OpenSQLiteReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, active := Current(reader, "service", "backend"); active {
		t.Fatal("expired drain remained active")
	}
	if got := writer.Get(Key("service", "backend")); got.Value != string(payload) {
		t.Fatal("read-only drain evaluation changed persisted state")
	}
}
