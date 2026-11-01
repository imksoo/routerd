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

// Router advertisements participate in graceful VRRP activation. Their owner
// must wake directly for a role transition without waiting behind unrelated
// status events or the lock of the resource that triggered the wakeup.
func TestDHCPv6ServerHasBoundedVRRPWakeup(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{}
	wakeups := bus.New()
	runner := &Runner{Router: router, Store: store, Bus: wakeups, Opts: Options{EnabledControllers: []string{"dhcpv6-server"}}}
	controllers, _, _, err := runner.frameworkControllers(t.Context(), slog.New(slog.NewTextHandler(io.Discard, nil)), eventedStore{Store: store, Bus: wakeups, Router: router}, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(controllers) != 1 {
		t.Fatalf("dhcpv6-server registration: %d", len(controllers))
	}
	registered := controllers[0].(framework.FuncController)
	if registered.EventLockKey != "dhcpv6-server" {
		t.Fatalf("dhcpv6-server event lock = %q", registered.EventLockKey)
	}
	role := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "keepalived", Kind: "keepalived"}, daemonapi.EventVRRPRoleTransition, daemonapi.SeverityInfo)
	if !subscriptionSetAccepts(registered.Subscriptions(), role) {
		t.Fatal("dhcpv6-server did not subscribe to the VRRP transition")
	}
	if !subscriptionSetAccepts(registered.Subscriptions(), statusChangedEvent("IPv6DelegatedAddress", "lan-base")) {
		t.Fatal("dhcpv6-server did not subscribe to router-advertisement dependencies")
	}
	if subscriptionSetAccepts(registered.Subscriptions(), statusChangedEvent("HealthCheck", "internet")) {
		t.Fatal("unrelated health-check churn reaches dhcpv6-server")
	}

	observed := make(chan struct{}, 1)
	registered.Every = time.Hour
	registered.PeriodicFunc = func(context.Context) (bool, error) {
		observed <- struct{}{}
		return false, nil
	}
	locker := lock.NewResourceLocker()
	source := &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress", Name: "lan-base"}
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
				t.Errorf("dhcpv6-server worker exit: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("dhcpv6-server worker did not stop")
		}
	}()
	select {
	case <-observer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("dhcpv6-server worker did not start")
	}
	if err := wakeups.Publish(ctx, statusChangedEvent(source.Kind, source.Name)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-observed:
	case <-time.After(2 * time.Second):
		t.Fatal("input source lock blocked dhcpv6-server activation")
	}
}
