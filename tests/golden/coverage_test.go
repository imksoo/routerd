// SPDX-License-Identifier: BSD-3-Clause

package golden_test

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestAbstractionLayerCoverageSnapshot(t *testing.T) {
	data, err := os.ReadFile("coverage.txt")
	if err != nil {
		t.Fatal(err)
	}
	minimums := parseCoverageMinimums(t, string(data))
	args := []string{"test", "-cover", "./pkg/servicemgr", "./pkg/firewallbackend", "./pkg/netconfigbackend"}
	cmd := exec.Command("go", args...)
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	actual := parseGoCoverage(t, string(out))
	for pkg, minimum := range minimums {
		got, ok := actual[pkg]
		if !ok {
			t.Fatalf("coverage output missing %s:\n%s", pkg, out)
		}
		if got < minimum {
			t.Fatalf("%s coverage %.1f%% below snapshot minimum %.1f%%\n%s", pkg, got, minimum, out)
		}
	}
}

func parseCoverageMinimums(t *testing.T, text string) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("invalid coverage snapshot line %q", line)
		}
		value, err := strconv.ParseFloat(strings.TrimSuffix(fields[1], "%"), 64)
		if err != nil {
			t.Fatalf("invalid coverage value in %q: %v", line, err)
		}
		out[fields[0]] = value
	}
	return out
}

func parseGoCoverage(t *testing.T, text string) map[string]float64 {
	t.Helper()
	re := regexp.MustCompile(`ok\s+(routerd/pkg/(?:servicemgr|firewallbackend|netconfigbackend))\s+.*coverage:\s+([0-9.]+)%`)
	out := map[string]float64{}
	for _, match := range re.FindAllSubmatch([]byte(text), -1) {
		value, err := strconv.ParseFloat(string(match[2]), 64)
		if err != nil {
			t.Fatalf("invalid go coverage value %q: %v", match[2], err)
		}
		out[string(bytes.TrimSpace(match[1]))] = value
	}
	return out
}
