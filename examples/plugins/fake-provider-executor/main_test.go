// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func decodeResult(t *testing.T, out []byte) executeActionResult {
	t.Helper()
	var res executeActionResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("decode result: %v\n%s", err, out)
	}
	return res
}

func runFake(t *testing.T, req executeActionRequest) executeActionResult {
	t.Helper()
	in, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(bytes.NewReader(in), &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	return decodeResult(t, out.Bytes())
}

func TestFakeSucceeds(t *testing.T) {
	res := runFake(t, executeActionRequest{Spec: executeActionRequestSpec{
		Action:         "assign-secondary-ip",
		Provider:       "aws",
		Target:         map[string]string{"address": "10.0.0.5/32"},
		Mode:           "execute",
		IdempotencyKey: "k1",
	}})
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q", res.Status.Status)
	}
	if !res.Status.UndoAvailable {
		t.Errorf("assign-secondary-ip must set UndoAvailable=true")
	}
	if res.Status.Observed["assignedAddress"] != "10.0.0.5/32" {
		t.Errorf("want observed assignedAddress, got %v", res.Status.Observed)
	}
}

func TestFakeDryRunNoSideEffect(t *testing.T) {
	res := runFake(t, executeActionRequest{Spec: executeActionRequestSpec{Action: "assign-secondary-ip", Mode: "dry-run", IdempotencyKey: "k1"}})
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q", res.Status.Status)
	}
	if !strings.HasPrefix(res.Status.Message, "would ") {
		t.Errorf("dry-run message should start with 'would ', got %q", res.Status.Message)
	}
	if len(res.Status.Observed) != 0 {
		t.Errorf("dry-run must report no observed facts, got %v", res.Status.Observed)
	}
}

func TestFakeOutcomeFailedAndSkipped(t *testing.T) {
	res := runFake(t, executeActionRequest{Spec: executeActionRequestSpec{Action: "x", Mode: "execute", IdempotencyKey: "k", Parameters: map[string]string{"fakeOutcome": "failed"}}})
	if res.Status.Status != statusFailed {
		t.Fatalf("want failed, got %q", res.Status.Status)
	}
	res = runFake(t, executeActionRequest{Spec: executeActionRequestSpec{Action: "x", Mode: "execute", IdempotencyKey: "k", Parameters: map[string]string{"fakeOutcome": "skipped"}}})
	if res.Status.Status != statusSkipped {
		t.Fatalf("want skipped, got %q", res.Status.Status)
	}
}

func TestFakeEnvOutcome(t *testing.T) {
	t.Setenv("FAKE_OUTCOME", "failed")
	res := runFake(t, executeActionRequest{Spec: executeActionRequestSpec{Action: "x", Mode: "execute", IdempotencyKey: "k"}})
	if res.Status.Status != statusFailed {
		t.Fatalf("want failed via env, got %q", res.Status.Status)
	}
}

// TestNoExecImports is the no-real-cloud invariant for this executor: its
// shipped code must import neither os/exec, nor any network/RPC package, nor any
// cloud SDK. We assert it statically by parsing every non-test .go file in this
// directory.
func TestNoExecImports(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	dir := filepath.Dir(thisFile)
	forbidden := []string{
		"os/exec",
		"net",
		"net/http",
		"net/rpc",
		"github.com/aws/",
		"github.com/Azure/",
		"github.com/oracle/oci-go-sdk",
	}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatal(perr)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if p == bad || strings.HasPrefix(p, bad) {
					t.Errorf("%s imports forbidden package %q (no-real-cloud invariant)", name, p)
				}
			}
		}
	}
}
