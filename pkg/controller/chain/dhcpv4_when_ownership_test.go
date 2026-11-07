// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/dhcpv4client"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/state"
)

func TestWhenStatusPublicationCannotEraseDHCPv4WithdrawalOwnership(t *testing.T) {
	if platform.CurrentOS() != platform.OSLinux {
		t.Skip("Linux command fixture")
	}
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.DHCPv4ClientSpec{Interface: "eth-test", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"HealthCheck/link.phase": {Equals: "Healthy"}}}}},
	}}}
	sample := api.HealthObservation{Probe: (api.HealthCheckSpec{Type: "ping", Protocol: "tcp", Target: "192.0.2.1", Port: 80, Interval: "1h", Timeout: "1s"}).Probe(), Phase: "Unhealthy", CheckedAt: time.Now().UTC()}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "link", map[string]any{"phase": "Unhealthy", "routing": sample}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan", map[string]any{"phase": "Bound", "ifname": "eth-test", "appliedAddress": "192.0.2.10/24"}); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Router: router, Store: store}
	present := true
	attempts := 0
	failure := errors.New("address deletion failed")
	controller := dhcpv4client.Controller{Router: router, Store: store, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		switch command {
		case "ip -4 addr show dev eth-test":
			if present {
				return []byte("inet 192.0.2.10/24 scope global eth-test\n"), nil
			}
			return nil, nil
		case "ip -4 addr del 192.0.2.10/24 dev eth-test":
			attempts++
			if attempts == 1 {
				return nil, failure
			}
			present = false
			return nil, nil
		}
		return nil, errors.New("unexpected command: " + command)
	}}
	// Production selection publishes when statuses before the lease callback.
	if err := runner.saveWhenFalseStatuses(eventedStore{Store: store}.withRouter(router)); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(t.Context(), "wan"); !errors.Is(err, failure) {
		t.Fatalf("owned address removal was not attempted: attempts=%d err=%v", attempts, err)
	}
	if err := runner.saveWhenFalseStatuses(eventedStore{Store: store}.withRouter(router)); err != nil {
		t.Fatal(err)
	}
	current, err := store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if err != nil {
		t.Fatal(err)
	}
	if current["phase"] != "Error" || current["reason"] != "WithdrawFailed" || current["appliedAddress"] != "192.0.2.10/24" {
		t.Fatalf("failed withdrawal ownership/diagnostic lost: %+v", current)
	}
	if err := controller.Reconcile(t.Context(), "wan"); err != nil {
		t.Fatal(err)
	}
	if err := runner.saveWhenFalseStatuses(eventedStore{Store: store}.withRouter(router)); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(t.Context(), "wan"); err != nil {
		t.Fatal(err)
	}
	current, err = store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if err != nil {
		t.Fatal(err)
	}
	if present || attempts != 2 || current["reason"] != "WhenFalse" || current["appliedAddress"] != nil {
		t.Fatalf("withdrawal did not converge: present=%v attempts=%d status=%+v", present, attempts, current)
	}
}
