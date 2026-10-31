// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/lock"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// Readiness notifications are inputs to VRRP admission, not objects owned by
// VRRP. A different consumer may still be working on the notification source.
// It must not hold up this worker and every later ready/role notification.
func TestVRRPReadinessReevaluationDoesNotWaitForInputOwner(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: "lan-gw"},
		Spec: api.VirtualAddressSpec{Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{
			GracefulActivation: &api.VirtualAddressVRRPGracefulActivationSpec{ReadyWhen: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
				"DHCPv6PrefixDelegation/wan.phase":  {Equals: "Bound"},
				"IPv6RouterAdvertisement/lan.phase": {Equals: "Applied"},
			}}},
		}},
	}}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wakeups := bus.New()
	runner := &Runner{Router: router, Store: store, Bus: wakeups, Opts: Options{EnabledControllers: []string{"vrrp"}}}
	controllers, _, _, err := runner.frameworkControllers(t.Context(), logger, eventedStore{Store: store, Bus: wakeups, Router: router}, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(controllers) != 1 {
		t.Fatalf("VRRP registration: %d", len(controllers))
	}
	registered := controllers[0].(framework.FuncController)
	// Retain the real registration, subscriptions and framework lock path.
	// Observe the call boundary without performing host network mutations.
	observed := make(chan daemonapi.DaemonEvent, 2)
	registered.Every = time.Hour
	registered.PeriodicFunc = nil
	registered.ReconcileFunc = func(_ context.Context, event daemonapi.DaemonEvent) error {
		observed <- event
		return nil
	}
	locker := lock.NewResourceLocker()
	source := &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation", Name: "wan"}
	release, err := locker.Lock(t.Context(), source.APIVersion+"/"+source.Kind+"/"+source.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	observer := leaseRoleLockObserver{started: make(chan string, 1), completed: make(chan string, 4)}
	runtime := framework.Runner{Bus: wakeups, Locker: locker, Logger: logger, Observer: observer, SkipBootstrap: true, Interval: time.Hour}
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx, registered) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("VRRP worker exit: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("VRRP worker did not stop")
		}
	}()
	select {
	case <-observer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("VRRP worker did not start")
	}
	first := statusChangedEvent(source.Kind, source.Name)
	second := statusChangedEvent("IPv6RouterAdvertisement", "lan")
	for _, event := range []daemonapi.DaemonEvent{first, second} {
		if err := wakeups.Publish(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []daemonapi.DaemonEvent{first, second} {
		select {
		case got := <-observed:
			if got.Type != want.Type || got.Resource == nil || *got.Resource != *want.Resource {
				t.Fatalf("readiness notification was lost or rewritten: %+v", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("input source lock blocked VRRP readiness and its following notification")
		}
	}
	// The source lock remains held until this test returns: success must not
	// depend on canceling the other consumer, dropping the event or a timer.
}
