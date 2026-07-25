// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"testing"

	"github.com/imksoo/routerd/pkg/platform"
)

func TestBGPServeDefaultPathsFollowPlatformDirectories(t *testing.T) {
	tests := []struct {
		name     string
		defaults platform.Defaults
		want     [3]string
	}{
		{
			name:     "linux",
			defaults: platform.Defaults{RuntimeDir: "/run/routerd", StateDir: "/var/lib/routerd"},
			want:     [3]string{"/run/routerd/bgp/gobgp.sock", "/run/routerd/bgp/control.sock", "/var/lib/routerd/bgp/applied.json"},
		},
		{
			name:     "freebsd",
			defaults: platform.Defaults{RuntimeDir: "/var/run/routerd", StateDir: "/var/db/routerd"},
			want:     [3]string{"/var/run/routerd/bgp/gobgp.sock", "/var/run/routerd/bgp/control.sock", "/var/db/routerd/bgp/applied.json"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			socket, controlSocket, state := bgpServeDefaultPaths(tc.defaults)
			got := [3]string{socket, controlSocket, state}
			if got != tc.want {
				t.Fatalf("paths = %q, want %q", got, tc.want)
			}
		})
	}
}
