// SPDX-License-Identifier: BSD-3-Clause

package main

import "testing"

func TestParseOptions(t *testing.T) {
	opts, err := parseOptions([]string{"activate", "--parent", "eth0", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:13"})
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}
	if opts.action != "activate" || opts.parent != "eth0" || opts.ifname != "wan-vmac" {
		t.Fatalf("unexpected options: %#v", opts)
	}
	if got := commandsFor(opts); len(got) != 3 || got[0][0] != "ip" || got[0][1] != "link" || got[0][2] != "add" {
		t.Fatalf("activate commands: %#v", got)
	}
}

func TestParseOptionsRejectsInvalidInterface(t *testing.T) {
	if _, err := parseOptions([]string{"activate", "--parent", "eth0", "--interface", "bad name", "--mac", "02:00:5e:00:01:13"}); err == nil {
		t.Fatal("expected invalid interface error")
	}
}
