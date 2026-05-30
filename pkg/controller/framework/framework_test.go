// SPDX-License-Identifier: BSD-3-Clause

package framework

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/lock"
)

type recordingResourceObserver struct {
	mu       sync.Mutex
	started  []string
	reconcil []reconcileCall
}

type reconcileCall struct {
	name         string
	trigger      string
	resourceKind string
	resourceName string
	err          error
}

func (r *recordingResourceObserver) ControllerStarted(name string, _ time.Duration) {
	r.mu.Lock()
	r.started = append(r.started, name)
	r.mu.Unlock()
}

func (r *recordingResourceObserver) ControllerReconciled(name, trigger string, _, _ time.Duration, err error) {
	r.mu.Lock()
	r.reconcil = append(r.reconcil, reconcileCall{name: name, trigger: trigger, err: err})
	r.mu.Unlock()
}

func (r *recordingResourceObserver) ControllerReconciledResource(name, trigger, resourceKind, resourceName string, _, _ time.Duration, err error) {
	r.mu.Lock()
	r.reconcil = append(r.reconcil, reconcileCall{name: name, trigger: trigger, resourceKind: resourceKind, resourceName: resourceName, err: err})
	r.mu.Unlock()
}

func (r *recordingResourceObserver) snapshot() []reconcileCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]reconcileCall, len(r.reconcil))
	copy(out, r.reconcil)
	return out
}

func TestRunLockedDispatchesResourceObserver(t *testing.T) {
	observer := &recordingResourceObserver{}
	logger := slog.Default()
	locker := lock.NewResourceLocker()
	runLocked(context.Background(), logger, locker, observer, "key", "dns", "event", "DNSResolver", "lan", time.Second, func(context.Context) error {
		return errors.New("boom")
	})
	got := observer.snapshot()
	if len(got) != 1 {
		t.Fatalf("calls = %d, want 1", len(got))
	}
	if got[0].name != "dns" || got[0].trigger != "event" || got[0].resourceKind != "DNSResolver" || got[0].resourceName != "lan" {
		t.Fatalf("call = %+v", got[0])
	}
	if got[0].err == nil {
		t.Fatalf("expected error to be propagated")
	}
}

type plainObserver struct {
	mu    sync.Mutex
	calls []reconcileCall
}

func (p *plainObserver) ControllerStarted(string, time.Duration) {}
func (p *plainObserver) ControllerReconciled(name, trigger string, _, _ time.Duration, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, reconcileCall{name: name, trigger: trigger, err: err})
}

func TestRunLockedFallsBackForPlainObserver(t *testing.T) {
	observer := &plainObserver{}
	runLocked(context.Background(), slog.Default(), lock.NewResourceLocker(), observer, "key", "dns", "event", "DNSResolver", "lan", time.Second, func(context.Context) error {
		return nil
	})
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(observer.calls))
	}
	if observer.calls[0].resourceKind != "" || observer.calls[0].resourceName != "" {
		t.Fatalf("plain observer should not receive resource info: %+v", observer.calls[0])
	}
}

func TestEventResourceKindName(t *testing.T) {
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "test", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{Kind: "DHCPv6Client", Name: "wan"}
	kind, name := eventResourceKindName(event)
	if kind != "DHCPv6Client" || name != "wan" {
		t.Fatalf("unexpected %q/%q", kind, name)
	}

	emptyEvent := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "test", daemonapi.SeverityInfo)
	kind, name = eventResourceKindName(emptyEvent)
	if kind != "" || name != "" {
		t.Fatalf("expected empty resource info, got %q/%q", kind, name)
	}
}
