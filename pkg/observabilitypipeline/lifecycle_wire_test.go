// SPDX-License-Identifier: BSD-3-Clause

package observabilitypipeline

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPipelineStartCannotPublishRunningWithAnUnavailableJournal(t *testing.T) {
	journal, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	status, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer status.Close()
	controller := &Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ObservabilityPipeline"}, Metadata: api.ObjectMeta{Name: "wire"}, Spec: api.ObservabilityPipelineSpec{Logs: api.ObservabilityPipelineLogsSpec{Sinks: []api.ObservabilityPipelineLogSink{{Type: "stdout"}}}}}}}},
		Bus:    bus.New(), Events: journal, Store: status,
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		if err := controller.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := controller.Start(t.Context()); err == nil {
		t.Fatal("unavailable journal was acknowledged as started")
	}
	if got := status.ObjectStatus(api.SystemAPIVersion, "ObservabilityPipeline", "wire"); got["phase"] == "Running" {
		t.Fatalf("unavailable journal published Running: %v", got)
	}
}
