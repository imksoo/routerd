// SPDX-License-Identifier: BSD-3-Clause

package derived

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFailedStatusOrOutputCannotConsumeTransition(t *testing.T) {
	for _, trigger := range []string{
		`CREATE TRIGGER reject_derived BEFORE INSERT ON objects WHEN NEW.kind='DerivedEvent' BEGIN SELECT RAISE(FAIL,'derived status fault'); END`,
		`CREATE TRIGGER reject_derived BEFORE INSERT ON events WHEN NEW.topic='routerd.virtual.dslite.asserted' BEGIN SELECT RAISE(FAIL,'derived output fault'); END`,
	} {
		t.Run(trigger, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			setInput(t, store, "Pending", "")
			controller := testController(store, "0s", false)
			reconcile(t, controller)
			before := checkpoint(t, store)
			fault, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer fault.Close()
			if _, err := fault.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			setInput(t, store, "Up", "")
			if err := controller.Reconcile(t.Context(), false); err == nil {
				t.Fatal("injected write failure was swallowed")
			}
			if after := checkpoint(t, store); after != before {
				t.Fatalf("failed output advanced latch: %+v -> %+v", before, after)
			}
			if got := controller.Bus.Recent("routerd.virtual.dslite.asserted"); len(got) != 0 {
				t.Fatal("failed transition delivered")
			}
			if _, err := fault.Exec("DROP TRIGGER reject_derived"); err != nil {
				t.Fatal(err)
			}
			reconcile(t, controller)
			reconcile(t, controller)
			outputs, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.virtual.dslite.asserted"})
			if err != nil || len(outputs) != 1 {
				t.Fatalf("retry did not commit exactly one output: %+v %v", outputs, err)
			}
			if !checkpoint(t, store).Asserted {
				t.Fatal("successful retry did not commit latch")
			}
		})
	}
}

type lostReplyStore struct {
	*routerstate.SQLiteStore
	lose bool
}

func (s *lostReplyStore) CommitDerived(ctx context.Context, name string, expected, next routerstate.DerivedCheckpoint, output *daemonapi.DaemonEvent, inputs routerstate.DerivedInputs) ([]daemonapi.DaemonEvent, error) {
	events, err := s.SQLiteStore.CommitDerived(ctx, name, expected, next, output, inputs)
	if err == nil && output != nil && s.lose {
		s.lose = false
		return nil, errors.New("reply lost after SQLite commit")
	}
	return events, err
}

func TestLostCommitReplyRereadsLatchWithoutDuplicateOutput(t *testing.T) {
	store := openStore(t)
	setInput(t, store, "Pending", "")
	controller := testController(store, "0s", false)
	controller.Store = &lostReplyStore{SQLiteStore: store, lose: true}
	reconcile(t, controller)
	setInput(t, store, "Up", "")
	if err := controller.Reconcile(t.Context(), false); err == nil {
		t.Fatal("lost acknowledgement not observed")
	}
	reconcile(t, controller)
	outputs, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.virtual.dslite.asserted"})
	if err != nil || len(outputs) != 1 {
		t.Fatalf("lost-ack retry duplicated durable output: %+v %v", outputs, err)
	}
	if !checkpoint(t, store).Asserted {
		t.Fatal("lost-ack committed latch was not recovered")
	}
}

func TestRestartResumesDurableHysteresisDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	setInput(t, store, "Pending", "")
	controller := testController(store, "10s", false)
	now := time.Now().UTC()
	controller.Now = func() time.Time { return now }
	reconcile(t, controller)
	setInput(t, store, "Up", "")
	reconcile(t, controller)
	before := checkpoint(t, store)
	if before.PendingDue.IsZero() {
		t.Fatal("pending deadline not committed")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	controller = testController(store, "10s", false)
	now = now.Add(9 * time.Second)
	controller.Now = func() time.Time { return now }
	reconcile(t, controller)
	if cp := checkpoint(t, store); cp != before {
		t.Fatalf("restart rescheduled pending transition: %+v -> %+v", before, cp)
	}
	now = now.Add(time.Second)
	reconcile(t, controller)
	if cp := checkpoint(t, store); !cp.Asserted || !cp.PendingDue.IsZero() {
		t.Fatalf("deadline did not resume: %+v", cp)
	}
	controller = testController(store, "10s", false)
	controller.Now = func() time.Time { return now }
	reconcile(t, controller)
	outputs, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.virtual.dslite.asserted"})
	if err != nil || len(outputs) != 1 {
		t.Fatalf("restart duplicated output: %+v %v", outputs, err)
	}
}

