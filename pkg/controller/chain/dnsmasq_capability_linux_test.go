//go:build linux && routerd_native

// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/imksoo/routerd/pkg/render"
)

// Run explicitly as root in a new PID/mount namespace. Ordinary tests do not
// exercise native privilege changes. The child models dnsmasq's dropped UID;
// the observer uses the actual service's capability bounding set.
func TestDnsmasqOwnerWithServiceCapabilities(t *testing.T) {
	if value := os.Getenv("ROUTERD_NATIVE_CAPABILITY_CHILD"); value != "" {
		pid, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		observed, err := readNativeDnsmasqProcess(t.Context(), pid)
		if err != nil {
			t.Fatalf("observe dropped-UID helper: %v", err)
		}
		if observed.PID != pid || observed.Scope == "" || observed.Birth == "" {
			t.Fatalf("missing native identity: %+v", observed)
		}
		if err := signalNativeDnsmasqProcess(t.Context(), observed, syscall.SIGTERM); err != nil {
			t.Fatalf("stop observed dropped-UID helper: %v", err)
		}
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("explicit native root test required")
	}
	setpriv, err := exec.LookPath("setpriv")
	if err != nil {
		t.Fatal(err)
	}
	sleeper := exec.Command("sleep", "30")
	sleeper.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	caps := []string{"-all"}
	for _, capability := range render.RouterdServiceSystemdSpec().CapabilityBoundingSet {
		caps = append(caps, "+"+strings.ToLower(strings.TrimPrefix(capability, "CAP_")))
	}
	observer := exec.Command(setpriv, "--bounding-set="+strings.Join(caps, ","), executable, "-test.run=^TestDnsmasqOwnerWithServiceCapabilities$", "-test.v")
	observer.Env = append(os.Environ(), "ROUTERD_NATIVE_CAPABILITY_CHILD="+strconv.Itoa(sleeper.Process.Pid))
	if out, err := observer.CombinedOutput(); err != nil {
		t.Fatalf("service capabilities cannot supervise dropped-UID child: %v\n%s", err, out)
	}
	if err := sleeper.Wait(); err == nil {
		t.Fatal("child exited without the requested termination")
	}
	status, ok := sleeper.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Fatalf("child was not stopped by the observed owner: %v", sleeper.ProcessState)
	}
}
