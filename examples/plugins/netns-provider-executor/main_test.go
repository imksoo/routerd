// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssignSecondaryExecutesIPAddrReplace(t *testing.T) {
	var calls []string
	req := `{"spec":{"action":"assign-secondary-ip","provider":"netns","mode":"execute","target":{"interface":"eth2","address":"10.77.60.11/32","captureStrategy":"secondary-ip"}}}`
	err := run(context.Background(), strings.NewReader(req), &bytes.Buffer{}, func(_ context.Context, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(calls) != 1 || calls[0] != "ip addr replace 10.77.60.11/32 dev eth2" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestAssignSecondaryIgnoresMobilityPathAndExecutesIPAddrReplace(t *testing.T) {
	var calls []string
	req := `{"spec":{"action":"assign-secondary-ip","provider":"netns","mode":"execute","target":{"interface":"eth2","address":"10.77.60.11/32","captureStrategy":"secondary-ip"},"parameters":{"mobilityPathSig":"prefix=10.77.60.11/32;nextHops=10.99.0.55"}}}`
	err := run(context.Background(), strings.NewReader(req), &bytes.Buffer{}, func(_ context.Context, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(calls) != 1 || calls[0] != "ip addr replace 10.77.60.11/32 dev eth2" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestDryRunDoesNotMutate(t *testing.T) {
	req := `{"spec":{"action":"assign-secondary-ip","provider":"netns","mode":"dry-run","target":{"interface":"eth2","address":"10.77.60.11/32"}}}`
	var out bytes.Buffer
	err := run(context.Background(), strings.NewReader(req), &out, func(_ context.Context, name string, args ...string) (string, error) {
		t.Fatalf("dry-run executed %s %v", name, args)
		return "", nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var res executeActionResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Status.Status != statusSucceeded || !res.Status.UndoAvailable {
		t.Fatalf("result = %#v", res.Status)
	}
}

func TestAssignSecondaryUpdatesProviderState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "provider-state.json")
	t.Setenv(envProviderState, statePath)
	t.Setenv(envSelfNode, "aws-leaf-a")
	req := `{"spec":{"action":"assign-secondary-ip","provider":"netns","mode":"execute","target":{"interface":"eth2","address":"10.77.60.11","captureStrategy":"secondary-ip"}}}`
	var out bytes.Buffer
	err := run(context.Background(), strings.NewReader(req), &out, func(_ context.Context, name string, args ...string) (string, error) {
		t.Fatalf("state mode executed %s %v", name, args)
		return "", nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var res executeActionResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if res.Status.Status != statusSucceeded || res.Status.Observed["nodeRef"] != "aws-leaf-a" {
		t.Fatalf("result = %#v", res.Status)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state providerState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if len(state.Assignments) != 1 || state.Assignments[0] != (providerAssignment{NodeRef: "aws-leaf-a", NICRef: "eth2", Address: "10.77.60.11/32"}) {
		t.Fatalf("state = %#v", state)
	}
}

func TestUnassignSecondaryUpdatesProviderState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "provider-state.json")
	t.Setenv(envProviderState, statePath)
	t.Setenv(envSelfNode, "aws-leaf-a")
	if err := updateProviderState(statePath, providerAssignment{NodeRef: "aws-leaf-a", NICRef: "eth2", Address: "10.77.60.11/32"}, true); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	req := `{"spec":{"action":"unassign-secondary-ip","provider":"netns","mode":"execute","target":{"interface":"eth2","address":"10.77.60.11/32"}}}`
	err := run(context.Background(), strings.NewReader(req), &bytes.Buffer{}, func(_ context.Context, name string, args ...string) (string, error) {
		t.Fatalf("state mode executed %s %v", name, args)
		return "", nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state providerState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if len(state.Assignments) != 0 {
		t.Fatalf("state = %#v", state)
	}
}
