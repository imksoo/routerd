// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/subprocessio"
)

func TestWireGuardFailedDumpDoesNotPrintPrivateKeys(t *testing.T) {
	dir := t.TempDir()
	writeTestCommand(t, filepath.Join(dir, "wg"), "#!/bin/sh\nprintf 'interface-private-key peer-preshared-key'\nprintf 'status read failed' >&2\nexit 1\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, args := range [][]string{{"vpn", "wg", "list"}, {"vpn", "wg", "show", "wg0"}} {
		var stdout, stderr bytes.Buffer
		err := run(args, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "status read failed") {
			t.Fatalf("failed dump was not reported: %v", err)
		}
		if stdout.Len() != 0 || strings.Contains(err.Error(), "interface-private-key") || strings.Contains(err.Error(), "peer-preshared-key") {
			t.Fatalf("failed dump exposed data: stdout=%q error=%v", stdout.String(), err)
		}
	}
}

func TestWireGuardSuccessfulDumpSeparatesStderrAndSecrets(t *testing.T) {
	dir := t.TempDir()
	writeTestCommand(t, filepath.Join(dir, "wg"), "#!/bin/sh\nprintf 'wg0\tinterface-private-key\tinterface-public\t51820\toff\nwg0\tpeer-public\tpeer-preshared-key\t(none)\t10.0.0.2/32\t0\t0\t0\t0\n'\nprintf 'read-only status warning' >&2\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout, stderr bytes.Buffer
	if err := run([]string{"vpn", "wg", "list", "-o", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("valid dump was mixed with stderr: %v", err)
	}
	if !strings.Contains(stdout.String(), "interface-public") || strings.Contains(stdout.String(), "private-key") || strings.Contains(stdout.String(), "preshared-key") || strings.Contains(stdout.String(), "status warning") {
		t.Fatalf("dump data or diagnostic separation lost: %s", stdout.String())
	}
}

func TestCLICommandOutputIsBounded(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload")
	if err := os.WriteFile(payload, bytes.Repeat([]byte("x"), subprocessio.StdoutLimit+1), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "status")
	writeTestCommand(t, path, "#!/bin/sh\ncat \"$1\"\n")
	stdout, _, err := runHostCommand(context.Background(), path, payload)
	var limitErr *subprocessio.OutputLimitError
	if stdout != nil || !errors.As(err, &limitErr) {
		t.Fatalf("oversized native status became a successful partial read: bytes=%d error=%v", len(stdout), err)
	}
}

func TestCLIStatusTimeoutReapsInheritedOutputPipe(t *testing.T) {
	dir := t.TempDir()
	writeTestCommand(t, filepath.Join(dir, "wg"), "#!/bin/sh\n/bin/sleep 30 &\nwait\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	started := time.Now()
	var stdout, stderr bytes.Buffer
	err := run([]string{"vpn", "wg", "list", "--timeout", "100ms"}, &stdout, &stderr)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
		t.Fatalf("bounded status timeout did not reach the owned child group: elapsed=%v error=%v", time.Since(started), err)
	}
}

func TestCandidateReaderUsesActualAPIRequestCeiling(t *testing.T) {
	data := strings.Repeat("x", controlapi.MaxRequestBodyBytes+1)
	if _, err := readCandidateYAML("-", strings.NewReader(data)); err == nil {
		t.Fatal("oversized stdin candidate was read as usable API input")
	}
	path := filepath.Join(t.TempDir(), "oversized.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCandidateYAML(path, nil); err == nil {
		t.Fatal("oversized file candidate was read as usable API input")
	}
	if text, err := readCandidateYAML("-", strings.NewReader("kind: Router\n")); err != nil || text != "kind: Router\n" {
		t.Fatalf("usable candidate changed: %q, %v", text, err)
	}
}
