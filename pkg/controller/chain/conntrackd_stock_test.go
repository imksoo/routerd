// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"errors"
	"github.com/imksoo/routerd/pkg/api"
	"strings"
	"testing"
)

func TestStockConntrackdProtection(t *testing.T) {
	for _, tt := range []struct {
		name, before, after       string
		dry, maskError, wantError bool
		wantCalls                 int
	}{
		{name: "loaded", before: "LoadState=loaded\nActiveState=active\n", after: "LoadState=masked\nActiveState=inactive\n", wantCalls: 3},
		{name: "already masked", before: "LoadState=masked\nActiveState=inactive\n", wantCalls: 1},
		{name: "masked but active", before: "LoadState=masked\nActiveState=active\n", after: "LoadState=masked\nActiveState=inactive\n", wantCalls: 3},
		{name: "local unit refusal", before: "LoadState=loaded\nActiveState=active\n", maskError: true, wantError: true, wantCalls: 2},
		{name: "mask ineffective", before: "LoadState=loaded\nActiveState=active\n", after: "LoadState=loaded\nActiveState=active\n", wantError: true, wantCalls: 3},
		{name: "dry run", dry: true, wantCalls: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			c := SystemdUnitController{DryRun: tt.dry}
			err := c.ensureStockConntrackdMasked(t.Context(), func(_ context.Context, name string, args ...string) ([]byte, error) {
				call := strings.Join(append([]string{name}, args...), " ")
				calls = append(calls, call)
				if len(calls) == 1 {
					return []byte(tt.before), nil
				}
				if len(calls) == 2 {
					if call != "systemctl mask --now conntrackd.service" {
						t.Fatalf("unexpected command: %s", call)
					}
					if tt.maskError {
						return []byte("File exists"), errors.New("refused")
					}
					return nil, nil
				}
				return []byte(tt.after), nil
			})
			if (err != nil) != tt.wantError || len(calls) != tt.wantCalls {
				t.Fatalf("err=%v calls=%v", err, calls)
			}
		})
	}
}

func TestConntrackdMaskFailurePreventsManagedApply(t *testing.T) {
	c := SystemdUnitController{Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44SessionSync"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.NAT44SessionSyncSpec{Mode: "conntrackd", Conntrackd: &api.ConntrackdSyncSpec{LocalAddress: "192.0.2.1", PeerAddress: "192.0.2.2", Interface: "test0"}}},
	}}}}
	var calls []string
	err := c.reconcileConntrackdSyncUnits(t.Context(), func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := strings.Join(append([]string{name}, args...), " ")
		calls = append(calls, call)
		if len(calls) == 1 {
			return []byte("LoadState=loaded\nActiveState=active\n"), nil
		}
		return nil, errors.New("mask refused")
	})
	if err == nil || len(calls) != 2 || calls[1] != "systemctl mask --now conntrackd.service" {
		t.Fatalf("err=%v calls=%v", err, calls)
	}
}
