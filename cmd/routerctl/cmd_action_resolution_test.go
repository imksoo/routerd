// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestActionResolveRecordsVerifiedOutcomeThroughRealJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.ImportAction(routerstate.ActionExecutionRecord{IdempotencyKey: "lost", Provider: "aws", Action: "assign-secondary-ip", Target: map[string]string{"address": "10.0.0.5/32", "nicRef": "private-fixture"}}); err != nil {
		t.Fatal(err)
	}
	rec, _, err := store.GetActionByIdempotencyKey("lost")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.ApproveAction(rec.ID, "approval", now); err != nil {
		t.Fatal(err)
	}
	if claimed, err := store.BeginActionExecution(rec.ID, now); err != nil || !claimed {
		t.Fatalf("claim %v %v", claimed, err)
	}
	var out bytes.Buffer
	args := []string{"resolve", "--state-file", path, "--outcome", "succeeded", "--by", "alice", "--evidence", "verified exact assigned address; no request in flight", fmt.Sprint(rec.ID)}
	guard, err := store.TryLockActionOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := actionCommand(args, &out, &out); err == nil {
		t.Fatal("CLI stole live provider invocation")
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := actionCommand(args, &out, &out); err != nil {
		t.Fatal(err)
	}
	got, _, err := store.GetActionByID(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != routerstate.ActionSucceeded || !strings.Contains(got.ResultMessage, "alice") || !strings.Contains(got.ResultMessage, "verified exact") {
		t.Fatalf("operator evidence lost: %#v", got)
	}
	if got.Target["address"] != "10.0.0.5/32" {
		t.Fatal("resolution changed plan")
	}
}

func TestActionHelpDoesNotRequireAnID(t *testing.T) {
	for _, command := range []string{"show", "approve", "execute", "rollback", "resolve"} {
		var out bytes.Buffer
		if err := actionCommand([]string{command, "--help"}, &out, &out); err != nil {
			t.Fatalf("%s help: %v", command, err)
		}
		if out.Len() == 0 {
			t.Fatalf("%s help missing", command)
		}
	}
}
