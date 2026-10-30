// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"context"
	"errors"
	"github.com/imksoo/routerd/pkg/api"
	"strings"
	"testing"
	"time"
)

func TestNextExpiryClearsAfterWithdrawalAndRetainsFailedCleanupRetry(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	fail, present := true, true
	c := effectController(func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		switch line {
		case "ip -4 addr show dev eth0":
			if present {
				return []byte("inet 192.0.2.10/24 scope global eth0\n"), nil
			}
			return nil, nil
		case "ip -4 addr del 192.0.2.10/24 dev eth0":
			if fail {
				return nil, errors.New("address busy")
			}
			present = false
			return nil, nil
		}
		return nil, errors.New("unexpected command: " + line)
	})
	key := api.NetAPIVersion + "/DHCPv4Client/wan"
	store := mapStore{key: {"phase": "Bound", "ifname": "eth0", "currentAddress": "192.0.2.10", "appliedAddress": "192.0.2.10/24", "expiresAt": time.Unix(1, 0).UTC().Format(time.RFC3339Nano)}}
	c.Store = store
	// The absent worker is an error in either case; it must neither extend the
	// recorded authority nor erase a failed cleanup's safety retry deadline.
	if err := c.ReconcileAll(context.Background()); err == nil {
		t.Fatal("unavailable worker/failed cleanup was hidden")
	}
	if after := c.NextExpiryAfter(); after <= 0 || after > time.Second {
		t.Fatalf("failed cleanup lost bounded retry: %s", after)
	}
	fail = false
	if err := c.ReconcileAll(context.Background()); err == nil {
		t.Fatal("unavailable worker was reported healthy")
	}
	if present || mapString(store[key], "expiresAt") == "" {
		t.Fatal("cleanup lost display history or did not withdraw address")
	}
	if after := c.NextExpiryAfter(); after != 0 {
		t.Fatalf("withdrawn display history pinned safety polling: %s", after)
	}
}

func TestNextExpiryIgnoresUnmanagedLeaseHistory(t *testing.T) {
	c := effectController(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("touched unmanaged effects")
		return nil, nil
	})
	c.Router = &api.Router{}
	key := api.NetAPIVersion + "/DHCPv4Client/foreign"
	store := mapStore{key: {"managedBy": "external", "phase": "Bound", "ifname": "other", "currentAddress": "192.0.2.20", "appliedAddress": "192.0.2.20/24", "expiresAt": time.Unix(1, 0).UTC().Format(time.RFC3339Nano)}}
	c.Store = store
	if err := c.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.NextExpiryAfter() != 0 || store[key] == nil {
		t.Fatal("unmanaged history acquired a routerd cleanup deadline or was deleted")
	}
}
