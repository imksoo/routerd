// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
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

type leaseRoleLockObserver struct {
	started   chan string
	completed chan string
}

func (o leaseRoleLockObserver) ControllerStarted(name string, _ time.Duration) {
	o.started <- name
}
func (o leaseRoleLockObserver) ControllerReconciled(name, _ string, _, _ time.Duration, _ error) {
	select {
	case o.completed <- name:
	default:
	}
}

// A role notification is a read-only input to lease replication. Waiting for a
// stopped peer must not lock that source and prevent forwarding owners from
// processing the same MASTER notification.
func TestLeaseSyncRoleNotificationDoesNotBlockOtherConsumers(t *testing.T) {
	dir := t.TempDir()
	lease := filepath.Join(dir, "dnsmasq.leases")
	if err := os.WriteFile(lease, []byte("1 02:00:00:00:00:01 192.0.2.10 client *\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DHCPv4ServerSpec{LeaseFile: lease}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"}, Metadata: api.ObjectMeta{Name: "leases"}, Spec: api.DHCPv4ServerLeaseSyncSpec{
			Source:  api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan"},
			Targets: []api.LeaseSyncTargetSpec{{Host: "stopped-peer.invalid"}},
		}},
	}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wakeups := bus.New()
	runner := &Runner{Router: router, Store: store, Bus: wakeups, Opts: Options{EnabledControllers: []string{"dhcp-lease-sync"}}}
	controllers, _, _, err := runner.frameworkControllers(t.Context(), logger, eventedStore{Store: store, Bus: wakeups, Router: router}, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(controllers) != 1 {
		t.Fatalf("lease-sync registration: %d controllers", len(controllers))
	}
	registered := controllers[0].(framework.FuncController)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	transfer := FileSyncController{Router: router, Store: store, Logger: logger, Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return nil, errors.New("peer is unavailable")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	// Preserve the real chain subscription/lock scope and actual FileSync
	// implementation; replace only its process boundary and scheduling period.
	registered.PeriodicFunc = didWorkPeriodic(transfer.Reconcile)
	registered.Every = time.Hour
	ctx, cancel := context.WithCancel(t.Context())
	observer := leaseRoleLockObserver{started: make(chan string, 2), completed: make(chan string, 4)}
	locker := lock.NewResourceLocker()
	runtime := framework.Runner{Bus: wakeups, Locker: locker, Logger: logger, Observer: observer, SkipBootstrap: true, Interval: time.Hour}
	done := make(chan error, 2)
	workers := 1
	go func() { done <- runtime.Run(ctx, registered) }()
	defer func() {
		close(release)
		cancel()
		for i := 0; i < workers; i++ {
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("worker exit: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("worker did not stop")
			}
		}
	}()
	wait := func(ch <-chan string, want string) {
		t.Helper()
		select {
		case got := <-ch:
			if got != want {
				t.Fatalf("worker=%s, want %s", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("worker %s did not start", want)
		}
	}
	wait(observer.started, "dhcp-lease-sync")
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "keepalived", Kind: "keepalived", Instance: "notify"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress", Name: "gateway"}
	if err := wakeups.Publish(ctx, event); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("lease replication did not enter its process boundary")
	}
	observed := make(chan daemonapi.DaemonEvent, 1)
	consumer := framework.FuncController{ControllerName: "forwarding-owner", Every: time.Hour, Subs: statusSubscriptions("VirtualAddress"), ReconcileFunc: func(_ context.Context, event daemonapi.DaemonEvent) error {
		select {
		case observed <- event:
		default:
		}
		return nil
	}}
	workers++
	go func() { done <- runtime.Run(ctx, consumer) }()
	wait(observer.started, "forwarding-owner")
	if err := wakeups.Publish(ctx, event); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-observed:
		if got.Resource == nil || *got.Resource != *event.Resource || got.Type != event.Type {
			t.Fatalf("role source was rewritten: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stopped-peer lease replication held the MASTER notification source lock and blocked another consumer")
	}
	// The transfer is still blocked: this result cannot come from shortening its
	// timeout, suppressing the role subscription, or reporting a successful copy.
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "leases")
	if err != nil || status["phase"] == "Synced" {
		t.Fatalf("unfinished transfer reported success: %+v %v", status, err)
	}
}
