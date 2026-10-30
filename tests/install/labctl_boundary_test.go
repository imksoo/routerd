// SPDX-License-Identifier: BSD-3-Clause

package install_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLabctlExplicitPassCannotPromoteMissingOrFailedEvidence(t *testing.T) {
	for _, sample := range []struct {
		name, forced, required string
		matrix                 string
		want                   string
	}{
		{"missing", "pass", `[]`, "", "fail"},
		{"failed", "pass", `[]`, "fail", "fail"},
		{"unproven-required", "pass", `["ownership_epoch_bumped"]`, "pass", "fail"},
		{"proven", "pass", `["source_ip_preserved"]`, "pass", "pass"},
		{"forced-failure", "fail", `[]`, "pass", "fail"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			dir := t.TempDir()
			stateDir, outDir := filepath.Join(dir, "state"), filepath.Join(dir, "out")
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			const runID = "20261029T000000Z-cloudedge-evidence"
			if err := os.WriteFile(filepath.Join(stateDir, runID+".manifest"), []byte("ttl=1h\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"evidence", "collect", "--out", outDir, "--run-id", runID, "--scenario", "evidence", "--commit", "offline", "--result", sample.forced, "--required-assertions", sample.required}
			if sample.matrix != "" {
				var flows []map[string]string
				for _, src := range []string{"aws", "azure", "oci", "onprem"} {
					for _, dst := range []string{"aws", "azure", "oci", "onprem"} {
						if src != dst {
							flows = append(flows, map[string]string{"src": src, "dst": dst, "result": "pass", "sourceIpPreserved": "pass", "defaultGwUnchanged": "pass", "noNat": "pass"})
						}
					}
				}
				if sample.matrix == "fail" {
					flows[0]["result"] = "fail"
				}
				data, err := json.Marshal(map[string]any{"flows": flows, "summary": map[string]any{"total": 12, "result": sample.matrix}})
				if err != nil {
					t.Fatal(err)
				}
				matrix := filepath.Join(dir, "matrix.json")
				if err := os.WriteFile(matrix, data, 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--matrix-json", matrix)
			}
			out, err := labctlCommand(t, stateDir, "1", args...).CombinedOutput()
			if err != nil {
				t.Fatalf("evidence assembly failed: %v\n%s", err, out)
			}
			data, err := os.ReadFile(filepath.Join(outDir, "result.json"))
			if err != nil {
				t.Fatal(err)
			}
			var result struct{ Result string }
			if err := json.Unmarshal(data, &result); err != nil || result.Result != sample.want {
				t.Fatalf("result=%s want=%s err=%v\n%s", result.Result, sample.want, err, data)
			}
			summary, err := os.ReadFile(filepath.Join(outDir, "summary.md"))
			if err != nil || !strings.Contains(string(summary), "- result: "+sample.want) {
				t.Fatalf("summary disagreed with verdict: %v\n%s", err, summary)
			}
		})
	}
}

func labctlCommand(t *testing.T, stateDir, dry string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(repoRoot(t), "scripts", "cloudedge-labctl.sh")}, args...)...)
	cmd.Env = append(os.Environ(), "CE_STATE_DIR="+stateDir, "CE_DRY_RUN="+dry)
	return cmd
}

func TestLabctlUnimplementedLiveOperationsFailBeforeEffects(t *testing.T) {
	for _, args := range [][]string{
		{"up", "--profile", "full"},
		{"deploy", "--commit", "HEAD"},
		{"failover", "--provider", "aws", "--fault", "stop-active"},
		{"failover", "--provider", "azure", "--fault", "drain"},
		{"failover", "--provider", "oci", "--fault", "heartbeat-stop"},
		{"failover", "--provider", "aws", "--fault", "executor-fail"},
		{"failover", "--provider", "aws", "--fault", "stale-replay"},
	} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			dir := t.TempDir()
			stateDir, binDir, log := filepath.Join(dir, "state"), filepath.Join(dir, "bin"), filepath.Join(dir, "effects.log")
			for _, name := range []string{"aws", "az", "oci", "ssh", "make", "git"} {
				writeExecutable(t, filepath.Join(binDir, name), "#!/bin/sh\nprintf '%s\\n' unexpected-effect >> \"$LABCTL_EFFECT_LOG\"\nexit 99\n")
			}
			cmd := labctlCommand(t, stateDir, "0", args...)
			cmd.Env = append(cmd.Env, "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"), "LABCTL_EFFECT_LOG="+log)
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "unsupported") {
				t.Fatalf("unimplemented live operation succeeded: %v\n%s", err, out)
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatalf("live operation crossed the native boundary: %v", err)
			}
			if entries, err := os.ReadDir(stateDir); err == nil && len(entries) != 0 {
				t.Fatalf("unsupported operation published state: %v", entries)
			}
		})
	}
}

