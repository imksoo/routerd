// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelftestRedactsSharedRuntimeConfig(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"selftest", "--interface", "vtnet0", "--username", "user", "--password", "secret value", "--runtime-dir", t.TempDir()}, &output); err != nil {
		t.Fatalf("selftest: %v", err)
	}
	if strings.Contains(output.String(), "secret value") {
		t.Fatalf("selftest leaked password: %s", output.String())
	}
	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode selftest: %v", err)
	}
	if result["peer"] == nil || result["freebsd"] == nil {
		t.Fatalf("selftest missing platform configs: %#v", result)
	}
}

func TestPPPoEDiagnosticRedactsPassword(t *testing.T) {
	for name, line := range map[string]string{
		"linux-pppd":   "pppd: authentication failed for secret value",
		"freebsd-mpd5": "mpd: authentication failed for secret value",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			stateFile := filepath.Join(dir, "state.json")
			eventFile := filepath.Join(dir, "events.jsonl")
			d := &daemon{opts: options{resource: "wan", password: "secret value", stateFile: stateFile, eventFile: eventFile}}
			d.scanLog(strings.NewReader(line))
			d.mu.Lock()
			gotDiagnostic := d.exitDiagnosticLocked(os.ErrInvalid)
			d.mu.Unlock()
			if strings.Contains(gotDiagnostic, "secret value") || !strings.Contains(gotDiagnostic, "[REDACTED]") {
				t.Fatalf("diagnostic = %q", gotDiagnostic)
			}
			for _, path := range []string{stateFile, eventFile} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read %s: %v", path, err)
				}
				got := string(data)
				if strings.Contains(got, "secret value") || !strings.Contains(got, "[REDACTED]") {
					t.Fatalf("persisted diagnostic %s = %q", path, got)
				}
			}
		})
	}
}

func TestParseFreeBSDPPPoEInterfaceObservation(t *testing.T) {
	addresses, err := parseFreeBSDPPPoEAddresses(`ppp39362e66: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST> metric 0 mtu 1454
		inet 198.18.10.2 --> 198.18.10.1 netmask 0xffffffff
`)
	if err != nil {
		t.Fatalf("parse FreeBSD PPPoE addresses: %v", err)
	}
	if addresses.CurrentAddress != "198.18.10.2" || addresses.PeerAddress != "198.18.10.1" {
		t.Fatalf("addresses = %#v", addresses)
	}
	bytesIn, bytesOut, err := parseFreeBSDPPPoECounters(`Name    Mtu Network       Address              Ipkts Ierrs Idrop Ibytes    Opkts Oerrs  Obytes Coll
ppp39362e66 1454 <Link#9>    00:00:00:00:00:00       7     0     0   4567       9     0    6789    0
`, "ppp39362e66")
	if err != nil {
		t.Fatalf("parse FreeBSD PPPoE counters: %v", err)
	}
	if bytesIn != 4567 || bytesOut != 6789 {
		t.Fatalf("counters = (%d, %d), want (4567, 6789)", bytesIn, bytesOut)
	}
}
