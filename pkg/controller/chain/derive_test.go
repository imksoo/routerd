package chain

import "testing"

func TestDeriveIPv6Address(t *testing.T) {
	got, err := DeriveIPv6Address("2409:10:3d60:1220::/60", "1", "::1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "2409:10:3d60:1221::1/64" {
		t.Fatalf("address = %s", got)
	}
}
