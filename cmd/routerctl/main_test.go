package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	routerstate "routerd/pkg/state"
)

func TestShowPDCommandPrintsStateRows(t *testing.T) {
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		CurrentPrefix:  "2001:db8:1200:1220::/60",
		LastPrefix:     "2001:db8:1200:1220::/60",
		LastObservedAt: "2026-04-28T01:02:03Z",
		LastMissingAt:  "2026-04-28T02:03:04Z",
		DUIDText:       "00:03:00:01:02:00:5e:10:20:30",
		IAID:           "0",
		ExpectedDUID:   "0003000102005e102030",
		IdentitySource: "systemd-networkd-link",
	}), "test")
	path := filepath.Join(t.TempDir(), "state.json")
	if err := store.Save(path); err != nil {
		t.Fatalf("save state: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"show", "pd", "--state-file", path}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("show pd: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"NAME",
		"wan-pd",
		"00:03:00:01:02:00:5e:10:20:30",
		"2001:db8:1200:1220::/60",
		"systemd-networkd-link",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("show pd output missing %q:\n%s", want, got)
		}
	}
}

func TestShowPDCommandMigratesLegacyStateForDisplay(t *testing.T) {
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.currentPrefix", "2001:db8:1200:1220::/60", "test")
	store.Set("ipv6PrefixDelegation.wan-pd.lastPrefix", "2001:db8:1200:1210::/60", "test")
	store.Set("ipv6PrefixDelegation.wan-pd.iaid", "7", "test")
	path := filepath.Join(t.TempDir(), "state.json")
	if err := store.Save(path); err != nil {
		t.Fatalf("save state: %v", err)
	}

	var out bytes.Buffer
	if err := showPDCommand([]string{"--state-file", path}, &out); err != nil {
		t.Fatalf("show pd: %v", err)
	}
	got := out.String()
	for _, want := range []string{"wan-pd", "2001:db8:1200:1220::/60", "2001:db8:1200:1210::/60", "  7  "} {
		if !strings.Contains(got, want) {
			t.Fatalf("show pd output missing %q:\n%s", want, got)
		}
	}
}

func TestShowPDCommandMissingStateFilePrintsHeader(t *testing.T) {
	var out bytes.Buffer
	if err := showPDCommand([]string{"--state-file", filepath.Join(t.TempDir(), "missing.json")}, &out); err != nil {
		t.Fatalf("show pd: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "NAME") {
		t.Fatalf("show pd output = %q, want header", got)
	}
}

func TestDefaultStatePathUsesPlatformStateDir(t *testing.T) {
	if got := defaultStatePath(); got == "" || filepath.Base(got) != "state.json" {
		t.Fatalf("default state path = %q", got)
	}
}
