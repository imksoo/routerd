// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestDSLiteWithdrawalRechecksReceiptAfterPendingNativeRead(t *testing.T) {
	controller, native, store := newDSLiteNativeController(t, platform.OSLinux)
	spec := controller.Router.Spec.Resources[1].Spec.(api.DSLiteTunnelSpec)
	spec.DefaultRoute = false
	controller.Router.Spec.Resources[1].Spec = spec
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := native.links["ds-owned"]
	writes := len(native.writes)
	command, reads := controller.CommandOutput, 0
	var replacement dsliteTunnelReceipt
	controller.CommandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		output, err := command(ctx, name, args...)
		if err == nil && name == "ip" && slices.Equal(args, []string{"-j", "-d", "link", "show"}) {
			reads++
			if reads == 3 {
				records, err := controller.nativeTunnelReceipts()
				if err != nil || len(records) != 1 {
					t.Fatalf("actual pending receipt unavailable: %+v %v", records, err)
				}
				replacement = records[0]
				replacement.Token = strings.Repeat("c", 48)
				if err := store.SQLiteStore.SaveObjectStatus(api.RouterAPIVersion, "Router", dsliteNativeStatusName, map[string]any{"tunnels": []dsliteTunnelReceipt{replacement}}); err != nil {
					t.Fatal(err)
				}
			}
		}
		return output, err
	}
	controller.Router = &api.Router{}
	err := controller.reconcile(t.Context())
	records, readErr := controller.nativeTunnelReceipts()
	if err == nil || reads < 3 || len(native.writes) != writes || native.links["ds-owned"] != before || readErr != nil || len(records) != 1 || records[0] != replacement {
		t.Fatalf("withdrawal crossed a final pending receipt change: reads=%d writes=%v link=%+v records=%+v err=%v read=%v", reads, native.writes[writes:], native.links["ds-owned"], records, err, readErr)
	}
}

func TestDSLiteCreateRechecksReceiptAfterDelegatedSourceWait(t *testing.T) {
	controller, fixture, _ := newDSLiteEndpointFixture(t, false)
	var replacement dsliteTunnelReceipt
	fixture.beforeAddressRead = func() {
		link := fixture.native.links["ds-lite-ra"]
		if !link.Exists || link.Token != "" || replacement.Resource != "" {
			return
		}
		records, err := controller.nativeTunnelReceipts()
		if err != nil || len(records) != 1 {
			t.Fatalf("actual create receipt unavailable: %+v %v", records, err)
		}
		replacement = records[0]
		replacement.Token = strings.Repeat("d", 48)
		if err := fixture.store.SQLiteStore.SaveObjectStatus(api.RouterAPIVersion, "Router", dsliteNativeStatusName, map[string]any{"tunnels": []dsliteTunnelReceipt{replacement}}); err != nil {
			t.Fatal(err)
		}
	}
	err := controller.reconcile(t.Context())
	records, readErr := controller.nativeTunnelReceipts()
	if err == nil || replacement.Resource == "" || fixture.native.links["ds-lite-ra"].Token != "" || readErr != nil || len(records) != 1 || records[0] != replacement {
		t.Fatalf("create ACK retagged a link after the receipt changed during source observation: link=%+v records=%+v err=%v read=%v", fixture.native.links["ds-lite-ra"], records, err, readErr)
	}
}
