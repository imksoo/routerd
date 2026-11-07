// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrafficCounterSQLiteRoundTripAndAggregateCompleteness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	log, err := OpenTrafficFlowLog(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	for _, flow := range []TrafficFlow{{FlowKey: "out", StartedAt: time.Now(), Protocol: "udp", BytesOut: counterFixture(0), PacketsIn: counterFixture(4)}, {FlowKey: "in", StartedAt: time.Now(), Protocol: "tcp", BytesIn: counterFixture(9), PacketsOut: counterFixture(0)}} {
		if err := log.UpsertActive(t.Context(), flow); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := OpenTrafficFlowLogReadOnly(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	rows, err := reader.List(t.Context(), TrafficFlowFilter{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v %v", rows, err)
	}
	for _, flow := range rows {
		if flow.FlowKey == "out" && (flow.BytesOut == nil || *flow.BytesOut != 0 || flow.BytesIn != nil || flow.PacketsOut != nil || flow.PacketsIn == nil || *flow.PacketsIn != 4) {
			t.Fatalf("roundtrip invented missing counters: %+v", flow)
		}
	}
	aggregate, err := reader.Aggregate(t.Context(), TrafficFlowFilter{})
	if err != nil || aggregate.TotalBytesOut == nil || *aggregate.TotalBytesOut != 0 || aggregate.TotalBytesIn == nil || *aggregate.TotalBytesIn != 9 || !aggregate.BytesOutIncomplete || !aggregate.BytesInIncomplete {
		t.Fatalf("partial aggregate reported complete: %+v %v", aggregate, err)
	}
	empty, err := reader.Aggregate(t.Context(), TrafficFlowFilter{Since: time.Now().Add(time.Hour)})
	if err != nil || empty.Total != 0 || empty.TotalBytesOut == nil || *empty.TotalBytesOut != 0 || empty.TotalBytesIn == nil || *empty.TotalBytesIn != 0 || empty.BytesOutIncomplete || empty.BytesInIncomplete {
		t.Fatalf("observed empty aggregate=%+v %v", empty, err)
	}
	data, err := json.Marshal(rows)
	if err != nil || strings.Contains(string(data), "accounting") || !strings.Contains(string(data), `"bytesOut":0`) {
		t.Fatalf("wire presence=%s err=%v", data, err)
	}
}

func TestTrafficCounterFormatRejectsPriorZeroEncodingWithoutChangingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE flows(flow_key TEXT,accounting INTEGER,bytes_out INTEGER); INSERT INTO flows VALUES('unobserved',0,0)`); err != nil {
		t.Fatal(err)
	}
	if reader, err := OpenTrafficFlowLogReadOnly(t.Context(), path); err == nil {
		_ = reader.Close()
		t.Fatal("old missing-as-zero format was reinterpreted as observed")
	}
	if writer, err := OpenTrafficFlowLog(t.Context(), path); err == nil {
		_ = writer.Close()
		t.Fatal("writer silently adopted old traffic format")
	}
	var accounting, bytesOut int
	if err := database.QueryRow(`SELECT accounting,bytes_out FROM flows WHERE flow_key='unobserved'`).Scan(&accounting, &bytesOut); err != nil || accounting != 0 || bytesOut != 0 {
		t.Fatalf("format rejection modified old data: %d %d %v", accounting, bytesOut, err)
	}
}
