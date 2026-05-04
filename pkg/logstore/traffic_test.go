package logstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestTrafficFlowLogUpsertAndEndMissing(t *testing.T) {
	log, err := OpenTrafficFlowLog(filepath.Join(t.TempDir(), "traffic-flows.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	flow := TrafficFlow{
		StartedAt:     time.Now().UTC(),
		ClientAddress: "172.18.0.10",
		ClientPort:    12345,
		PeerAddress:   "1.1.1.1",
		PeerPort:      443,
		Protocol:      "tcp",
		BytesOut:      100,
	}
	flow.FlowKey = FlowKey(flow.Protocol, flow.ClientAddress, flow.ClientPort, flow.PeerAddress, flow.PeerPort)
	if err := log.UpsertActive(context.Background(), flow); err != nil {
		t.Fatal(err)
	}
	flow.BytesOut = 200
	if err := log.UpsertActive(context.Background(), flow); err != nil {
		t.Fatal(err)
	}
	if err := log.EndMissing(context.Background(), nil, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	rows, err := log.List(context.Background(), TrafficFlowFilter{Client: "172.18.0.10", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("len = %d", len(rows))
	}
	if rows[0].BytesOut != 200 || rows[0].EndedAt.IsZero() {
		t.Fatalf("flow = %#v", rows[0])
	}
}
