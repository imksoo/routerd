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

func TestDSLiteWithdrawalDoesNotOverwriteReceiptReplacedDuringNativeRead(t *testing.T) {
	controller, native, store := newDSLiteNativeController(t, platform.OSLinux)
	spec := controller.Router.Spec.Resources[1].Spec.(api.DSLiteTunnelSpec)
	spec.DefaultRoute = false
	controller.Router.Spec.Resources[1].Spec = spec
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	beforeLink := native.links["ds-owned"]
	beforeWrites := len(native.writes)
	command := controller.CommandOutput
	replaced := false
	var replacement dsliteTunnelReceipt
	controller.CommandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		output, err := command(ctx, name, args...)
		if err != nil || replaced || name != "ip" || !slices.Equal(args, []string{"-j", "-d", "link", "show"}) {
			return output, err
		}
		records, err := controller.nativeTunnelReceipts()
		if err != nil || len(records) != 1 {
			t.Fatalf("fixture receipt: %+v %v", records, err)
		}
		// The native observation has captured the old cookie. Replace the real
		// durable owner before returning that observation to the controller.
		replacement = records[0]
		replacement.Pending = true
		replacement.Token = strings.Repeat("a", 48)
		if replacement.Token == records[0].Token {
			replacement.Token = strings.Repeat("b", 48)
		}
		if err := store.SQLiteStore.SaveObjectStatus(api.RouterAPIVersion, "Router", dsliteNativeStatusName, map[string]any{"tunnels": []dsliteTunnelReceipt{replacement}}); err != nil {
			t.Fatal(err)
		}
		replaced = true
		return output, nil
	}
	controller.Router = &api.Router{}
	err := controller.reconcile(t.Context())
	if !replaced {
		t.Fatal("fixture did not replace the receipt during the withdrawal observation")
	}
	if len(native.writes) != beforeWrites || native.links["ds-owned"] != beforeLink {
		t.Fatalf("stale withdrawal mutated native state after its receipt was replaced: writes=%v link=%+v err=%v", native.writes[beforeWrites:], native.links["ds-owned"], err)
	}
	records, readErr := controller.nativeTunnelReceipts()
	if readErr != nil || len(records) != 1 || records[0] != replacement {
		t.Fatalf("stale withdrawal replaced or removed the new owner: records=%+v err=%v", records, readErr)
	}
	if err == nil {
		t.Fatal("changed ownership receipt did not stop withdrawal")
	}
}

func TestDSLiteWithdrawalPreservesLinkReplacedDuringInnerAddressRead(t *testing.T) {
	controller, native, _ := newDSLiteNativeController(t, platform.OSLinux)
	spec := controller.Router.Spec.Resources[1].Spec.(api.DSLiteTunnelSpec)
	spec.DefaultRoute = false
	controller.Router.Spec.Resources[1].Spec = spec
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	records, err := controller.nativeTunnelReceipts()
	if err != nil || len(records) != 1 {
		t.Fatalf("fixture receipt: %+v %v", records, err)
	}
	expected := records[0]
	expected.Pending = true
	beforeWrites := len(native.writes)
	command, reads := controller.CommandOutput, 0
	var replacement dsliteTunnelObservation
	controller.CommandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		output, err := command(ctx, name, args...)
		if err == nil && name == "ip" && slices.Equal(args, []string{"-j", "-4", "addr", "show", "dev", "ds-owned"}) {
			reads++
			if reads == 3 {
				// The link identity was read before this inner-address command.
				// Replace the actual native fixture link while that read returns.
				replacement = native.links["ds-owned"]
				replacement.Index++
				replacement.Token = strings.Repeat("e", 48)
				native.links["ds-owned"] = replacement
			}
		}
		return output, err
	}
	controller.Router = &api.Router{}
	err = controller.reconcile(t.Context())
	if reads < 3 {
		t.Fatalf("fixture did not reach the final inner-address read: reads=%d err=%v", reads, err)
	}
	if len(native.writes) != beforeWrites || native.links["ds-owned"] != replacement {
		t.Fatalf("cached native identity authorized deletion of a replacement link: writes=%v link=%+v err=%v", native.writes[beforeWrites:], native.links["ds-owned"], err)
	}
	records, readErr := controller.nativeTunnelReceipts()
	if err == nil || readErr != nil || len(records) != 1 || records[0] != expected {
		t.Fatalf("replacement did not retain the unresolved original owner: records=%+v err=%v read=%v", records, err, readErr)
	}
}
