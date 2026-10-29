// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/federation"
	"github.com/imksoo/routerd/pkg/federationguard"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFailedNativeCaptureWithdrawalStillBlocksFederationFeedback(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	applier := &fakeSAMApplier{}
	owner := SAMController{Router: &api.Router{}, Store: store, OS: platform.OSLinux, Applier: applier, LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{samJournalCapture()}}
	if err := owner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner.LocalCaptureIntents = nil
	applier.deleteErr = errors.New("native withdrawal unavailable")
	if err := owner.Reconcile(t.Context()); err == nil {
		t.Fatal("failed native cleanup reported success")
	}
	observed := federation.Event{Type: federation.ObservedIPv4EventType, Subject: samJournalCapture().Address}
	var captured federationguard.SelfCapturedObservedEventError
	if err := federationguard.RejectSelfCapturedObservedEvent(store, observed, time.Now()); !errors.As(err, &captured) {
		t.Fatalf("failed real native owner cleanup authorized observer feedback: %v", err)
	}
	applier.deleteErr = nil
	if err := owner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := federationguard.RejectSelfCapturedObservedEvent(store, observed, time.Now()); err != nil {
		t.Fatalf("successful actual cleanup did not release guard: %v", err)
	}
}
