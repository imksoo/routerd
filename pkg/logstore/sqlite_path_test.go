// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestDNSLogSQLitePathsRetainRelativeAndMemoryInputs(t *testing.T) {
	for _, name := range []string{"queries.db", ":memory:"} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			store, err := OpenDNSQueryLog(name)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.Record(t.Context(), DNSQuery{Timestamp: time.Now(), QuestionName: "path.example", QuestionType: "A"}); err != nil {
				t.Fatal(err)
			}
			rows, err := store.List(t.Context(), DNSQueryFilter{Limit: 10})
			if err != nil || len(rows) != 1 || rows[0].QuestionName != "path.example" {
				t.Fatalf("SQLite input %q did not keep its records: %v %v", name, rows, err)
			}
			if name == ":memory:" {
				if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("memory input created a filesystem name: %v", err)
				}
			}
		})
	}
}
