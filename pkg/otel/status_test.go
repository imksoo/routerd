// SPDX-License-Identifier: BSD-3-Clause

package otel

import (
	"context"
	"testing"

	routerstate "github.com/imksoo/routerd/pkg/state"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRecordStatusMetricsEmitsMobilityOwnershipEpoch(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(previous)

	RecordStatusMetrics(context.Background(), []routerstate.ObjectStatus{{
		APIVersion: "mobility.routerd.net/v1alpha1",
		Kind:       "MobilityPool",
		Name:       "cloudedge",
		Status: map[string]any{
			"phase": "Projected",
			"ownershipMap": []map[string]any{{
				"address":        "10.88.60.10/32",
				"ownerNode":      "azure-router-a",
				"ownershipEpoch": int64(7),
			}},
		},
	}}, nil, nil)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !hasInt64GaugePoint(rm, "routerd.mobility.ownership.epoch", 7) {
		t.Fatalf("routerd.mobility.ownership.epoch gauge with value 7 not found: %+v", rm)
	}
}

func hasInt64GaugePoint(rm metricdata.ResourceMetrics, name string, value int64) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			gauge, ok := metric.Data.(metricdata.Gauge[int64])
			if !ok {
				return false
			}
			for _, point := range gauge.DataPoints {
				if point.Value == value {
					return true
				}
			}
		}
	}
	return false
}
