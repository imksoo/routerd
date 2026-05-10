// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import "testing"

func TestParseFreeBSDFIB(t *testing.T) {
	fib, ok, err := parseFreeBSDFIB("fib:3")
	if err != nil || !ok || fib != 3 {
		t.Fatalf("parseFreeBSDFIB(fib:3) = %d, %v, %v", fib, ok, err)
	}
	if _, ok, err := parseFreeBSDFIB("em0"); err != nil || ok {
		t.Fatalf("parseFreeBSDFIB(em0) = ok %v err %v", ok, err)
	}
	if _, ok, err := parseFreeBSDFIB("fib:-1"); !ok || err == nil {
		t.Fatalf("parseFreeBSDFIB(fib:-1) = ok %v err %v, want error", ok, err)
	}
}
