// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/pppoeclient"
)

func TestRunCancellationWaitsForNativeStopAndReportsCleanup(t *testing.T) {
	for _, unresolved := range []bool{false, true} {
		name := "confirmed"
		if unresolved {
			name = "unconfirmed"
		}
		t.Run(name, func(t *testing.T) {
			oldOS, oldCommand := currentPPPoEOS, pppoeSessionCommand
			oldExists := linuxPPPoEInterfaceExists
			oldGrace, oldTimeout, oldInterval := pppoeStopGracePeriod, pppoeInterfaceTeardownTimeout, pppoeInterfaceProbeInterval
			t.Cleanup(func() {
				currentPPPoEOS, pppoeSessionCommand = oldOS, oldCommand
				linuxPPPoEInterfaceExists = oldExists
				pppoeStopGracePeriod, pppoeInterfaceTeardownTimeout, pppoeInterfaceProbeInterval = oldGrace, oldTimeout, oldInterval
			})
			currentPPPoEOS = func() platform.OS { return platform.OSLinux }
			var present atomic.Bool
			linuxPPPoEInterfaceExists = func(context.Context, string) (bool, error) { return present.Load(), nil }
			pppoeStopGracePeriod, pppoeInterfaceTeardownTimeout, pppoeInterfaceProbeInterval = 200*time.Millisecond, 30*time.Millisecond, time.Millisecond
			dir := t.TempDir()
			ready, stopped := filepath.Join(dir, "ready"), filepath.Join(dir, "stopped")
			script := filepath.Join(dir, "backend.sh")
			data := "#!/bin/sh\ntrap 'printf stopped > " + stopped + "; exit 0' INT\nprintf ready > " + ready + "\nwhile :; do sleep 0.01; done\n"
			if err := os.WriteFile(script, []byte(data), 0o700); err != nil {
				t.Fatal(err)
			}
			pppoeSessionCommand = func(pppoeclient.Config) (string, []string) { return "/bin/sh", []string{script} }
			d := newDaemon(options{resource: "cancel", ifname: "fixture0", username: "fixture", password: "fixture", runtimeDir: dir, socketPath: filepath.Join(dir, "control.sock"), stateFile: filepath.Join(dir, "state.json"), eventFile: filepath.Join(dir, "events.jsonl")}, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- d.Run(ctx) }()
			deadline := time.Now().Add(2 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("owned backend did not start")
				}
				time.Sleep(time.Millisecond)
			}
			present.Store(unresolved)
			cancel()
			select {
			case err := <-done:
				if unresolved {
					if err == nil || !strings.Contains(err.Error(), "cleanup") {
						t.Errorf("unconfirmed cleanup returned %v", err)
					}
					d.mu.Lock()
					fenced := d.teardownFailed && d.cmd != nil
					d.mu.Unlock()
					if !fenced {
						t.Error("unconfirmed native identity was released")
					}
				} else if err != nil {
					t.Errorf("confirmed cancellation returned %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("daemon retained its owned backend")
			}
			if _, err := os.Stat(stopped); err != nil {
				t.Errorf("native owner did not deliver graceful stop: %v", err)
			}
		})
	}
}
