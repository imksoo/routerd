// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerationDiffAppliesToOriginalFile(t *testing.T) {
	patch, err := exec.LookPath("patch")
	if err != nil {
		t.Skip("patch executable is unavailable")
	}
	for _, pair := range [][2]string{
		{"", "new\n"},
		{"old\n", ""},
		{"old", "new"},
		{"same\nold\nsame-middle\nold-again\nend\n", "same\nnew\nsame-middle\nnew-again\nend\n"},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte(pair[0]), 0o600); err != nil {
			t.Fatal(err)
		}
		diff, err := unifiedDiff(context.Background(), "config.yaml", "config.yaml", pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(patch, "--batch", "-p0")
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(diff)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cannot apply generated diff: %v\n%s\n%s", err, out, diff)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != pair[1] {
			t.Fatalf("patched config = %q, error = %v; want %q", got, err, pair[1])
		}
	}
}

func TestGenerationDiffPreservesBothConfigs(t *testing.T) {
	for _, pair := range [][2]string{
		{"", "new\n"},
		{"old\n", ""},
		{"common\nold\nsame\nold-again\nend\n", "common\nnew\nsame\nnew-again\nend\n"},
		{"same\n", "same\n"},
	} {
		ops, err := diffLineOps(context.Background(), splitDiffLines(pair[0]), splitDiffLines(pair[1]))
		if err != nil {
			t.Fatal(err)
		}
		var from, to strings.Builder
		for _, op := range ops {
			if op.prefix != '+' {
				from.WriteString(op.line)
			}
			if op.prefix != '-' {
				to.WriteString(op.line)
			}
		}
		if from.String() != pair[0] || to.String() != pair[1] {
			t.Fatalf("diff changed config content: from=%q to=%q", from.String(), to.String())
		}
	}
}

func TestGenerationDiffLargeConfigs(t *testing.T) {
	var from, to strings.Builder
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&from, "old-%d\n", i)
		fmt.Fprintf(&to, "new-%d\n", i)
	}
	diff, err := unifiedDiff(context.Background(), "old.yaml", "new.yaml", from.String(), to.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@@ -1,10000 +1,10000 @@", "-old-9999\n", "+new-9999\n"} {
		if !strings.Contains(diff, want) {
			t.Fatalf("large config diff missing %q", want)
		}
	}
}

func TestGenerationDiffHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := unifiedDiff(ctx, "old.yaml", "new.yaml", "old\n", "new\n"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}
