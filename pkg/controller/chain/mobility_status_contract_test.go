// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

// This table fixes notification, changedFields and severity before moving the
// MobilityPool projection out of chain. All writes still persist their input.
func TestContractMobilityStatusNotificationProjection(t *testing.T) {
	for _, tc := range []struct {
		name          string
		current, next map[string]any
		fields        []string
		publish       bool
		severity      string
	}{
		{"identical", map[string]any{"phase": "Ready"}, map[string]any{"phase": "Ready"}, nil, false, daemonapi.SeverityInfo},
		{"scan timestamp", map[string]any{"phase": "Ready", "discoveryLastScanAt": "old"}, map[string]any{"phase": "Ready", "discoveryLastScanAt": "new"}, nil, false, daemonapi.SeverityInfo},
		{"freshness extension", map[string]any{"phase": "Ready", "discoveryFreshUntil": "old"}, map[string]any{"phase": "Ready", "discoveryFreshUntil": "new"}, []string{"discoveryFreshUntil"}, false, daemonapi.SeverityInfo},
		{"observed count", map[string]any{"phase": "Ready", "discoveryObserved": 1}, map[string]any{"phase": "Ready", "discoveryObserved": 2}, []string{"discoveryObserved"}, false, daemonapi.SeverityDebug},
		{"routine phase", map[string]any{"phase": "Watching"}, map[string]any{"phase": "Ready"}, []string{"phase"}, false, daemonapi.SeverityDebug},
		{"pending watching", map[string]any{"phase": "Pending"}, map[string]any{"phase": "Watching"}, []string{"phase"}, false, daemonapi.SeverityInfo},
		{"pending ready", map[string]any{"phase": "Pending"}, map[string]any{"phase": "Ready"}, []string{"phase"}, true, daemonapi.SeverityInfo},
		{"degraded", map[string]any{"phase": "Ready"}, map[string]any{"phase": "Degraded"}, []string{"phase"}, true, daemonapi.SeverityInfo},
		{"typed plan digest", map[string]any{"phase": "Projected", "dynamicDigest": "sha256:old"}, map[string]any{"phase": "Projected", "dynamicDigest": "sha256:new"}, []string{"dynamicDigest"}, true, daemonapi.SeverityInfo},
		{"capture intent", map[string]any{"phase": "Projected", "generatedLocalCaptureIntents": 0}, map[string]any{"phase": "Projected", "generatedLocalCaptureIntents": 1}, []string{"generatedLocalCaptureIntents"}, true, daemonapi.SeverityInfo},
		{"routine phase and plan", map[string]any{"phase": "Watching", "dynamicDigest": "sha256:old"}, map[string]any{"phase": "Ready", "dynamicDigest": "sha256:new"}, []string{"dynamicDigest", "phase"}, true, daemonapi.SeverityInfo},
	} {
		for _, merge := range []bool{false, true} {
			mode := "save"
			if merge {
				mode = "merge"
			}
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				before, after := maps.Clone(tc.current), maps.Clone(tc.next)
				fields := statusChangedFieldsForEvent(api.MobilityAPIVersion, "MobilityPool", tc.current, tc.next)
				if !reflect.DeepEqual(fields, tc.fields) {
					t.Fatalf("changedFields=%v, want %v", fields, tc.fields)
				}
				if got := statusChangedEventSeverity(api.MobilityAPIVersion, "MobilityPool", tc.current, tc.next, fields); got != tc.severity {
					t.Fatalf("severity=%s, want %s", got, tc.severity)
				}
				base := mapStore{api.MobilityAPIVersion + "/MobilityPool/pool": statusWithOwnership(api.MobilityAPIVersion, "MobilityPool", tc.current)}
				eventBus := bus.New()
				ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{Topics: []string{"routerd.resource.status.changed"}}, 4)
				defer cancel()
				store := eventedStore{Store: base, Bus: eventBus}
				write := store.SaveObjectStatus
				if merge {
					write = store.MergeObjectStatus
				}
				if err := write(api.MobilityAPIVersion, "MobilityPool", "pool", tc.next); err != nil {
					t.Fatal(err)
				}
				select {
				case event := <-ch:
					if !tc.publish {
						t.Fatalf("unexpected event: %+v", event)
					}
					if event.Attributes["changedFields"] != strings.Join(tc.fields, ",") || event.Severity != tc.severity {
						t.Fatalf("event=%+v", event)
					}
				default:
					if tc.publish {
						t.Fatal("missing status change event")
					}
				}
				for key, value := range tc.next {
					if !reflect.DeepEqual(base.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "pool")[key], value) {
						t.Fatalf("status field %s not persisted", key)
					}
				}
				if !reflect.DeepEqual(tc.current, before) || !reflect.DeepEqual(tc.next, after) {
					t.Fatal("projection mutated its input")
				}
			})
		}
	}
}
