// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestWireGuardListenPortChangeAcknowledgesCurrentNativePorts(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	for _, scenario := range []struct {
		name string
		port int
	}{{name: "changed", port: 51821}, {name: "omitted", port: 0}} {
		t.Run(scenario.name, func(t *testing.T) {
			parent, native, store := wireGuardChildFixture(t)
			setPort := func(port int) error {
				spec, err := parent.Router.Spec.Resources[0].WireGuardInterfaceSpec()
				if err != nil {
					t.Fatal(err)
				}
				spec.ListenPort = port
				parent.Router.Spec.Resources[0].Spec = spec
				return parent.Reconcile(t.Context())
			}
			if err := setPort(51820); err != nil {
				t.Fatal(err)
			}
			before := native.links["wg-owned"]
			if err := setPort(scenario.port); err != nil {
				records, readErr := parent.readNativeWireGuard()
				t.Fatalf("listen-port change could not acknowledge its own native receipt: err=%v records=%+v read=%v", err, records, readErr)
			}
			if err := parent.Reconcile(t.Context()); err != nil {
				t.Fatalf("unchanged listen-port reconciliation did not converge: %v", err)
			}
			after := native.links["wg-owned"]
			if after.IfIndex != before.IfIndex || after.Token != before.Token {
				t.Fatalf("listen-port change replaced the native owner: before=%+v after=%+v", before, after)
			}
			records, err := parent.readNativeWireGuard()
			var expected []int
			if scenario.port > 0 {
				expected = []int{scenario.port}
			}
			if err != nil || len(records) != 1 || records[0].Pending || !slices.Equal(records[0].Ports, expected) || len(native.rules) != len(expected) {
				t.Fatalf("listen-port change retained obsolete pending effects: records=%+v rules=%v err=%v", records, native.rules, err)
			}
			reader := nativeParentReader{Router: parent.Router, Store: store, WireGuard: &parent}
			if proof, err := reader.observeDeclared(t.Context(), "wg-owned"); err != nil || !proof.Exists {
				t.Fatalf("acknowledged listen-port change left children blocked: proof=%+v err=%v", proof, err)
			}
		})
	}
}

type wireGuardPortShrinkFaultStore struct {
	*routerstate.SQLiteStore
	failNext bool
	failure  error
}

func (s *wireGuardPortShrinkFaultStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if version == api.RouterAPIVersion && kind == "Router" && name == wireGuardNativeStatusName && s.failNext {
		if records, ok := status["interfaces"].([]wireGuardNativeRecord); ok && len(records) == 1 && records[0].Pending && slices.Equal(records[0].Ports, []int{51821}) {
			s.failNext = false
			return s.failure
		}
	}
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}

func TestWireGuardListenPortShrinkSaveFailureRetainsPendingRetry(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	parent, native, database := wireGuardChildFixture(t)
	spec, err := parent.Router.Spec.Resources[0].WireGuardInterfaceSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.ListenPort = 51820
	parent.Router.Spec.Resources[0].Spec = spec
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := native.links["wg-owned"]
	failure := errors.New("injected native port receipt save failure")
	store := &wireGuardPortShrinkFaultStore{SQLiteStore: database, failNext: true, failure: failure}
	parent.Store = store
	spec.ListenPort = 51821
	parent.Router.Spec.Resources[0].Spec = spec
	if err := parent.Reconcile(t.Context()); !errors.Is(err, failure) || store.failNext {
		t.Fatalf("port receipt save failure was not returned: err=%v failNext=%t", err, store.failNext)
	}
	records, err := parent.readNativeWireGuard()
	if err != nil || len(records) != 1 || !records[0].Pending || !slices.Equal(records[0].Ports, []int{51820, 51821}) {
		t.Fatalf("failed port receipt save lost pending cleanup authority: records=%+v err=%v", records, err)
	}
	newRule := strings.Join(wireGuardInputRule("-C", 51821, before.Token)[2:], " ")
	if len(native.rules) != 1 || !native.rules[newRule] {
		t.Fatalf("port receipt save failed before obsolete rules were removed: rules=%v", native.rules)
	}
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatalf("pending port receipt did not recover after one save failure: %v", err)
	}
	records, err = parent.readNativeWireGuard()
	if err != nil || len(records) != 1 || records[0].Pending || !slices.Equal(records[0].Ports, []int{51821}) || len(native.rules) != 1 || !native.rules[newRule] {
		t.Fatalf("port receipt retry did not acknowledge completed effects: records=%+v rules=%v err=%v", records, native.rules, err)
	}
	if after := native.links["wg-owned"]; after.IfIndex != before.IfIndex || after.Token != before.Token {
		t.Fatalf("port receipt retry replaced the native owner: before=%+v after=%+v", before, after)
	}
}
