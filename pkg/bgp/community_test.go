// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"reflect"
	"testing"
)

func TestStandardCommunitiesPreservesSupportedOperatorSyntax(t *testing.T) {
	got, err := StandardCommunities([]string{"internet", "no-export", "no-advertise", "64512:100", "4294967295"})
	want := []uint32{0, 0xffffff01, 0xffffff02, 64512<<16 | 100, 0xffffffff}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("communities = %v, %v; want %v", got, err, want)
	}
}
