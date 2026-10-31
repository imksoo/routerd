// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestRollbackStateFileAcceptsRelativePathWithoutCreatingAnotherDatabase(t *testing.T) {
	for _, name := range []string{"state.db", "state?mode=memory.db"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			store, err := routerstate.OpenSQLite(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			generation := seedGeneration(t, store, "private-state", testRouterYAML("relative-state"), true, "Applied")
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			if err := rollbackCommand([]string{"--list", "--state-file", name}, &output, io.Discard); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), fmt.Sprint(generation)) || !strings.Contains(output.String(), "(latest)") {
				t.Fatalf("relative state-file did not select the stored generation: %s", &output)
			}
			if err := rollbackCommand([]string{"--list", "--state-file", "missing.db"}, io.Discard, io.Discard); err == nil {
				t.Fatal("missing read-only source was acknowledged")
			}
			if _, err := os.Stat("missing.db"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("read-only state-file created another database: %v", err)
			}
		})
	}
}
