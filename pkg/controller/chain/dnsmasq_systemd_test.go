// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

// The CLI fixture only performs process operations and records their ordering.
// Production code still reads real executable/argv/birth and exit observations.
func dnsmasqSystemdFixture(t *testing.T, binary string) (dnsmasqProcessSpec, string, string) {
	t.Helper()
	spec := dnsmasqFixtureSpec(t, binary, "enable-ra\n")
	dir := filepath.Dir(spec.Config)
	unitPath := filepath.Join(dir, dnsmasqUnitName)
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
dir=` + shellQuote(dir) + `
unit=` + shellQuote(unitPath) + `
config=` + shellQuote(spec.Config) + `
pidfile=` + shellQuote(spec.PIDFile) + `
binary=` + shellQuote(spec.Command) + `
printf '%s\n' "$*" >> "$dir/systemctl.log"
case "$1" in
show)
 case "$2" in
 --property=FragmentPath) if test -f "$dir/foreign-fragment"; then echo /foreign/dnsmasq.service; else echo "$unit"; fi;;
 --property=DropInPaths) if test -f "$dir/foreign-override"; then echo /foreign/override.conf; fi;;
 --property=UnitFileState) cat "$dir/enabled";;
 --property=ActiveState) if test -f "$dir/read-failure"; then exit 77; fi; cat "$dir/active";;
 --property=MainPID) cat "$pidfile";;
 *) exit 90;;
 esac;;
daemon-reload) if test -f "$dir/fail-reload"; then exit 43; fi;;
enable) echo enabled > "$dir/enabled";;
disable) echo disabled > "$dir/enabled";;
reset-failed) echo inactive > "$dir/active";;
restart)
 cp "$unit.routerd-activation.json" "$dir/pending-at-restart"
 if test -f "$dir/fail-restart"; then exit 42; fi
 if test -f "$pidfile"; then kill -TERM "$(cat "$pidfile")" 2>/dev/null || true; fi
 "$binary" --fixture-detach --conf-file="$config" --pid-file="$pidfile"
 pid=$(cat "$pidfile")
 echo active > "$dir/active"
 count=0
 while ! test -f "$config.live.$pid"; do count=$((count+1)); if test "$count" -gt 100; then exit 91; fi; sleep 0.01; done;;
stop)
 if test -f "$dir/fail-stop"; then exit 44; fi
 if ! test -f "$dir/ignore-stop" && test -f "$pidfile"; then kill -TERM "$(cat "$pidfile")" 2>/dev/null || true; fi
 echo inactive > "$dir/active";;
