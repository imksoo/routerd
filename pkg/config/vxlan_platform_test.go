// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestVXLANSegmentPlatformAdmissionDoesNotDiscardRemoteIntent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		os      platform.OS
		remotes []string
		group   string
		wantErr bool
	}{
		{name: "Linux all peers", os: platform.OSLinux, remotes: []string{"192.0.2.2", "192.0.2.3"}},
		{name: "FreeBSD multiple peers", os: platform.OSFreeBSD, remotes: []string{"192.0.2.2", "192.0.2.3"}, wantErr: true},
		{name: "FreeBSD point to point", os: platform.OSFreeBSD, remotes: []string{"192.0.2.2"}},
		{name: "FreeBSD duplicate same peer", os: platform.OSFreeBSD, remotes: []string{"192.0.2.2", "192.0.2.2"}},
		{name: "FreeBSD multicast", os: platform.OSFreeBSD, group: "239.1.2.3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "vxlan"}, Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "underlay"}, Spec: api.InterfaceSpec{IfName: "uplink0"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"}, Metadata: api.ObjectMeta{Name: "segment"}, Spec: api.VXLANSegmentSpec{IfName: "vxlan100", VNI: 100, LocalAddress: "192.0.2.1", UnderlayInterface: "underlay", Remotes: tc.remotes, MulticastGroup: tc.group}},
			}}}
			err := ValidateForOS(router, tc.os)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "multiple unicast remotes") {
					t.Fatalf("partial peer intent admitted: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVXLANSegmentFreeBSDReservedVNIDoesNotBecomeUnconfigured(t *testing.T) {
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "vxlan"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "underlay"}, Spec: api.InterfaceSpec{IfName: "uplink0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"}, Metadata: api.ObjectMeta{Name: "segment"}, Spec: api.VXLANSegmentSpec{VNI: 16777215, LocalAddress: "192.0.2.1", UnderlayInterface: "underlay", Remotes: []string{"192.0.2.2"}}},
	}}}
	if err := ValidateForOS(router, platform.OSFreeBSD); err == nil || !strings.Contains(err.Error(), "16777215") {
		t.Fatalf("reserved native VNI admitted: %v", err)
	}
	if err := ValidateForOS(router, platform.OSLinux); err != nil {
		t.Fatal(err)
	}
}
