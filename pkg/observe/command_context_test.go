// SPDX-License-Identifier: BSD-3-Clause

package observe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/subprocessio"
)

func TestConnectionObserversCancelTheirOwnedCommands(t *testing.T) {
	for _, test := range []struct {
		name string
		read func(context.Context) (*ConnectionTable, error)
	}{
		{"conntrack", func(ctx context.Context) (*ConnectionTable, error) { return Connections(ctx, 10) }},
		{"pf-states", func(ctx context.Context) (*ConnectionTable, error) { return PFStates(ctx, 10) }},
		{"pf-snapshot", PFStateSnapshot},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			started := filepath.Join(dir, "started")
			for _, name := range []string{"conntrack", "pfctl"} {
				script := fmt.Sprintf("#!/bin/sh\nprintf started > %q\nexec /bin/sleep 30\n", started)
				if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := test.read(ctx); done <- err }()
			deadline := time.Now().Add(time.Second)
			for {
				if _, err := os.Stat(started); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("test-owned observation command did not start")
				}
				time.Sleep(time.Millisecond)
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled observer returned %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled observer retained its child command")
			}
		})
	}
}

func TestConnectionsRejectOversizedSnapshotAndKeepStatesOutOfErrors(t *testing.T) {
	dir := t.TempDir()
	writeCommands := func(script string) {
		t.Helper()
		for _, name := range []string{"conntrack", "pfctl"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeCommands("#!/bin/sh\nexec /bin/dd if=/dev/zero bs=1048576 count=65 2>/dev/null\n")
	table, err := Connections(context.Background(), -1)
	var limitErr *subprocessio.OutputLimitError
	if table != nil || !errors.As(err, &limitErr) || limitErr.Stream != "stdout" || limitErr.Limit != connectionOutputLimit {
		t.Fatalf("oversized dump became a complete observation: table=%v error=%v", table, err)
	}
	writeCommands("#!/bin/sh\nprintf 'private-connection-state'\nprintf 'permission denied' >&2\nexit 1\n")
	table, err = Connections(context.Background(), 1)
	if table != nil || err == nil || strings.Contains(err.Error(), "private-connection-state") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("failed observer diagnostics: table=%v error=%v", table, err)
	}
}

func TestObserversRejectUnreadableDumpInsteadOfPublishingEmpty(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"conntrack", "pfctl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nprintf 'unrecognized state output\\n'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, read := range []func(context.Context, int) (*ConnectionTable, error){Connections, PFStates} {
		table, err := read(context.Background(), -1)
		if table != nil || err == nil {
			t.Fatalf("unreadable complete dump published empty: table=%+v error=%v", table, err)
		}
	}
}