reload) kill -HUP "$(cat "$pidfile")";;
*) exit 92;;
esac
`
	for path, data := range map[string]string{filepath.Join(binDir, "systemctl"): script, filepath.Join(dir, "enabled"): "disabled\n", filepath.Join(dir, "active"): "inactive\n"} {
		if err := os.WriteFile(path, []byte(data), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return spec, unitPath, dir
}

func dnsmasqUnitFixturePID(t *testing.T, spec dnsmasqProcessSpec) int {
	t.Helper()
	data, err := os.ReadFile(spec.PIDFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func TestDnsmasqSystemdActivationAndWithdrawal(t *testing.T) {
	binary := buildDnsmasqProcessFixture(t)
	spec, unitPath, dir := dnsmasqSystemdFixture(t, binary)
	ensure := func() error {
		return ensureSystemdDnsmasqService(t.Context(), dir, binary, spec.Config, spec.PIDFile, false)
	}
	if err := ensure(); err != nil {
		t.Fatal(err)
	}
	firstPID := dnsmasqUnitFixturePID(t, spec)
	if err := ensure(); err != nil {
		t.Fatal(err)
	}
	if dnsmasqUnitFixturePID(t, spec) != firstPID {
		t.Fatal("acknowledged unchanged input restarted")
	}
	if err := reloadSystemdDnsmasq(t.Context(), unitPath, spec.Config, spec.PIDFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.Config, []byte(routerdGeneratedDNSMasqMarker+"# DHCP only\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail-restart"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensure(); err == nil {
		t.Fatal("restart failure acknowledged")
	}
	receipt, err := artifactActivationReceipt(unitPath)
	if err != nil || receipt.AppliedDigest == "" || receipt.PendingDigest == "" {
		t.Fatalf("failed restart lost applied/pending: %+v %v", receipt, err)
	}
	if err := reloadSystemdDnsmasq(t.Context(), unitPath, spec.Config, spec.PIDFile); err == nil {
		t.Fatal("hosts HUP acknowledged pending main config")
	}
	if err := os.Remove(filepath.Join(dir, "fail-restart")); err != nil {
		t.Fatal(err)
	}
	if err := ensure(); err != nil {
		t.Fatal(err)
	}
	secondPID := dnsmasqUnitFixturePID(t, spec)
	if firstPID == secondPID {
		t.Fatal("equal-file retry did not replace old RA daemon")
	}
	if old, err := readNativeDnsmasqProcess(t.Context(), firstPID); err != nil || old.PID != 0 {
		t.Fatalf("old process still live after restart: %+v %v", old, err)
	}
	data, err := os.ReadFile(spec.Config + ".live." + strconv.Itoa(secondPID))
	if err != nil || strings.Contains(string(data), "enable-ra") {
		t.Fatalf("new process retained RA: %q %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail-stop"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := stopSystemdDnsmasq(t.Context(), unitPath, spec.Config, true); err == nil {
		t.Fatal("manager inactive claim acknowledged a still-live process")
	}
	receipt, err = artifactActivationReceipt(unitPath)
	if err != nil || !receipt.Removing {
		t.Fatalf("failed stop lost removal receipt: %+v %v", receipt, err)
	}
	if err := os.Remove(filepath.Join(dir, "fail-stop")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "active"), []byte("inactive\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore-stop"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := stopSystemdDnsmasq(t.Context(), unitPath, spec.Config, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stopped service unit remains: %v", err)
	}
	if process, err := readNativeDnsmasqProcess(t.Context(), secondPID); err != nil || process.PID != 0 {
		t.Fatalf("withdrawal did not stop old native process: %+v %v", process, err)
	}
}

func TestDnsmasqSystemdUnknownReadAndForeignFragmentPreserveProcess(t *testing.T) {
	binary := buildDnsmasqProcessFixture(t)
	spec, unitPath, dir := dnsmasqSystemdFixture(t, binary)
	if err := ensureSystemdDnsmasqService(t.Context(), dir, binary, spec.Config, spec.PIDFile, false); err != nil {
		t.Fatal(err)
	}
	pid := dnsmasqUnitFixturePID(t, spec)
	for _, failure := range []string{"read-failure", "foreign-fragment", "foreign-override"} {
		if err := os.WriteFile(filepath.Join(dir, failure), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := stopSystemdDnsmasq(t.Context(), unitPath, spec.Config, true); err == nil {
			t.Fatalf("%s granted stop authority", failure)
		}
		if live, err := readNativeDnsmasqProcess(t.Context(), pid); err != nil || live.PID != pid {
			t.Fatalf("%s signaled native process: %+v %v", failure, live, err)
		}
		if err := os.Remove(filepath.Join(dir, failure)); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := stopSystemdDnsmasq(ctx, unitPath, spec.Config, true); err != nil {
		t.Fatal(err)
	}
}

func TestDnsmasqNoDHCPControllerWithdrawsOwnedProcess(t *testing.T) {
	binary := buildDnsmasqProcessFixture(t)
	spec, _, _ := dnsmasqSystemdFixture(t, binary)
	if err := ensureDnsmasqFixture(t.Context(), spec, false); err != nil {
		t.Fatal(err)
	}
	pid := ownedDnsmasqPID(t, spec)
	ownerPath := dnsmasqOwnerPath(spec.PIDFile)
	owner, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	controller := DHCPv6ServerController{Router: &api.Router{}, Store: mapStore{}, Command: binary, ConfigPath: spec.Config, PIDFile: spec.PIDFile}
	if err := os.WriteFile(ownerPath, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := controller.reconcile(t.Context()); err == nil {
		t.Fatal("no-DHCP handoff ignored an unknown old process owner")
	}
	if live, err := readNativeDnsmasqProcess(t.Context(), pid); err != nil || live.PID != pid {
		t.Fatalf("unknown handoff signaled process: %+v %v", live, err)
	}
	if err := os.WriteFile(ownerPath, owner, 0600); err != nil {
		t.Fatal(err)
	}
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if live, err := readNativeDnsmasqProcess(t.Context(), pid); err != nil || live.PID != 0 {
		t.Fatalf("no-DHCP early return retained old RA daemon: %+v %v", live, err)
	}
}
