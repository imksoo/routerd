// SPDX-License-Identifier: BSD-3-Clause

//go:build linux || freebsd

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeSmokeCannotPublishPassWithoutNativeAdmission(t *testing.T) {
	// Only the platform-admission command is supplied. The actual script must
	// exit before interface creation, process launch or any product operation.
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(bin, "unexecuted-client")
	if err := os.Symlink(self, fixture); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "uname"), []byte("#!/bin/sh\nprintf 'Linux\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	evidence := t.TempDir()
	command := exec.Command("sh", "../../scripts/freebsd-lifecycle-runtime-smoke.sh",
		"--dhcpv4-client", fixture, "--dhcpv6-client", fixture, "--dns-resolver", fixture,
		"--routerd", fixture, "--lifecycle-helper", fixture, "--evidence-dir", evidence)
	out, err := command.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 3 || !strings.Contains(string(out), "native FreeBSD is required") {
		t.Fatalf("unsupported native target fabricated success: %v: %s", err, out)
	}
	result, err := os.ReadFile(filepath.Join(evidence, "result"))
	if err != nil || string(result) != "freebsd-lifecycle-runtime=not-run\n" {
		t.Fatalf("unrun native test has a false result: %s %v", result, err)
	}
}

func TestNativeSmokeSelectsFullActualDnsmasqReceipt(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("native harness requires jq")
	}
	source, err := os.ReadFile("../../scripts/freebsd-lifecycle-runtime-smoke.sh")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(source), "wait_managed_owner() {\n")
	if start < 0 {
		t.Fatal("native owner shell boundary is missing")
	}
	body := string(source[start:])
	end := strings.Index(body, "\n}\n")
	if end < 0 {
		t.Fatal("native owner shell function is incomplete")
	}
	body = body[:end+3]
	dir := t.TempDir()
	receipt, output := filepath.Join(dir, "owner.json"), filepath.Join(dir, "observed.json")
	// These are the actual dnsmasqProcessOwner JSON tags and exported native
	// fields. Exercise the script's exact decoder and retain the full receipt.
	if err := os.WriteFile(receipt, []byte(`{"scope":"freebsd:fixture:0","state":"running","spec":{"Config":"/private/dnsmasq.conf"},"process":{"PID":73,"Birth":"Tue Oct 27 12:00:00 2026","Scope":"freebsd:fixture:0"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "-c", "set -eu\n"+body+"\njot() { printf '1\\n'; }\nprocess_alive() { return 1; }\nmanaged_dnsmasq_owner=$1\nmanaged_config=/private/dnsmasq.conf\nwait_managed_owner \"$2\" 72\njq -e '.process.PID == 73 and .spec.Config == \"/private/dnsmasq.conf\"' \"$2\"\n", "receipt-check", receipt, output)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("actual shell receipt decoder lost identity or replaced it with a boolean: %v: %s", err, out)
	}
}