func TestLabctlDefaultOrderAndTeardownRetainManifest(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	out, err := labctlCommand(t, stateDir, "1", "up", "--profile", "minimal").CombinedOutput()
	if err != nil {
		t.Fatalf("default declared order failed: %v\n%s", err, out)
	}
	manifests, err := filepath.Glob(filepath.Join(stateDir, "*.manifest"))
	if err != nil || len(manifests) != 1 {
		t.Fatalf("simulation manifest missing: %v %v", manifests, err)
	}
	manifest := manifests[0]
	data, err := os.ReadFile(manifest)
	if err != nil || !strings.Contains(string(data), "providers=onprem,aws\n") {
		t.Fatalf("declared provider order changed: %v\n%s", err, data)
	}
	// Teardown cannot forget the only resource-selection receipt without an
	// observed successful provider operation, including a simulated operation.
	runID := strings.TrimSuffix(filepath.Base(manifest), ".manifest")
	data = []byte("run_id=" + runID + "\nttl_expires_at=2000-01-01T00:00:00Z\nproviders=aws\n")
	if err := os.WriteFile(manifest, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		dry  string
		args []string
	}{
		{"0", []string{"down", "--run-id", runID}},
		{"0", []string{"down", "--expired"}},
		{"1", []string{"down", "--run-id", runID}},
	} {
		out, err := labctlCommand(t, stateDir, sample.dry, sample.args...).CombinedOutput()
		if (err == nil) != (sample.dry == "1") {
			t.Fatalf("unexpected teardown outcome: %v\n%s", err, out)
		}
		after, err := os.ReadFile(manifest)
		if err != nil || string(after) != string(data) {
			t.Fatalf("incomplete teardown discarded manifest: %v\n%s", err, out)
		}
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if out, err := labctlCommand(t, stateDir, "0", "down", "--expired").CombinedOutput(); err != nil {
		t.Fatalf("empty managed inventory should be a no-op: %v\n%s", err, out)
	}
}

func TestAcceptanceStopsWhenLiveFaultWasNotInjected(t *testing.T) {
	dir := t.TempDir()
	scenarios, outDir, marker := filepath.Join(dir, "scenarios.json"), filepath.Join(dir, "evidence"), filepath.Join(dir, "matrix-ran")
	if err := os.WriteFile(scenarios, []byte(`{"scenarios":[{"id":"fault","fault":{"type":"drain","provider":"aws"},"matrix":{"name":"d3"},"expectedAssertions":["source_ip_preserved"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := filepath.Join(dir, "matrix-runner")
	writeExecutable(t, runner, "#!/bin/sh\ntouch \"$ACCEPTANCE_MATRIX_MARKER\"\nexit 0\n")
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "cloudedge-acceptance.sh"), "run", "--scenario", "fault", "--out", outDir)
	cmd.Env = append(os.Environ(), "CE_DRY_RUN=0", "CE_ACCEPTANCE_SCENARIOS="+scenarios, "CE_STATE_DIR="+filepath.Join(dir, "state"), "MATRIX_RUNNER="+runner, "ACCEPTANCE_MATRIX_MARKER="+marker)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "unsupported") {
		t.Fatalf("actual acceptance caller accepted an uninjected fault: %v\n%s", err, out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("matrix ran without the required fault: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "result.json")); !os.IsNotExist(err) {
		t.Fatalf("acceptance published success evidence after fault failure: %v", err)
	}
}
