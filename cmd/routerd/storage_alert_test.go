// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestMirrorStorageAlertUsesRuntimeFilesystemAndSystemdStatus(t *testing.T) {
	originalDefaults := platformDefaults
	platformDefaults.RuntimeDir = filepath.Join(t.TempDir(), "run", "routerd")
	t.Cleanup(func() { platformDefaults = originalDefaults })

	socket := filepath.Join(t.TempDir(), "notify.sock")
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("NOTIFY_SOCKET", socket)

	alert := routerstate.NewStorageAlert("/var/lib/routerd/routerd.db", time.Date(2026, 9, 12, 17, 0, 0, 0, time.UTC))
	mirrorStorageAlert(alert)
	data, err := os.ReadFile(filepath.Join(platformDefaults.RuntimeDir, storageCriticalFile))
	if err != nil || !strings.Contains(string(data), "reboot-volatile-router-os") {
		t.Fatalf("runtime alert: %v %s", err, data)
	}
	if err := listener.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2048)
	n, _, err := listener.ReadFromUnix(buf)
	if err != nil {
		t.Fatal(err)
	}
	got := string(buf[:n])
	if !strings.HasPrefix(got, "STATUS=StorageCritical:") || !strings.Contains(got, "reboot router OS") {
		t.Fatalf("systemd notification = %q", got)
	}

	clearStorageAlert()
	if _, err := os.Stat(filepath.Join(platformDefaults.RuntimeDir, storageCriticalFile)); !os.IsNotExist(err) {
		t.Fatalf("stale alert was not cleared: %v", err)
	}
}

func TestNotifySystemdReadyWithoutSystemd(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	if err := notifySystemd("READY=1\nSTATUS=ready"); err != nil {
		t.Fatal(err)
	}
}
