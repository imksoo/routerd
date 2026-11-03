// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"testing"

	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestServeDNSWorkerHasOneOwnerOnLinuxAndFreeBSD(t *testing.T) {
	for _, tc := range []struct {
		name      string
		features  platform.Features
		nativeDNS bool
	}{
		{"Linux systemd", platform.Features{HasSystemd: true}, false},
		{"FreeBSD rc.d", platform.Features{HasRCD: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opts controllerchain.Options
			configureServeDaemonSupervision(&opts, tc.features)
			if !opts.SuperviseClientDaemons {
				t.Fatal("the long-running daemon must supervise its client workers")
			}
			if opts.SuperviseDNSResolvers != tc.nativeDNS {
				t.Fatalf("native DNS owner = %v, want %v; systemd=%v rc.d=%v", opts.SuperviseDNSResolvers, tc.nativeDNS, tc.features.HasSystemd, tc.features.HasRCD)
			}
		})
	}
}
