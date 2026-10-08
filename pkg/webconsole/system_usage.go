// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type systemUsageSampler struct {
	mu      sync.Mutex
	prevCPU cpuTimes
}

type cpuTimes struct {
	total uint64
	idle  uint64
}

func (h Handler) readSystemUsage() SystemUsage {
	if h.systemUsage == nil {
		return SystemUsage{}
	}
	usage := h.systemUsage.sample()
	if disk, ok := diskUsage("/"); ok {
		usage.Disks = append(usage.Disks, disk)
	}
	return usage
}

func (s *systemUsageSampler) sample() SystemUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	usage := SystemUsage{}
	if load, ok := readLoad1(); ok {
		usage.Load1 = &load
	}
	if total, available, ok := readMemoryInfo(); ok && total > 0 {
		used := total - available
		usage.MemoryTotalBytes = total
		usage.MemoryUsedBytes = used
		percent := float64(used) / float64(total)
		usage.MemoryUsedPercent = &percent
	}
	if current, ok := readCPUTimes(); ok {
		if s.prevCPU.total > 0 && current.total > s.prevCPU.total {
			totalDelta := current.total - s.prevCPU.total
			idleDelta := current.idle - s.prevCPU.idle
			if totalDelta > 0 && idleDelta <= totalDelta {
				percent := float64(totalDelta-idleDelta) / float64(totalDelta)
				usage.CPUPercent = &percent
			}
		}
		s.prevCPU = current
	}
	return usage
}

func readCPUTimes() (cpuTimes, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuTimes{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		values := make([]uint64, 0, len(fields)-1)
		for _, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuTimes{}, false
			}
			values = append(values, value)
		}
		total := uint64(0)
		for _, value := range values {
			total += value
		}
		idle := values[3]
		if len(values) > 4 {
			idle += values[4]
		}
		return cpuTimes{total: total, idle: idle}, true
	}
	return cpuTimes{}, false
}

func readLoad1() (float64, bool) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func readMemoryInfo() (total uint64, available uint64, ok bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	values := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		values[key] = value * 1024
	}
	total = values["MemTotal"]
	available = values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	return total, available, total > 0
}

func diskUsage(path string) (DiskUsage, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return DiskUsage{}, false
	}
	if stat.Bsize <= 0 || stat.Blocks <= 0 || stat.Bavail < 0 {
		return DiskUsage{}, false
	}
	blockSize := uint64(stat.Bsize)
	blocks := uint64(stat.Blocks)
	available := uint64(stat.Bavail)
	maxUint := ^uint64(0)
	if blocks > maxUint/blockSize || available > maxUint/blockSize {
		return DiskUsage{}, false
	}
	total := blocks * blockSize
	free := available * blockSize
	if total == 0 || free > total {
		return DiskUsage{}, false
	}
	used := total - free
	percent := float64(used) / float64(total)
	return DiskUsage{Path: path, UsedBytes: used, TotalBytes: total, UsedPercent: &percent}, true
}
