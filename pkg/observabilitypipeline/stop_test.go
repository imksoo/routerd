// SPDX-License-Identifier: BSD-3-Clause

package observabilitypipeline

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type blockedPipelineOutput struct {
	entered chan struct{}
	release chan struct{}
}

func (w blockedPipelineOutput) Write(data []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(data), nil
}

func TestStopRequiresActualExporterExit(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.LoadOrInitializeEventConsumerCursor("observabilitypipeline/test"); err != nil {
		t.Fatal(err)
	}
	events := bus.NewWithStore(store)
	output := blockedPipelineOutput{entered: make(chan struct{}), release: make(chan struct{})}
	controller := testStoredController(events, store, store, &lockedBuffer{})
	controller.Stdout = output
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := events.Publish(t.Context(), daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test"}, "routerd.test", daemonapi.SeverityInfo)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-output.entered:
	case <-time.After(time.Second):
		t.Fatal("exporter did not enter output")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := controller.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked worker acknowledged teardown: %v", err)
	}
	if len(controller.running) != 1 {
		t.Fatal("unacknowledged owner forgotten")
	}
	close(output.release)
	if err := controller.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(controller.running) != 0 {
		t.Fatal("exited worker remained owned")
	}
}
