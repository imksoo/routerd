// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	state "github.com/imksoo/routerd/pkg/state"
)

type lanFreeBSDConfirmFault struct {
	*state.SQLiteStore
	address  string
	deadline time.Time
	fail     bool
}

func (s *lanFreeBSDConfirmFault) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if s.fail && kind == "IPv6DelegatedAddress" {
		receipt, err := delegatedReceipt(status)
		if err != nil {
			return err
		}
		if entry := receipt.entry(s.address); entry != nil && entry.Held && entry.Pending == nil && entry.ValidUntil.Equal(s.deadline) {
			return errors.New("confirmed lifetime receipt lost")
		}
	}
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}

func TestLANFreeBSDRefreshKeepsHeldAddressAcrossLostConfirmation(t *testing.T) {
	for _, badReadback := range []bool{false, true} {
		t.Run(map[bool]string{false: "lost-confirmation", true: "ignored-lifetime"}[badReadback], func(t *testing.T) {
			store, _, c, path := sqliteNativeLANFixture(t)
			c.OperatingSystem = platform.OSFreeBSD
			lease, _, err := store.ReadPDLease("wan-pd")
			if err != nil {
				t.Fatal(err)
			}
			now := c.now()
			current := "2001:db8:1200:1::1/64"
			old := "2001:db8:1100:1::1/64"
			lease.PreviousPrefixes = []state.PDPreviousPrefix{{Prefix: "2001:db8:1100::/60", ExpiresAt: now.Add(300 * time.Second)}}
			if err := store.SavePDLease("wan-pd", lease); err != nil {
				t.Fatal(err)
			}
			receipt := &delegatedNativeReceipt{IfName: "lan0", IfIndex: 7, Scope: "lan-test-kernel", PrefixDelegation: "wan-pd"}
			preferred, valid := nativePDAddressDeadlines(lease, false)
			receipt.claim(current, preferred, valid)
			receipt.confirm(current)
			receipt.claim(old, time.Unix(0, 0).UTC(), now.Add(60*time.Second))
			receipt.confirm(old)
			if err := c.saveDelegatedReceipt("lan-address", map[string]any{"phase": "Applied"}, receipt); err != nil {
				t.Fatal(err)
			}
			fault := &lanFreeBSDConfirmFault{SQLiteStore: store, address: old, deadline: now.Add(300 * time.Second), fail: !badReadback}
			c.Store = fault
			native := map[string][2]uint32{current: {90, 120}, old: {0, 60}}
			c.ObserveAddress = func(_ context.Context, _ string, address string, staged bool) (delegatedAddressObservation, error) {
				lifetime, found := native[address]
				if !found {
					return delegatedAddressObservation{ifIndex: 7, scope: "lan-test-kernel"}, nil
				}
				prefix, err := netip.ParsePrefix(address)
				if err != nil {
					return delegatedAddressObservation{}, err
				}
				out := fmt.Sprintf("inet6 %s prefixlen %d pltime %d vltime %d\n", prefix.Addr(), prefix.Bits(), lifetime[0], lifetime[1])
				if lifetime[0] == 0 {
					out = strings.TrimSuffix(out, "\n") + " deprecated\n"
				}
				observed, err := parseDelegatedAddressObservation([]byte(out), address, platform.OSFreeBSD, staged)
				observed.ifIndex, observed.scope = 7, "lan-test-kernel"
				return observed, err
			}
			var commands []string
			c.Command = func(_ context.Context, name string, args ...string) error {
				commands = append(commands, name+" "+strings.Join(args, " "))
				if name != "ifconfig" || len(args) != 10 || args[2] != "2001:db8:1100:1::1" || args[9] != "alias" || strings.Contains(strings.Join(args, " "), "-alias") {
					t.Fatalf("refresh did not use one in-place alias update: %s %+v", name, args)
				}
				seconds, err := strconv.ParseUint(args[8], 10, 32)
				if err != nil {
					return err
				}
				native[old] = [2]uint32{0, uint32(seconds)}
				if badReadback {
					native[old] = [2]uint32{0, infinitePDLifetime}
				}
				return nil
			}
			err = c.Reconcile(t.Context())
			if err == nil {
				t.Fatal("failed lifetime observation/commit acknowledged")
			}
			if badReadback && !strings.Contains(err.Error(), "lifetime readback") || !badReadback && !strings.Contains(err.Error(), "receipt lost") {
				t.Fatalf("unexpected failure: %v", err)
			}
			if len(commands) != 1 {
				t.Fatalf("unexpected native effects: %+v", commands)
			}
			saved, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
			if err != nil {
				t.Fatal(err)
			}
			pending, err := delegatedReceipt(saved)
			if err != nil {
				t.Fatal(err)
			}
			entry := pending.entry(old)
			if entry == nil || !entry.Held || entry.Pending == nil || entry.Pending.Operation != "apply" || !entry.ValidUntil.Equal(now.Add(60*time.Second)) {
				t.Fatalf("failed refresh extended prior confirmed deadline: %+v", entry)
			}
			if badReadback {
				return
			}

			// Restart recovers the known Held alias without any destructive gap.
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := state.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			c.Store = reopened
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(commands) != 2 {
				t.Fatalf("refresh recovery did not retry one in-place update: %+v", commands)
			}
			saved, err = reopened.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
			if err != nil {
				t.Fatal(err)
			}
			held, err := delegatedReceipt(saved)
			if err != nil {
				t.Fatal(err)
			}
			if entry := held.entry(old); entry == nil || !entry.Held || entry.Pending != nil || !entry.ValidUntil.Equal(now.Add(300*time.Second)) || saved["phase"] != "Applied" {
				t.Fatalf("genuine refresh not confirmed after restart: %+v %+v", entry, saved)
			}
		})
	}
}

func TestLANFreeBSDLifetimeReadbackParsesKernelObservations(t *testing.T) {
	for _, tc := range []struct {
		line             string
		preferred, valid uint32
	}{
		{"inet6 2001:db8::1 prefixlen 64 pltime 0 vltime 300 deprecated", 0, 300},
		{"inet6 2001:db8::1 prefixlen 64 pltime 0 vltime infty 0 deprecated", 0, infinitePDLifetime},
		{"inet6 2001:db8::1 prefixlen 64", infinitePDLifetime, infinitePDLifetime},
	} {
		observed, err := parseDelegatedAddressObservation([]byte(tc.line), "2001:db8::1/64", platform.OSFreeBSD, false)
		if err != nil || !observed.present || !observed.lifetimeKnown || observed.preferredSeconds != tc.preferred || observed.validSeconds != tc.valid {
			t.Fatalf("native lifetime parsed incorrectly: %+v %v", observed, err)
		}
	}
	c := LANAddressController{OperatingSystem: platform.OSFreeBSD}
	if err := c.checkDelegatedLifetimeReadback(delegatedAddressObservation{present: true, lifetimeKnown: true, validSeconds: 0}, 0, infinitePDLifetime); err == nil {
		t.Fatal("zero native lifetime inferred to be infinite")
	}
}
