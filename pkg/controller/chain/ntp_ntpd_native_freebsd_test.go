//go:build freebsd

// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"golang.org/x/sys/unix"
)

type ntpdNativeProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	err     error
	stopped bool
}

func (p *ntpdNativeProcess) running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *ntpdNativeProcess) stop() error {
	if !p.running() {
		if p.stopped {
			return nil
		}
		return p.err
	}
	p.stopped = true
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
		return nil // This owner requested termination and completed Wait.
	case <-time.After(3 * time.Second):
		_ = p.cmd.Process.Kill()
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(3 * time.Second):
		return errors.New("owned ntpd did not terminate")
	}
}

func TestNTPNTPDClockGuard(t *testing.T) {
	if os.Getenv("ROUTERD_NTPD_HELPER") != "clock" {
		t.Skip("private VNET child only")
	}
	out, err := exec.Command("sysctl", "-n", "security.jail.jailed").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Fatalf("ntpd child is not jailed: %s %v", out, err)
	}
	// releng/14.3 kern_clock_settime checks privilege before validating Nsec.
	// Nsec=-1 cannot set a clock even if the privilege restriction regresses.
	invalid := unix.Timespec{Sec: 0, Nsec: -1}
	_, _, errno := unix.Syscall(unix.SYS_CLOCK_SETTIME, uintptr(unix.CLOCK_REALTIME), uintptr(unsafe.Pointer(&invalid)), 0)
	err = errno
	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("jail clock privilege was not denied: %v", err)
	}
	t.Log("jailed=1 invalid-timespec clock_settime=EPERM; no time value applied")
}

func TestNTPNTPDIndependentProbe(t *testing.T) {
	if os.Getenv("ROUTERD_NTPD_HELPER") != "probe" {
		t.Skip("private VNET child only")
	}
	local := net.ParseIP(os.Getenv("ROUTERD_NTPD_PROBE_SOURCE"))
	remote, err := net.ResolveUDPAddr("udp4", os.Getenv("ROUTERD_NTPD_PROBE_TARGET"))
	if err != nil || local.To4() == nil {
		t.Fatalf("invalid independent probe endpoints: %v", err)
	}
	want := os.Getenv("ROUTERD_NTPD_PROBE_WANT")
	if want != "reply" && want != "silent" {
		t.Fatal("probe requires reply or silent")
	}
	for attempt := 0; attempt < 3; attempt++ {
		conn, err := net.DialUDP("udp4", &net.UDPAddr{IP: local}, remote)
		if err != nil {
			t.Fatal(err)
		}
		request := make([]byte, 48)
		request[0] = 0x23 // NTPv4, client mode.
		now := time.Now()
		binary.BigEndian.PutUint64(request[40:], uint64(now.Unix()+2208988800)<<32|uint64(now.Nanosecond())<<32/1000000000)
		if err := conn.SetDeadline(time.Now().Add(700 * time.Millisecond)); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		if _, err := conn.Write(request); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		response := make([]byte, 512)
		n, readErr := conn.Read(response)
		conn.Close()
		if n > 0 {
			if want == "silent" {
				t.Fatalf("denied source %s received UDP reply: %x", local, response[:n])
			}
			if readErr != nil || n < 48 || response[0]&7 != 4 || response[0]>>6 == 3 || response[1] == 0 || response[1] > 15 || !bytes.Equal(response[24:32], request[40:48]) || binary.BigEndian.Uint64(response[40:48]) == 0 {
				t.Fatalf("allowed source received unusable NTP response: bytes=%x err=%v", response[:n], readErr)
			}
			t.Logf("source=%s mode=4 LI=%d stratum=%d originate matched; reply=%x", local, response[0]>>6, response[1], response[:48])
			return
		}
		var networkErr net.Error
		if !errors.As(readErr, &networkErr) || !networkErr.Timeout() && !errors.Is(readErr, syscall.ECONNREFUSED) {
			t.Fatalf("probe failed without an interpretable receive result: %v", readErr)
		}
		if want == "reply" && attempt == 2 {
			t.Fatalf("allowed source received no NTP response: %v", readErr)
		}
	}
	t.Logf("source=%s received no UDP response in three independent queries", local)
}

