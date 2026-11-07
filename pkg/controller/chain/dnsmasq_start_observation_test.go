// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"os/exec"
	"testing"
)

// A just-started child can still expose the pre-exec executable and argv in
// procfs. Keep that state observable with an independent launcher, then exec
// the expected fixture without changing its PID.
func TestDnsmasqStartWaitsForExecutableTransition(t *testing.T) {
	binary := buildDnsmasqProcessFixture(t)
	spec := dnsmasqFixtureSpec(t, binary, "enable-ra\n")
	cmd := exec.Command("sh", "-c", `sleep 0.2; exec "$@"`, "launcher", spec.Command, "--keep-in-foreground", "--conf-file="+spec.Config, "--pid-file="+spec.PIDFile)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	defer func() { _ = cmd.Process.Kill(); <-done }()
	process, err := waitStartedDnsmasq(t.Context(), spec, cmd.Process.Pid, done)
	if err != nil {
		t.Fatal(err)
	}
	if process.PID != cmd.Process.Pid || !dnsmasqProcessMatches(process, spec) {
		t.Fatal("startup accepted an unverified process")
	}
}

func TestDnsmasqStartRejectsPersistentIdentityMismatch(t *testing.T) {
	binary := buildDnsmasqProcessFixture(t)
	spec := dnsmasqFixtureSpec(t, binary, "enable-ra\n")
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	defer func() { _ = cmd.Process.Kill(); <-done }()
	process, err := waitStartedDnsmasq(t.Context(), spec, cmd.Process.Pid, done)
	if err == nil || process.PID != 0 {
		t.Fatal("startup adopted a persistently mismatching executable")
	}
}
