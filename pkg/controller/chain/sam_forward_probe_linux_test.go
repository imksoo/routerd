// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestSAMEmptyForwardPathsProbe(t *testing.T) {
	const incompatible = "iptables v1.8.7 (nf_tables): chain `routerd_sam_forward' in table `filter' is incompatible, use 'nft' tool."
	const policies = "-P INPUT ACCEPT\n-P FORWARD ACCEPT\n-P OUTPUT ACCEPT\n"
	for _, tc := range []struct {
		name       string
		probe      string
		probeErr   error
		listing    string
		listingErr error
		wantList   bool
		wantErr    bool
	}{
		{name: "legacy absent", probe: "No chain/target/match by that name.", probeErr: errors.New("exit status 1")},
		{name: "binary absent", probeErr: exec.ErrNotFound},
		{name: "nft 1.8.7 absent", probe: incompatible, probeErr: errors.New("exit status 1"), listing: policies, wantList: true},
		{name: "nft 1.8.7 empty table", probe: incompatible, probeErr: errors.New("exit status 1"), wantList: true},
		{name: "different chain", probe: incompatible, probeErr: errors.New("exit status 1"), listing: policies + "-N routerd_sam_forward_extra\n-A routerd_sam_forward_extra -j ACCEPT\n", wantList: true},
		{name: "existing chain", probe: incompatible, probeErr: errors.New("exit status 1"), listing: policies + "-N routerd_sam_forward\n", wantList: true, wantErr: true},
		{name: "existing rules", probe: incompatible, probeErr: errors.New("exit status 1"), listing: policies + "-A routerd_sam_forward -j ACCEPT\n", wantList: true, wantErr: true},
		{name: "incompatible table", probe: incompatible, probeErr: errors.New("exit status 1"), listing: policies, listingErr: errors.New("table is incompatible"), wantList: true, wantErr: true},
		{name: "listing permission failure", probe: incompatible, probeErr: errors.New("exit status 1"), listingErr: errors.New("Permission denied"), wantList: true, wantErr: true},
		{name: "probe permission failure", probe: "Permission denied", probeErr: errors.New("exit status 4"), wantErr: true},
		{name: "unrelated error", probe: "unexpected command failure", probeErr: errors.New("exit status 1"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			err := reconcileSAMForwardPaths(nil, samForwardPathOps{
				runIPTables: func(args ...string) ([]byte, error) {
					call := strings.Join(args, " ")
					calls = append(calls, call)
					switch call {
					case "-S routerd_sam_forward":
						return []byte(tc.probe), tc.probeErr
					case "-S":
						return []byte(tc.listing), tc.listingErr
					default:
						t.Fatalf("probe must not mutate firewall: iptables %s", call)
						return nil, nil
					}
				},
				setSysctl: func(key, value string) error {
					t.Fatalf("probe must not mutate sysctl %s=%s", key, value)
					return nil
				},
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("reconcile error = %v, want error %t", err, tc.wantErr)
			}
			want := []string{"-S routerd_sam_forward"}
			if tc.wantList {
				want = append(want, "-S")
			}
			if !slices.Equal(calls, want) {
				t.Fatalf("commands = %q, want %q", calls, want)
			}
		})
	}
}
