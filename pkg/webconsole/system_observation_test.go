// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import "testing"

func TestMemoryObservationKeepsZeroAvailableDistinctFromMissing(t *testing.T) {
	// MemAvailable is the kernel's reclaimability estimate; summing free and
	// cache fields cannot replace an observed zero (or a failed estimate).
	data := []byte("MemTotal: 1024 kB\nMemAvailable: 0 kB\nMemFree: 32 kB\nBuffers: 64 kB\nCached: 128 kB\n")
	total, available, ok := parseMemoryInfo(data)
	if !ok || total != 1024*1024 || available != 0 {
		t.Fatalf("zero availability replaced by a guessed value: total=%d available=%d ok=%v", total, available, ok)
	}
	for _, data := range []string{
		"MemTotal: 1024 kB\nMemFree: 32 kB\nCached: 128 kB\n",
		"MemTotal: 1024 kB\nMemAvailable: 2048 kB\n",
		"MemTotal: 1024 kB\nMemAvailable: broken kB\n",
	} {
		if _, _, ok := parseMemoryInfo([]byte(data)); ok {
			t.Fatalf("unavailable memory estimate became a current observation: %q", data)
		}
	}
}

func TestCPUObservationDoesNotDoubleCountGuestTime(t *testing.T) {
	// kernel/sched/cputime.c account_guest_time adds the same guest duration
	// to CPUTIME_USER/NICE and CPUTIME_GUEST/GUEST_NICE. The guest columns
	// must not increase the total a second time.
	cpu, ok := parseCPUTimes([]byte("cpu 100 20 30 400 50 6 7 8 90 15\ncpu0 1 2 3 4\n"))
	if !ok || cpu.total != 621 || cpu.idle != 450 {
		t.Fatalf("guest time counted twice: cpu=%+v ok=%v", cpu, ok)
	}
}
