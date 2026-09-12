// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

const storageCriticalFile = "storage-critical.json"

func mirrorStorageAlert(alert routerstate.StorageAlert) {
	path := filepath.Join(platformDefaults.RuntimeDir, storageCriticalFile)
	if err := routerstate.WriteStorageAlertFile(path, alert); err != nil {
		slog.Error("cannot write routerd storage alert outside state database", "path", path, "error", err)
	}
	status := fmt.Sprintf("StorageCritical: %s; volatile Live ISO: reboot router OS; persistent disk: prune/compact events or expand storage", alert.Condition)
	if err := notifySystemdStatus(status); err != nil {
		slog.Debug("cannot update systemd status", "error", err)
	}
	slog.Error("routerd storage critical; existing routing is kept where possible",
		"condition", alert.Condition,
		"stateFile", alert.StateFile,
		"recoveryAction", alert.RecoveryAction,
		"alertFile", path,
	)
}

func notifySystemdStatus(status string) error {
	return notifySystemd("STATUS=" + status)
}

func notifySystemd(payload string) error {
	path := strings.TrimSpace(os.Getenv("NOTIFY_SOCKET"))
	if path == "" {
		return nil
	}
	if strings.HasPrefix(path, "@") {
		path = "\x00" + strings.TrimPrefix(path, "@")
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(payload))
	return err
}

func clearStorageAlert() {
	path := filepath.Join(platformDefaults.RuntimeDir, storageCriticalFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		slog.Warn("cannot clear stale routerd storage alert", "path", path, "error", err)
	}
}
