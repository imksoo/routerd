// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
)

func withRuntimeStub(t *testing.T, stats *controlapi.RuntimeStats, fetchErr error) {
	t.Helper()
	prev := runtimeStatsFetcher
	runtimeStatsFetcher = func(socketPath string, timeout time.Duration) (*controlapi.RuntimeStats, error) {
		if fetchErr != nil {
			return nil, fetchErr
		}
		return stats, nil
	}
	t.Cleanup(func() { runtimeStatsFetcher = prev })
}

func runDoctorRuntime(t *testing.T) doctorReport {
	t.Helper()
	configPath, statePath := writeDoctorFixture(t)
	var out bytes.Buffer
	if err := run([]string{"doctor", "runtime", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor runtime: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	return report
}

func TestDoctorRuntimePassesAndSummarizesFootprint(t *testing.T) {
	stats := controlapi.NewRuntimeStats()
	stats.CollectedAt = time.Now().UTC()
	stats.HeapAllocBytes = 18 * 1024 * 1024
	stats.HeapObjects = 123456
	stats.NumGoroutine = 250
	stats.NumGC = 9
	stats.OpenFDs = 42
	stats.MaxFDs = 1024
	stats.CgroupMemoryCurrentBytes = 2473160704
	stats.CgroupAnonBytes = 27832320
	stats.CgroupFileBytes = 2422931456
	stats.CgroupInactiveFileBytes = 2386235392
	withRuntimeStub(t, &stats, nil)

	report := runDoctorRuntime(t)
	if len(report.Checks) != 1 {
		t.Fatalf("checks = %d:\n%#v", len(report.Checks), report.Checks)
	}
	check := report.Checks[0]
	if check.Area != "runtime" {
		t.Fatalf("area = %q", check.Area)
	}
	if check.Status != doctorPass {
		t.Fatalf("status = %q, want pass; detail=%q", check.Status, check.Detail)
	}
	for _, want := range []string{"heapAlloc=18.0MiB", "heapObjects=123456", "numGoroutine=250", "numGC=9", "openFds=42/1024", "cgroupCurrent=2358.6MiB", "cgroup memory is mostly file cache"} {
		if !strings.Contains(check.Detail, want) {
			t.Fatalf("detail missing %q: %q", want, check.Detail)
		}
	}
}

func TestDoctorRuntimeWarnsOnHighGoroutineCount(t *testing.T) {
	stats := controlapi.NewRuntimeStats()
	stats.HeapAllocBytes = 20 * 1024 * 1024
	stats.NumGoroutine = 20000
	stats.OpenFDs = 30
	stats.MaxFDs = 1024
	withRuntimeStub(t, &stats, nil)

	report := runDoctorRuntime(t)
	if len(report.Checks) != 1 || report.Checks[0].Status != doctorWarn {
		t.Fatalf("expected single warn, got %#v", report.Checks)
	}
	if !strings.Contains(report.Checks[0].Detail, "unusually high goroutine count") {
		t.Fatalf("detail missing goroutine warning: %q", report.Checks[0].Detail)
	}
}

func TestDoctorRuntimeWarnsOnFDPressure(t *testing.T) {
	stats := controlapi.NewRuntimeStats()
	stats.HeapAllocBytes = 5 * 1024 * 1024
	stats.NumGoroutine = 120
	stats.OpenFDs = 900
	stats.MaxFDs = 1024
	withRuntimeStub(t, &stats, nil)

	report := runDoctorRuntime(t)
	if len(report.Checks) != 1 || report.Checks[0].Status != doctorWarn {
		t.Fatalf("expected single warn, got %#v", report.Checks)
	}
	if !strings.Contains(report.Checks[0].Detail, "fd usage >=80%") {
		t.Fatalf("detail missing fd warning: %q", report.Checks[0].Detail)
	}
}

func TestDoctorRuntimeSkipsOnFetchError(t *testing.T) {
	withRuntimeStub(t, nil, errors.New("connection refused"))

	report := runDoctorRuntime(t)
	if len(report.Checks) != 1 || report.Checks[0].Status != doctorSkip {
		t.Fatalf("expected skip, got %#v", report.Checks)
	}
	if !strings.Contains(report.Checks[0].Detail, "connection refused") {
		t.Fatalf("detail = %q", report.Checks[0].Detail)
	}
}
