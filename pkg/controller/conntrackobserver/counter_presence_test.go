// SPDX-License-Identifier: BSD-3-Clause

package conntrackobserver

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestActualTrafficProducerRetainsKnownCounterAndUnknownOtherDirection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	controller := Controller{Store: &testStore{}, Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TrafficFlowLog"}, Metadata: api.ObjectMeta{Name: "capture"}, Spec: api.TrafficFlowLogSpec{Enabled: true, Path: path}}}}}, Connections: func(context.Context, int) (*observe.ConnectionTable, error) {
		return &observe.ConnectionTable{Entries: []observe.ConnectionEntry{{Protocol: "udp", Original: observe.ConntrackTuple{Source: "192.0.2.1", Destination: "198.51.100.1", SourcePort: "1234", DestinationPort: "53", Bytes: counterFixture(0)}, Reply: observe.ConntrackTuple{Source: "198.51.100.1", Destination: "192.0.2.1", Packets: counterFixture(4)}}}}, nil
	}}
	if err := controller.recordTrafficFlows(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	reader, err := logstore.OpenTrafficFlowLogReadOnly(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	rows, err := reader.List(t.Context(), logstore.TrafficFlowFilter{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("stored producer rows=%+v %v", rows, err)
	}
	row := rows[0]
	if row.BytesOut == nil || *row.BytesOut != 0 || row.BytesIn != nil || row.PacketsOut != nil || row.PacketsIn == nil || *row.PacketsIn != 4 {
		t.Fatalf("producer/SQLite promoted unknown counters: %+v", row)
	}
}

func TestTrafficMetricsKeepUnknownDirectionAbsentAndPreserveOwnedBaseline(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(previous); _ = provider.Shutdown(context.Background()) })
	controller := Controller{}
	value := int64(0)
	entry := observe.ConnectionEntry{Protocol: "udp", Original: observe.ConntrackTuple{Source: "192.0.2.1", Destination: "198.51.100.1", Bytes: &value}}
	controller.recordTrafficMetrics(t.Context(), []observe.ConnectionEntry{entry})
	collect := func() (map[string]int64, map[string]int64) {
		t.Helper()
		var result metricdata.ResourceMetrics
		if err := reader.Collect(t.Context(), &result); err != nil {
			t.Fatal(err)
		}
		bytesByDirection, flowsByProtocol := map[string]int64{}, map[string]int64{}
		for _, scope := range result.ScopeMetrics {
			for _, measurement := range scope.Metrics {
				switch measurement.Name {
				case "routerd.connection.bytes":
					if sum, ok := measurement.Data.(metricdata.Sum[int64]); ok {
						for _, point := range sum.DataPoints {
							direction, _ := point.Attributes.Value("network.direction")
							bytesByDirection[direction.AsString()] = point.Value
						}
					}
				case "routerd.connection.flow.protocol":
					if gauge, ok := measurement.Data.(metricdata.Gauge[int64]); ok {
						for _, point := range gauge.DataPoints {
							protocol, _ := point.Attributes.Value("network.protocol.name")
							flowsByProtocol[protocol.AsString()] = point.Value
						}
					}
				}
			}
		}
		return bytesByDirection, flowsByProtocol
	}
	directions, _ := collect()
	if value, ok := directions["out"]; !ok || value != 0 {
		t.Fatalf("observed zero missing: %v", directions)
	}
	if _, ok := directions["in"]; ok {
		t.Fatalf("unknown reply direction invented a zero metric: %v", directions)
	}
	value = 100
	controller.recordTrafficMetrics(t.Context(), []observe.ConnectionEntry{entry})
	entry.Original.Bytes = nil
	controller.recordTrafficMetrics(t.Context(), []observe.ConnectionEntry{entry})
	value = 120
	entry.Original.Bytes = &value
	controller.recordTrafficMetrics(t.Context(), []observe.ConnectionEntry{entry})
	directions, _ = collect()
	if directions["out"] != 120 {
		t.Fatalf("missing observation reset/aliased baseline: %v", directions)
	}
	controller.recordTrafficMetrics(t.Context(), nil)
	_, flows := collect()
	if value, ok := flows["udp"]; !ok || value != 0 {
		t.Fatalf("observed empty snapshot retained stale flow gauge: %v", flows)
	}
}
