// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/controlapi"
)

func TestApplyCgroupMemoryStatsReadsCgroupV2MemoryStat(t *testing.T) {
	dir := t.TempDir()
	procCgroup := filepath.Join(dir, "cgroup")
	cgroupRoot := filepath.Join(dir, "sys/fs/cgroup")
	serviceDir := filepath.Join(cgroupRoot, "system.slice/routerd.service")
	if err := os.MkdirAll(serviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(procCgroup, []byte("0::/system.slice/routerd.service\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serviceDir, "memory.current"), []byte("2473160704\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serviceDir, "memory.peak"), []byte("2550173696\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serviceDir, "memory.stat"), []byte(`anon 27832320
file 2422931456
active_file 36667392
inactive_file 2386235392
kernel 22396928
slab 21589328
`), 0o644); err != nil {
		t.Fatal(err)
	}

	stats := controlapi.NewRuntimeStats()
	applyCgroupMemoryStats(&stats, procCgroup, cgroupRoot)

	if stats.CgroupMemoryCurrentBytes != 2473160704 || stats.CgroupMemoryPeakBytes != 2550173696 {
		t.Fatalf("memory current/peak = %d/%d", stats.CgroupMemoryCurrentBytes, stats.CgroupMemoryPeakBytes)
	}
	if stats.CgroupAnonBytes != 27832320 ||
		stats.CgroupFileBytes != 2422931456 ||
		stats.CgroupActiveFileBytes != 36667392 ||
		stats.CgroupInactiveFileBytes != 2386235392 ||
		stats.CgroupKernelBytes != 22396928 ||
		stats.CgroupSlabBytes != 21589328 {
		t.Fatalf("cgroup stats = %#v", stats)
	}
}
