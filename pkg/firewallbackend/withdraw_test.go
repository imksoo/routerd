// SPDX-License-Identifier: BSD-3-Clause

package firewallbackend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/nftstate"
)

func TestNftablesWithdrawalRequiresNativeOwnershipAndAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name, inventory    string
		readErr, deleteErr bool
		wantErr            bool
	}{
		{name: "owned", inventory: `{"nftables":[{"table":{"family":"inet","name":"routerd_filter","handle":42,"comment":"routerd.owner=routerd routerd.generation=1"}}]}`},
		{name: "absent", inventory: `{"nftables":[]}`},
		{name: "foreign", inventory: `{"nftables":[{"table":{"family":"inet","name":"routerd_filter","handle":42}}]}`, wantErr: true},
		{name: "read-failed", readErr: true, wantErr: true},
		{name: "malformed", inventory: `broken`, wantErr: true},
		{name: "delete-failed", inventory: `{"nftables":[{"table":{"family":"inet","name":"routerd_filter","handle":42,"comment":"routerd.owner=routerd routerd.generation=1"}}]}`, deleteErr: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "firewall.nft")
			prior := []byte("previous rules")
			if err := os.WriteFile(path, prior, 0644); err != nil {
				t.Fatal(err)
			}
			if err := nftstate.MarkVerified(path, time.Now()); err != nil {
				t.Fatal(err)
			}
			var calls []string
			b := Nftables{Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
				call := strings.Join(args, " ")
				calls = append(calls, call)
				switch call {
				case "-j list tables":
					if tc.readErr {
						return nil, errors.New("read denied")
					}
					return []byte(tc.inventory), nil
				case "delete table inet handle 42":
					if tc.deleteErr {
						return nil, errors.New("delete denied")
					}
					return nil, nil
				default:
					return nil, errors.New("unexpected command: " + call)
				}
			}}
			_, err := b.Apply(t.Context(), Ruleset{Path: path}, false)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v calls=%v", err, calls)
			}
			data, readErr := os.ReadFile(path)
			if tc.wantErr {
				if readErr != nil || string(data) != string(prior) {
					t.Fatalf("failed withdrawal lost retry input: %q %v", data, readErr)
				}
				if len(calls) > 1 && tc.name != "delete-failed" {
					t.Fatalf("unsafe deletion: %v", calls)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatalf("successful withdrawal retained stale rules: %q %v", data, readErr)
			}
		})
	}
}

func TestNftablesWithdrawalDryRunAndUnusedFirewallAreInert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.nft")
	b := Nftables{Run: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unexpected native command")
		return nil, nil
	}}
	if changed, err := b.Apply(t.Context(), Ruleset{Path: path}, false); err != nil || changed {
		t.Fatalf("unused firewall: changed=%v err=%v", changed, err)
	}
	if err := os.WriteFile(path, []byte("active rules"), 0644); err != nil {
		t.Fatal(err)
	}
	if changed, err := b.Apply(t.Context(), Ruleset{Path: path}, true); err != nil || !changed {
		t.Fatalf("dry run: changed=%v err=%v", changed, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "active rules" {
		t.Fatalf("dry run changed rules: %q %v", data, err)
	}
}
