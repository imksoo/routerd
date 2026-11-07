// SPDX-License-Identifier: BSD-3-Clause

package egressroute

import (
	"errors"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
)

type failedReadStore struct {
	mapStore
	failKind   string
	failHealth bool
}

func (s failedReadStore) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	if kind == s.failKind {
		return nil, errors.New("fixture read failure")
	}
	return s.mapStore.ReadObjectStatus(version, kind, name)
}

func (s failedReadStore) HealthObservation(name string) (api.HealthObservation, error) {
	if s.failHealth {
		return api.HealthObservation{}, errors.New("fixture health read failure")
	}
	return s.mapStore.HealthObservation(name)
}

func TestSelectionReadFailurePreservesPreviousDecision(t *testing.T) {
	for _, scenario := range []string{"device", "source", "dependency", "health-status", "health-observation", "previous-selection"} {
		t.Run(scenario, func(t *testing.T) {
			previous := map[string]any{"phase": PhaseApplied, "selectedCandidate": "primary", "selectedDevice": "wan0"}
			store := failedReadStore{mapStore: mapStore{api.NetAPIVersion + "/EgressRoutePolicy/ipv4-default": previous}}
			candidate := api.EgressRoutePolicyCandidate{Name: "primary", Device: "wan0"}
			switch scenario {
			case "device":
				candidate.DeviceFrom = api.StatusValueSourceSpec{Resource: "Link/wan", Field: "ifname"}
				store.failKind = "Link"
			case "source":
				candidate.Source = "Link/wan"
				store.failKind = "Link"
			case "dependency":
				candidate.DependsOn = []api.ResourceDependencySpec{{Resource: "Link/wan"}}
				store.failKind = "Link"
			case "health-status", "health-observation":
				candidate.HealthCheck = "internet"
				if scenario == "health-status" {
					store.failKind = "HealthCheck"
				} else {
					store.failHealth = true
				}
			case "previous-selection":
				store.failKind = "EgressRoutePolicy"
			}
			controller := Controller{Router: routerWithPolicy(api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{candidate}}), Store: store, Bus: bus.New()}
			if candidate.HealthCheck != "" {
				controller.Router.Spec.Resources = append(controller.Router.Spec.Resources, healthCheckResource(candidate.HealthCheck, api.HealthCheckSpec{TargetSource: "static", Target: "192.0.2.1", Protocol: "tcp"}))
			}
			if err := controller.Reconcile(t.Context()); err == nil {
				t.Fatal("read failure was accepted as a new selection")
			}
			if got := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default"); !reflect.DeepEqual(got, previous) {
				t.Fatalf("read failure overwrote previous decision: %#v", got)
			}
		})
	}
}
