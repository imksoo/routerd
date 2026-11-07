// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestHealthSnapshotMatchesCurrentConfiguredProbe(t *testing.T) {
	now := time.Now().UTC()
	spec := api.HealthCheckSpec{Target: "192.0.2.1", Protocol: "tcp", Interface: "wan"}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet"}, Spec: spec},
	}}}
	bound := spec
	bound.SourceInterface = "eth0"
	sample := api.HealthObservation{Probe: bound.Probe(), Phase: "Healthy", CheckedAt: now}
	snapshot := daemonapi.ResourceStatus{Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: "internet"}, Phase: "Healthy", Routing: &sample}
	controller := DaemonStatusController{Router: router, Store: mapStore{}}
	current, err := controller.currentHealthSnapshot(snapshot, now)
	if err != nil || current.Phase != "Healthy" {
		t.Fatalf("configured snapshot: %+v %v", current, err)
	}
	spec.Target = "192.0.2.2"
	router.Spec.Resources[1].Spec = spec
	current, err = controller.currentHealthSnapshot(snapshot, now)
	if err != nil || current.Phase != "Unknown" || current.Routing != &sample {
		t.Fatalf("previous worker authorized current configuration or discarded history: %+v %v", current, err)
	}
	router.Spec.Resources[1].Spec = bound
	for _, absent := range []bool{false, true} {
		candidate := snapshot
		if absent {
			candidate.Routing = nil
		}
		current, err = controller.currentHealthSnapshot(candidate, now.Add(3*time.Minute))
		if err != nil || current.Phase != "Unknown" {
			t.Fatalf("absent/expired snapshot: %+v %v", current, err)
		}
	}
}
