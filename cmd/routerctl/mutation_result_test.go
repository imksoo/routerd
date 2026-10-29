// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestRollbackListDoesNotLabelFailedLatestAttemptCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, phase := range []string{"Committed", "Drifted"} {
		generation, err := store.BeginGeneration(phase)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.RecordGenerationConfig(generation, testRouterYAML(phase)); err != nil {
			t.Fatal(err)
		}
		if err := store.FinishGeneration(generation, phase, nil); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := listRollbackGenerations(path, 10, &output); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if strings.Contains(text, "(current)") || !strings.Contains(text, "Drifted") || !strings.Contains(text, "Committed") {
		t.Fatalf("rollback history fabricated current authority or lost evidence: %s", text)
	}
	for _, row := range strings.Split(text, "\n") {
		if strings.Contains(row, "(latest attempt)") && !strings.Contains(row, "Drifted") {
			t.Fatalf("latest attempt label attached to wrong generation: %s", row)
		}
	}
	if !strings.Contains(text, "(latest attempt)") {
		t.Fatalf("rollback history lost latest attempt marker: %s", text)
	}
}

func TestApplyTransportFailureDoesNotClaimServerState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "candidate.yaml")
	if err := os.WriteFile(path, []byte(testRouterYAML("candidate")), 0o600); err != nil {
		t.Fatal(err)
	}
	err := applyCommand([]string{"--socket", filepath.Join(dir, "missing.sock"), "-f", path}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "outcome is unconfirmed") || strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("missing mutation acknowledgement fabricated server state: %v", err)
	}
}

func TestMutationCommandsRejectUncommittedReplies(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, "candidate.yaml")
	if err := os.WriteFile(candidate, []byte(testRouterYAML("candidate")), 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "state.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	generation, err := store.BeginGeneration("candidate")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordGenerationConfig(generation, testRouterYAML("candidate")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "routerd.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("CLI transport fixture requires Unix sockets: %v", err)
	}
	server := &http.Server{Handler: controlapi.Handler{
		Apply: func(*http.Request, controlapi.ApplyRequest) (*controlapi.ApplyResult, error) {
			result := controlapi.NewApplyResult(&apply.Result{Phase: "Blocked", Generation: generation})
			return &result, nil
		},
		Delete: func(*http.Request, controlapi.DeleteRequest) (*controlapi.DeleteResult, error) {
			return &controlapi.DeleteResult{Result: &apply.Result{Phase: "Blocked", Generation: generation}}, nil
		},
	}}
	go func() { _ = server.Serve(listener) }()
	defer server.Shutdown(context.Background())
	for _, args := range [][]string{
		{"apply", "--socket", socket, "-f", candidate},
		{"delete", "--socket", socket, "Hostname/appliance"},
		{"rollback", "--socket", socket, "--state-file", statePath, "--to", "1"},
	} {
		var output bytes.Buffer
		err := run(args, &output, &bytes.Buffer{})
		var exitErr commandExitError
		if !errors.As(err, &exitErr) || exitErr.Code != 1 || !strings.Contains(output.String(), `"phase": "Blocked"`) {
			t.Fatalf("%s discarded failure diagnostics or returned success: %v %s", args[0], err, output.String())
		}
	}
}

func TestMutationResultPolicyRejectsUnconfirmedResults(t *testing.T) {
	for _, operation := range []string{"apply", "delete", "rollback"} {
		for _, phase := range []string{"Blocked", "Drifted", "Error", ""} {
			err := mutationResultError(operation, &apply.Result{Phase: phase})
			var exitErr commandExitError
			if !errors.As(err, &exitErr) || exitErr.Code != 1 {
				t.Fatalf("%s phase %q returned success: %v", operation, phase, err)
			}
		}
		for _, phase := range []string{"Healthy", "Applied", "Committed", "Standby"} {
			if err := mutationResultError(operation, &apply.Result{Phase: phase}); err != nil {
				t.Fatalf("%s phase %q lost its supported outcome: %v", operation, phase, err)
			}
		}
		if err := mutationResultError(operation, nil); err == nil {
			t.Fatalf("%s accepted a missing result", operation)
		}
	}
}
