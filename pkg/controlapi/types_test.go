// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestControllerModeReasonValues(t *testing.T) {
	got := []ControllerModeReason{
		ControllerModeReasonLive,
		ControllerModeReasonManual,
		ControllerModeReasonOSUnsupported,
		ControllerModeReasonDependencyUnmet,
		ControllerModeReasonSpecDisabled,
		ControllerModeReasonUnknown,
	}
	want := []string{
		"Live",
		"Manual",
		"OSUnsupported",
		"DependencyUnmet",
		"SpecDisabled",
		"Unknown",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	seen := map[string]bool{}
	for i, reason := range got {
		if string(reason) != want[i] {
			t.Fatalf("reason[%d] = %q, want %q", i, reason, want[i])
		}
		if seen[string(reason)] {
			t.Fatalf("duplicate reason %q", reason)
		}
		seen[string(reason)] = true
	}
}

func TestRuntimeStatsJSONRoundTrip(t *testing.T) {
	collected := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	lastGC := time.Date(2026, 5, 28, 11, 59, 0, 0, time.UTC)
	in := NewRuntimeStats()
	in.CollectedAt = collected
	in.HeapAllocBytes = 12 * 1024 * 1024
	in.HeapInuseBytes = 14 * 1024 * 1024
	in.HeapObjects = 123456
	in.StackInuseBytes = 2 * 1024 * 1024
	in.SysBytes = 64 * 1024 * 1024
	in.CgroupMemoryCurrentBytes = 2 * 1024 * 1024 * 1024
	in.CgroupMemoryPeakBytes = 3 * 1024 * 1024 * 1024
	in.CgroupAnonBytes = 32 * 1024 * 1024
	in.CgroupFileBytes = 1900 * 1024 * 1024
	in.CgroupActiveFileBytes = 100 * 1024 * 1024
	in.CgroupInactiveFileBytes = 1800 * 1024 * 1024
	in.CgroupKernelBytes = 16 * 1024 * 1024
	in.CgroupSlabBytes = 12 * 1024 * 1024
	in.NumGoroutine = 42
	in.NumGC = 7
	in.GCPauseTotalNs = 5_000_000
	in.LastGC = lastGC
	in.OpenFDs = 18
	in.MaxFDs = 1024

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Field names must remain stable for external tooling.
	for _, field := range []string{
		`"apiVersion"`, `"kind"`, `"collectedAt"`, `"heapAllocBytes"`,
		`"heapInuseBytes"`, `"heapObjects"`, `"stackInuseBytes"`, `"sysBytes"`,
		`"cgroupMemoryCurrentBytes"`, `"cgroupMemoryPeakBytes"`, `"cgroupAnonBytes"`,
		`"cgroupFileBytes"`, `"cgroupActiveFileBytes"`, `"cgroupInactiveFileBytes"`,
		`"cgroupKernelBytes"`, `"cgroupSlabBytes"`,
		`"numGoroutine"`, `"numGC"`, `"gcPauseTotalNs"`, `"lastGC"`,
		`"openFds"`, `"maxFds"`,
	} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("marshalled JSON missing field %s: %s", field, data)
		}
	}

	var out RuntimeStats
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.APIVersion != APIVersion || out.Kind != "RuntimeStats" {
		t.Fatalf("typeMeta = %q/%q", out.APIVersion, out.Kind)
	}
	if !out.CollectedAt.Equal(collected) {
		t.Fatalf("collectedAt = %v, want %v", out.CollectedAt, collected)
	}
	if !out.LastGC.Equal(lastGC) {
		t.Fatalf("lastGC = %v, want %v", out.LastGC, lastGC)
	}
	if out.HeapAllocBytes != in.HeapAllocBytes || out.HeapInuseBytes != in.HeapInuseBytes ||
		out.HeapObjects != in.HeapObjects || out.StackInuseBytes != in.StackInuseBytes ||
		out.SysBytes != in.SysBytes || out.NumGoroutine != in.NumGoroutine ||
		out.CgroupMemoryCurrentBytes != in.CgroupMemoryCurrentBytes ||
		out.CgroupMemoryPeakBytes != in.CgroupMemoryPeakBytes ||
		out.CgroupAnonBytes != in.CgroupAnonBytes ||
		out.CgroupFileBytes != in.CgroupFileBytes ||
		out.CgroupActiveFileBytes != in.CgroupActiveFileBytes ||
		out.CgroupInactiveFileBytes != in.CgroupInactiveFileBytes ||
		out.CgroupKernelBytes != in.CgroupKernelBytes ||
		out.CgroupSlabBytes != in.CgroupSlabBytes ||
		out.NumGC != in.NumGC || out.GCPauseTotalNs != in.GCPauseTotalNs ||
		out.OpenFDs != in.OpenFDs || out.MaxFDs != in.MaxFDs {
		t.Fatalf("round-trip mismatch:\n in = %#v\nout = %#v", in, out)
	}
}
