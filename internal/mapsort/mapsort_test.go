// SPDX-License-Identifier: BSD-3-Clause

package mapsort

import "testing"

func TestKeys(t *testing.T) {
	if got := Keys[string](nil); got == nil || len(got) != 0 {
		t.Fatalf("Keys(nil) = %#v, want non-nil empty slice", got)
	}
	got := Keys(map[string]int{"z": 1, "a": 2, "m": 3})
	want := []string{"a", "m", "z"}
	if len(got) != len(want) {
		t.Fatalf("Keys() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Keys() = %#v, want %#v", got, want)
		}
	}
}
