// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package chain

import (
	"os"
	"testing"
)

func TestFreeBSDSupervisedProcessInventoryIncludesCurrentProcess(t *testing.T) {
	for _, pid := range []int{0, os.Getpid()} {
		processes, err := listFreeBSDSupervisedDaemonProcesses(supervisedDaemonMarker{PID: pid})
		if err != nil {
			t.Fatalf("native process inventory for recorded PID %d: %v", pid, err)
		}
		found := false
		for _, process := range processes {
			if process.PID <= 0 {
				t.Fatalf("kernel process became a managed-process candidate: %d", process.PID)
			}
			if process.PID == os.Getpid() {
				found = true
			}
		}
		if !found {
			t.Fatalf("native inventory omitted the current process for recorded PID %d", pid)
		}
	}
}
