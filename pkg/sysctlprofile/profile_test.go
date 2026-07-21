// SPDX-License-Identifier: BSD-3-Clause

package sysctlprofile

import "testing"

func TestRouterFreeBSDProfileContainsNativeForwardingKeys(t *testing.T) {
	entries, err := Entries("router-freebsd", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"net.inet.ip.forwarding":   "1",
		"net.inet6.ip6.forwarding": "1",
	}
	if len(entries) != len(want) {
		t.Fatalf("entries = %#v", entries)
	}
	for _, entry := range entries {
		if want[entry.Key] != entry.Value {
			t.Fatalf("entry %q = %q, want %#v", entry.Key, entry.Value, want)
		}
	}
}
