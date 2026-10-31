// SPDX-License-Identifier: BSD-3-Clause

//go:build linux || freebsd

package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestCanceledOwnerReapsActualPluginAndCannotPublish(t *testing.T) {
	dir := t.TempDir()
	pidFile, executable := filepath.Join(dir, "pid"), filepath.Join(dir, "plugin.sh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nset -eu\ncat >/dev/null\necho $$ >\"$PID\"\nexec sleep 30\n"), 0755); err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"}, Metadata: api.ObjectMeta{Name: "sleeper"}, Spec: api.PluginSpec{Executable: executable, Timeout: "5s", Env: map[string]string{"PID": pidFile}, Triggers: []api.PluginTrigger{{Type: "interval", Every: "1h"}}}}}}}
	c, err := New(router, store, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	done := make(chan error, 1)
	go func() { _, err := c.Periodic(ctx); done <- err }()
	waited := false
	t.Cleanup(func() {
		cancel()
		if !waited {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("plugin owner did not stop")
			}
		}
	})
	pid := 0
	for pid == 0 {
		if data, err := os.ReadFile(pidFile); err == nil {
			pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || pid <= 0 {
				t.Fatalf("invalid owned plugin PID: %q %v", data, err)
			}
			break
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	cancel()
	select {
	case err := <-done:
		waited = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation result: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("actual plugin was not reaped")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned plugin survived owner cancellation: pid=%d err=%v", pid, err)
	}
	parts, err := store.GetDynamicConfigPartsBySource("Plugin/sleeper")
	if err != nil || len(parts) != 0 {
		t.Fatalf("canceled plugin published: %+v %v", parts, err)
	}
}