func TestNTPNTPDNativeFreeBSDControllerLifecycle(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NTPD_VNET") != "1" {
		t.Skip("run scripts/freebsd-ntp-runtime-smoke.sh with private VNET jails")
	}
	managed, reference := os.Getenv("ROUTERD_NTPD_MANAGED_JAIL"), os.Getenv("ROUTERD_NTPD_REFERENCE_JAIL")
	base := os.Getenv("ROUTERD_NTPD_WORK")
	if !strings.HasPrefix(managed, "routerd-ntpd-") || !strings.HasPrefix(reference, "routerd-ntpd-") || managed == reference || !filepath.IsAbs(base) {
		t.Fatal("private jail identities and evidence directory are required")
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 150*time.Second)
	t.Cleanup(cancel)
	binaryPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := func(jail, test, helper string, extra ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "jexec", jail, binaryPath, "-test.run", "^"+test+"$", "-test.v", "-test.count=1", "-test.timeout=10s")
		cmd.Env = append(os.Environ(), append([]string{"ROUTERD_NTPD_HELPER=" + helper}, extra...)...)
		out, err := cmd.CombinedOutput()
		t.Logf("independent %s/%s:\n%s", jail, helper, out)
		if err != nil || !bytes.Contains(out, []byte("--- PASS: "+test)) || bytes.Contains(out, []byte("--- SKIP:")) {
			t.Fatalf("independent jailed child: %v", err)
		}
	}
	child(managed, "TestNTPNTPDClockGuard", "clock")
	child(reference, "TestNTPNTPDClockGuard", "clock")
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	start := func(jail, config, label string) *ntpdNativeProcess {
		t.Helper()
		logPath := filepath.Join(base, fmt.Sprintf("%s-%d.log", label, time.Now().UnixNano()))
		log, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		p := &ntpdNativeProcess{cmd: exec.CommandContext(ctx, "jexec", jail, "/usr/sbin/ntpd", "-n", "-c", config, "-f", filepath.Join(base, label+".drift"), "-p", filepath.Join(base, label+".pid"), "-l", logPath+".daemon"), done: make(chan struct{})}
		p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
		p.cmd.Stdout, p.cmd.Stderr = log, log
		if err := p.cmd.Start(); err != nil {
			log.Close()
			t.Fatal(err)
		}
		go func() { p.err = p.cmd.Wait(); log.Close(); close(p.done) }()
		t.Cleanup(func() {
			if err := p.stop(); err != nil {
				t.Errorf("owned ntpd cleanup: %v", err)
			}
			if !p.running() {
				t.Logf("owned ntpd child pid=%d Wait completed", p.cmd.Process.Pid)
			}
			if t.Failed() {
				for _, path := range []string{logPath, logPath + ".daemon"} {
					if data, err := os.ReadFile(path); err == nil {
						if len(data) > 12000 {
							data = data[len(data)-12000:]
						}
						t.Logf("%s:\n%s", path, data)
					}
				}
			}
		})
		return p
	}
	waitSource := func(jail string, process *ntpdNativeProcess, source string) {
		t.Helper()
		deadline := time.Now().Add(40 * time.Second)
		last := ""
		for time.Now().Before(deadline) && ctx.Err() == nil && process.running() {
			queryCtx, queryCancel := context.WithTimeout(ctx, 2*time.Second)
			out, err := exec.CommandContext(queryCtx, "jexec", jail, "/usr/bin/ntpq", "-4", "-n", "-p", "127.0.0.1").CombinedOutput()
			queryCancel()
			last = string(out)
			if err == nil {
				for _, line := range strings.Split(last, "\n") {
					fields := strings.Fields(line)
					if len(fields) > 0 && strings.HasPrefix(fields[0], "*") && strings.Contains(strings.TrimPrefix(fields[0], "*"), source) {
						t.Logf("actual ntpq selected source (%s):\n%s", jail, last)
						return
					}
				}
			}
			time.Sleep(150 * time.Millisecond)
		}
		t.Fatalf("actual ntpd never selected %s: running=%v context=%v ntpq=%s", source, process.running(), ctx.Err(), last)
	}
	referenceConfig := filepath.Join(base, "reference.conf")
	write(referenceConfig, "disable ntp\ninterface ignore all\ninterface listen 127.0.0.1\ninterface listen 198.18.97.2\nrestrict default nomodify notrap nopeer noquery\nrestrict 127.0.0.1\nserver 127.127.1.0 minpoll 4 maxpoll 4\nfudge 127.127.1.0 stratum 10\ndriftfile "+filepath.Join(base, "reference.drift")+"\n")
	referenceProcess := start(reference, referenceConfig, "reference")
	waitSource(reference, referenceProcess, "LOCAL")
	fragment, runtime := filepath.Join(base, "routerd.conf"), filepath.Join(base, "runtime.conf")
	// Isolation controls only: no ACL or source is added outside the generated
	// fragment. The fragment is consumed verbatim by the real provider.
	write(runtime, "disable ntp\nincludefile "+fragment+"\ndisable ntp\ndriftfile "+filepath.Join(base, "managed.drift")+"\n")
	var provider *ntpdNativeProcess
	command := func(operation context.Context, name string, args ...string) ([]byte, error) {
		if err := operation.Err(); err != nil {
			return nil, err
		}
		if name == "sysrc" && len(args) == 1 && (args[0] == "ntpd_enable=YES" || args[0] == "ntpd_sync_on_start=YES" || args[0] == "ntpd_config="+fragment) {
			return nil, nil // Never dispatch shared rc.conf mutations.
		}
		if name != "service" || len(args) != 2 || args[0] != "ntpd" {
			return nil, fmt.Errorf("unexpected private provider command: %s %v", name, args)
		}
		switch args[1] {
		case "status":
			if provider != nil && provider.running() {
				return nil, nil
			}
			return nil, errors.New("owned ntpd is stopped")
		case "restart", "onestart":
			if provider != nil {
				if err := provider.stop(); err != nil {
					return nil, err
				}
			}
			if _, err := os.Stat(fragment); err != nil {
				return nil, err
			}
			if err := operation.Err(); err != nil {
				return nil, err
			}
			provider = start(managed, runtime, "managed")
			return nil, nil
		}
		return nil, fmt.Errorf("unowned ntpd service action refused: %v", args)
	}
	statePath := filepath.Join(base, "state.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.Set("ntp-role", "master", "private native fixture"); err != nil {
		t.Fatal(err)
	}
	full := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "ntpd-native"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"}, Metadata: api.ObjectMeta{Name: "upstream"}, Spec: api.NTPClientSpec{Provider: "ntpd", Managed: true, Servers: []string{"198.18.97.2"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPServer"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.NTPServerSpec{Provider: "ntpd", Managed: true, Servers: []string{"198.18.97.2"}, AllowCIDRs: []string{"198.18.97.10/32"}, ListenAddresses: []string{"198.18.97.1"}, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ntp-role": {Equals: "master"}}}}},
	}}}
	if err := config.ValidateForOS(full, platform.OSFreeBSD); err != nil {
		t.Fatal(err)
	}
	effective := full
	reconcile := func() {
		t.Helper()
		client := NTPClientController{Router: effective, DeclaredRouter: full, Store: store, ConfigPath: fragment, Command: command}
		server := NTPServerController{Router: effective, DeclaredRouter: full, Store: store, ConfigPath: fragment, Command: command}
		if err := client.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		if err := server.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		if err := client.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	probe := func(source, want string) {
		t.Helper()
		if provider == nil || !provider.running() {
			t.Fatal("ACL observation requires the owned provider to remain alive")
		}
		child(reference, "TestNTPNTPDIndependentProbe", "probe", "ROUTERD_NTPD_PROBE_SOURCE="+source, "ROUTERD_NTPD_PROBE_TARGET=198.18.97.1:123", "ROUTERD_NTPD_PROBE_WANT="+want)
		if !provider.running() {
			t.Fatal("provider exited during ACL observation")
		}
	}
	reconcile()
	waitSource(managed, provider, "198.18.97.2")
	probe("198.18.97.10", "reply")
	probe("198.18.97.20", "silent")
	t.Log("native-stage=allowed-and-denied")
	if err := provider.stop(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	reconcile()
	waitSource(managed, provider, "198.18.97.2")
	probe("198.18.97.10", "reply")
	probe("198.18.97.20", "silent")
	t.Log("native-stage=owned-process-restart-and-sqlite-reopen")
	if _, err := store.Set("ntp-role", "backup", "private native role change"); err != nil {
		t.Fatal(err)
	}
	effective = &api.Router{Spec: api.RouterSpec{Resources: full.Spec.Resources[:1]}}
	reconcile()
	waitSource(managed, provider, "198.18.97.2")
	probe("198.18.97.10", "silent")
	probe("198.18.97.20", "silent")
	t.Log("native-stage=WhenFalse-denies-clients-and-keeps-upstream")
	if _, err := store.Set("ntp-role", "master", "private native role restored"); err != nil {
		t.Fatal(err)
	}
	effective = full
	reconcile()
	waitSource(managed, provider, "198.18.97.2")
	probe("198.18.97.10", "reply")
	probe("198.18.97.20", "silent")
	t.Log("native-stage=WhenTrue-recovery")
}
