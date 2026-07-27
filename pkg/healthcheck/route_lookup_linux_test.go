// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package healthcheck

import (
	"context"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestLookupRouteUsesProbeBinding(t *testing.T) {
	original := routeLookupCommand
	defer func() { routeLookupCommand = original }()
	var got []string
	routeLookupCommand = func(_ context.Context, args ...string) ([]byte, error) {
		got = append([]string(nil), args...)
		return []byte(`[{"gateway":"fe80::1","dev":"dslite-a","prefsrc":"2409:10::2"}]`), nil
	}
	info, err := lookupRoute(t.Context(), api.HealthCheckSpec{Target: "2001:db8::1", AddressFamily: "ipv6", FwMark: 0x110, SourceAddress: "2409:10::2", SourceInterface: "dslite-a"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-j", "-6", "route", "get", "2001:db8::1", "mark", "0x110", "from", "2409:10::2", "oif", "dslite-a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ip args = %#v, want %#v", got, want)
	}
	if info.OutInterface != "dslite-a" || info.Source != "2409:10::2" {
		t.Fatalf("route info = %#v", info)
	}
}
