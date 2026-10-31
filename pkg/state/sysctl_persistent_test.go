// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func sysctlOwnerRecord(t *testing.T, token string) SysctlPersistentOwner {
	t.Helper()
	return SysctlPersistentOwner{Version: 1, Kind: "Sysctl", ResourceName: "forwarding", Name: SysctlPersistentOwnerName("forwarding", "net.inet.ip.forwarding"),
		Token: strings.Repeat(token, 32), Key: "net.inet.ip.forwarding", Path: filepath.Join(t.TempDir(), "sysctl.conf.local"), Block: "exact owned block", Pending: true}
}

func TestSysctlPersistentOwnerCASPreservesNewerReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	first, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	original := sysctlOwnerRecord(t, "a")
	if err := first.SaveSysctlPersistentOwner(t.Context(), original.Kind, original.Name, nil, &original, false); err != nil {
		t.Fatal(err)
	}
	replacement := original
	replacement.Token, replacement.Block = strings.Repeat("b", 32), "new owner's exact block"
	if err := second.SaveSysctlPersistentOwner(t.Context(), original.Kind, original.Name, &original, &replacement, false); err != nil {
		t.Fatal(err)
	}
	stale := original
	stale.Pending = false
	if err := first.SaveSysctlPersistentOwner(t.Context(), original.Kind, original.Name, &original, &stale, false); !errors.Is(err, ErrSysctlPersistentOwnerChanged) {
		t.Fatalf("stale completion was accepted: %v", err)
	}
	if err := first.SaveSysctlPersistentOwner(t.Context(), original.Kind, original.Name, &original, nil, true); !errors.Is(err, ErrSysctlPersistentOwnerChanged) {
		t.Fatalf("stale withdrawal was accepted: %v", err)
	}
	status, err := first.ReadObjectStatus(api.SystemAPIVersion, original.Kind, original.Name)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(status[SysctlPersistentOwnerKey])
	if err != nil {
		t.Fatal(err)
	}
	var actual SysctlPersistentOwner
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	if actual != replacement {
		t.Fatalf("new receipt was overwritten: %+v", actual)
	}
	if _, visible := status["phase"]; visible {
		t.Fatal("private receipt acquired a declaration display phase")
	}
}

func TestSysctlPersistentOwnerCancellationAfterMutexWaitCannotPublish(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	owner := sysctlOwnerRecord(t, "a")
	ctx, cancel := context.WithCancel(t.Context())
	store.mu.Lock()
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		done <- store.SaveSysctlPersistentOwner(ctx, owner.Kind, owner.Name, nil, &owner, false)
	}()
	<-started
	cancel()
	store.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled owner publication: %v", err)
	}
	status, err := store.ReadObjectStatus(api.SystemAPIVersion, owner.Kind, owner.Name)
	if err != nil || status != nil {
		t.Fatalf("canceled receipt was published: %+v %v", status, err)
	}
}

func TestSysctlDisplayUpdatesCannotOverwritePersistentAuthority(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	owner := sysctlOwnerRecord(t, "a")
	if err := store.SaveSysctlPersistentOwner(t.Context(), owner.Kind, owner.Name, nil, &owner, false); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.SystemAPIVersion, "Sysctl", owner.ResourceName, map[string]any{"phase": "Skipped", "reason": "ReadFailedOptional"}); err != nil {
		t.Fatal(err)
	}
	actual, err := store.ReadObjectStatus(api.SystemAPIVersion, owner.Kind, owner.Name)
	if err != nil || actual[SysctlPersistentOwnerKey] == nil {
		t.Fatalf("display update erased authority: %+v %v", actual, err)
	}
	display, err := store.ReadObjectStatus(api.SystemAPIVersion, owner.Kind, owner.ResourceName)
	if err != nil || display[SysctlPersistentOwnerKey] != nil || display["phase"] != "Skipped" {
		t.Fatalf("display and authority overlap: %+v %v", display, err)
	}
}

func TestSysctlPersistentRetirementCannotRemoveAnotherKeyDisplay(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	old := sysctlOwnerRecord(t, "a")
	if err := store.SaveSysctlPersistentOwner(t.Context(), old.Kind, old.Name, nil, &old, false); err != nil {
		t.Fatal(err)
	}
	current := old
	current.Key = "net.inet6.ip6.forwarding"
	current.Name = SysctlPersistentOwnerName(current.ResourceName, current.Key)
	current.Token = strings.Repeat("b", 32)
	if err := store.SaveSysctlPersistentOwner(t.Context(), current.Kind, current.Name, nil, &current, false); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.SystemAPIVersion, current.Kind, current.ResourceName, map[string]any{"phase": "Applied", "key": current.Key}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSysctlPersistentOwner(t.Context(), old.Kind, old.Name, &old, nil, true); err != nil {
		t.Fatal(err)
	}
	display := store.ObjectStatus(api.SystemAPIVersion, current.Kind, current.ResourceName)
	if display["phase"] != "Applied" || display["key"] != current.Key {
		t.Fatalf("retired key replaced another current owner display: %+v", display)
	}
}
