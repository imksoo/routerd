// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoActionPlanExecutionPathInCore is the no-execution invariant guard for
// Phase 4.1: routerd persists and displays plugin-proposed ActionPlans but must
// NEVER execute one and must NEVER invoke a provider CLI/SDK from core.
//
// There is no ActionPlan executor in the codebase by design. This test fails if
// a future change introduces an obvious executor entry point (a function whose
// name pairs an execute/apply/run/invoke verb with ActionPlan) anywhere under
// pkg/ or cmd/. ActionPlans are data only.
func TestNoActionPlanExecutionPathInCore(t *testing.T) {
	root := repoRoot(t)
	// Catch executor-style identifiers where a mutating verb is directly joined
	// to "ActionPlan", e.g. executeActionPlan, applyActionPlan, invokeActionPlan,
	// provisionActionPlan, ActionPlanExecutor, ActionPlanApplier. The Phase 4.1
	// invariant is data-only persistence: routerd never executes an ActionPlan.
	//
	// The verb must be immediately adjacent to "actionplan" (no intervening
	// words) so this targets executor entry points, not data plumbing such as
	// validation, persistence, or display. Test files are excluded: test helpers
	// legitimately construct ActionPlan values, and the invariant is about core.
	pattern := regexp.MustCompile(`(?i)(execute|apply|applies|invoke|perform|provision|deprovision|dispatch|enact|mutate)actionplan|actionplan(executor|applier|invoker|dispatcher|mutator|runner)`)
	dirs := []string{filepath.Join(root, "pkg"), filepath.Join(root, "cmd")}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			// Exclude all test files: the invariant guards core, and test
			// scaffolding legitimately builds ActionPlan values.
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if loc := pattern.FindIndex(data); loc != nil {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("possible ActionPlan execution path introduced in %s: %q — ActionPlans are dry-run/display-only and must never be executed by routerd core", rel, string(data[loc[0]:loc[1]]))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// repoRoot walks up from the test's working directory to the module root (the
// directory containing go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}