func TestRequiredReadFailurePreservesLatchAndPendingDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	setInput(t, store, "Pending", "")
	controller := testController(store, "1s", false)
	now := time.Now().UTC()
	controller.Now = func() time.Time { return now }
	reconcile(t, controller)
	setInput(t, store, "Up", "")
	reconcile(t, controller)
	before := checkpoint(t, store)
	fault, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer fault.Close()
	if _, err := fault.Exec(`UPDATE objects SET status='[]' WHERE kind='DSLiteTunnel' AND name='ds-lite'`); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := controller.Reconcile(t.Context(), false); err == nil {
		t.Fatal("required input read failure became false or true")
	}
	if after := checkpoint(t, store); after != before {
		t.Fatalf("unknown input changed state: %+v -> %+v", before, after)
	}
	if _, err := fault.Exec(`UPDATE objects SET status='{"phase":"Pending"}' WHERE kind='DSLiteTunnel' AND name='ds-lite'`); err != nil {
		t.Fatal(err)
	}
	reconcile(t, controller)
	if cp := checkpoint(t, store); cp.Asserted || !cp.PendingDue.IsZero() {
		t.Fatalf("current input did not cancel recovered pending assertion: %+v", cp)
	}
	outputs, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.virtual.dslite.asserted"})
	if err != nil || len(outputs) != 0 {
		t.Fatalf("unknown input fabricated transition: %+v %v", outputs, err)
	}
}

func TestStartRejectsDifferentJournalAndCorruptCheckpoint(t *testing.T) {
	store := openStore(t)
	other := openStore(t)
	controller := testController(store, "0s", true)
	controller.Bus = bus.NewWithStore(other)
	if _, err := controller.Start(t.Context()); err == nil {
		t.Fatal("different output journal admitted")
	}
	controller = testController(store, "0s", true)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DerivedEvent", "dslite-active", map[string]any{"derivedVersion": "1", "asserted": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Start(t.Context()); err == nil {
		t.Fatal("corrupt checkpoint admitted")
	}
}

type blockedCommitStore struct {
	*routerstate.SQLiteStore
	entered, release chan struct{}
}

func (s *blockedCommitStore) CommitDerived(ctx context.Context, name string, expected, next routerstate.DerivedCheckpoint, output *daemonapi.DaemonEvent, inputs routerstate.DerivedInputs) ([]daemonapi.DaemonEvent, error) {
	if output != nil {
		close(s.entered)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.release:
		}
	}
	return s.SQLiteStore.CommitDerived(ctx, name, expected, next, output, inputs)
}

func TestInputChangedBeforeCommitCannotPublishStaleAssertion(t *testing.T) {
	store := openStore(t)
	setInput(t, store, "Pending", "")
	controller := testController(store, "0s", false)
	reconcile(t, controller)
	before := checkpoint(t, store)
	setInput(t, store, "Up", "")
	blocked := &blockedCommitStore{SQLiteStore: store, entered: make(chan struct{}), release: make(chan struct{})}
	controller.Store = blocked
	result := make(chan error, 1)
	go func() { result <- controller.Reconcile(t.Context(), false) }()
	select {
	case <-blocked.entered:
	case <-time.After(time.Second):
		t.Fatal("transition did not reach actual commit boundary")
	}
	setInput(t, store, "Pending", "")
	close(blocked.release)
	if err := <-result; !errors.Is(err, routerstate.ErrDerivedConflict) {
		t.Fatalf("stale source was accepted: %v", err)
	}
	if after := checkpoint(t, store); after != before {
		t.Fatalf("stale input advanced latch: %+v -> %+v", before, after)
	}
	controller.Store = store
	reconcile(t, controller)
	outputs, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.virtual.dslite.asserted"})
	if err != nil || len(outputs) != 0 {
		t.Fatalf("stale assertion was emitted: %+v %v", outputs, err)
	}
}

func TestUnusableInputDoesNotBlockIndependentDerivedEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	setInput(t, store, "Pending", "")
	controller := testController(store, "0s", true)
	resource := controller.Router.Spec.Resources[0]
	resource.Metadata.Name = "independent"
	spec, err := resource.DerivedEventSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.Topic = "routerd.independent"
	spec.Inputs = []api.ReadyWhenSpec{{Field: "ready", Equals: "ready"}}
	resource.Spec = spec
	controller.Router.Spec.Resources = append(controller.Router.Spec.Resources, resource)
	fault, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer fault.Close()
	if _, err := fault.Exec(`UPDATE objects SET status='[]' WHERE kind='DSLiteTunnel'`); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(t.Context(), false); err == nil {
		t.Fatal("unusable required read was swallowed")
	}
	outputs, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.independent.asserted"})
	if err != nil || len(outputs) != 1 {
		t.Fatalf("unrelated input failure blocked valid DerivedEvent: %+v %v", outputs, err)
	}
}
