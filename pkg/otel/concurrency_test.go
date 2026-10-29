// SPDX-License-Identifier: BSD-3-Clause

package otel

import (
	"sync"
	"testing"
)

func TestZeroRuntimeInitializesConcurrentInstruments(t *testing.T) {
	runtime := &Runtime{ServiceName: "concurrent-metrics"}
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			for range 20 {
				runtime.Counter("events")
				runtime.Gauge("state")
				runtime.Float64Gauge("ratio")
			}
		})
	}
	workers.Wait()
	if len(runtime.counters) != 1 || len(runtime.gauges) != 1 || len(runtime.float64Gauges) != 1 {
		t.Fatal("shared runtime did not retain its instruments")
	}
}
