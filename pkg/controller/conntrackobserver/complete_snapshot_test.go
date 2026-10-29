// SPDX-License-Identifier: BSD-3-Clause

package conntrackobserver

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
)

func TestFlowReconciliationCannotEndEntryBeyondDisplayLimit(t *testing.T) {
	const count = 2
	path := filepath.Join(t.TempDir(), "flows.db")
	entries := make([]observe.ConnectionEntry, count)
	for i := range entries {
		entries[i] = observe.ConnectionEntry{Protocol: "tcp", Original: observe.ConntrackTuple{Source: "192.0.2.10", SourcePort: strconv.Itoa(i + 1), Destination: "198.51.100.20", DestinationPort: "443"}}
	}
	log, err := logstore.OpenTrafficFlowLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	last := trafficFlowFromConnection(entries[count-1], time.Now().UTC())
	if err := log.UpsertActive(t.Context(), last); err != nil {
		t.Fatal(err)
	}
	controller := &Controller{Store: &testStore{}, Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TrafficFlowLog"}, Metadata: api.ObjectMeta{Name: "flows"}, Spec: api.TrafficFlowLogSpec{Enabled: true, Path: path}}}}}, Connections: func(_ context.Context, limit int) (*observe.ConnectionTable, error) {
		selected := entries
		if limit >= 0 {
			// A bounded display selection omits the tail row. Only the complete
			// observation includes both still-active flows.
			selected = selected[:1]
		}
		return &observe.ConnectionTable{Count: count, Entries: selected}, nil
	}}
	if err := controller.recordTrafficFlows(t.Context(), count); err != nil {
		t.Fatal(err)
	}
	flows, err := log.List(t.Context(), logstore.TrafficFlowFilter{Limit: count + 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, flow := range flows {
		if flow.FlowKey == last.FlowKey {
			if !flow.EndedAt.IsZero() {
				t.Fatalf("still-active tail flow was ended by display truncation: %+v", flow)
			}
			return
		}
	}
	t.Fatal("known active flow disappeared")
}
