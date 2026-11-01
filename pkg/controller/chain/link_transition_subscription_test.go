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

// Link observation is an HA readiness input. It must wake directly for a VRRP
// transition and must not queue behind unrelated status churn before checking
// a newly created failover interface.
func TestLinkObservationHasBoundedVRRPWakeup(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: "lan-vmac"},
		Spec:     api.InterfaceSpec{IfName: "lan-vrrp"},
	}}}}
	wakeups := bus.New()
	runner := &Runner{Router: router, Store: store, Bus: wakeups, Opts: Options{EnabledControllers: []string{"link"}}}
	controllers, _, _, err := runner.frameworkControllers(t.Context(), slog.New(slog.NewTextHandler(io.Discard, nil)), eventedStore{Store: store, Bus: wakeups, Router: router}, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(controllers) != 1 {
		t.Fatalf("link registration: %d", len(controllers))
	}
	registered := controllers[0].(framework.FuncController)
	if registered.EventLockKey != "link" {
		t.Fatalf("link event lock = %q", registered.EventLockKey)
	}
	if registered.Every != time.Second {
		t.Fatalf("link observation interval = %s", registered.Every)
	}
	role := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "keepalived", Kind: "keepalived"}, daemonapi.EventVRRPRoleTransition, daemonapi.SeverityInfo)
	if !subscriptionSetAccepts(registered.Subscriptions(), role) {
		t.Fatal("link observation did not subscribe to the VRRP transition")
	}
	if !subscriptionSetAccepts(registered.Subscriptions(), statusChangedEvent("VirtualAddress", "lan-gw")) {
		t.Fatal("link observation did not subscribe to VirtualAddress status")
	}
	if subscriptionSetAccepts(registered.Subscriptions(), statusChangedEvent("IPv6RouterAdvertisement", "lan-ra")) {
		t.Fatal("unrelated router-advertisement churn reaches link observation")
	}

	// Retain the real subscriptions and framework locking. A different worker
	// may still own the VirtualAddress notification source while link observation
	// reads the native interfaces created by that transition.
	observed := make(chan daemonapi.DaemonEvent, 1)
	registered.Every = time.Hour
	registered.PeriodicFunc = nil
	registered.ReconcileFunc = func(_ context.Context, event daemonapi.DaemonEvent) error {
		observed <- event
		return nil
	}
	locker := lock.NewResourceLocker()
	source := &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress", Name: "lan-gw"}
	release, err := locker.Lock(t.Context(), source.APIVersion+"/"+source.Kind+"/"+source.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	observer := leaseRoleLockObserver{started: make(chan string, 1), completed: make(chan string, 2)}
	runtime := framework.Runner{Bus: wakeups, Locker: locker, Observer: observer, SkipBootstrap: true, Interval: time.Hour}
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx, registered) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("link worker exit: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("link worker did not stop")
		}
	}()
	select {
	case <-observer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("link worker did not start")
	}
	event := statusChangedEvent(source.Kind, source.Name)
	if err := wakeups.Publish(ctx, event); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-observed:
		if got.Resource == nil || *got.Resource != *source {
			t.Fatalf("link notification was lost or rewritten: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("input source lock blocked native link observation")
	}
}
