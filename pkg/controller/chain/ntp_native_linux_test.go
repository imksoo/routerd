//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
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

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type ntpNativeProcess struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func startNTPNativeProcess(t *testing.T, ctx context.Context, binary, config, logPath string) *ntpNativeProcess {
	t.Helper()
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	p := &ntpNativeProcess{command: exec.CommandContext(ctx, binary, "-x", "-n", "-u", "root", "-f", config), done: make(chan struct{})}
	p.command.Stdout, p.command.Stderr = log, log
	if err := p.command.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	go func() { p.err = p.command.Wait(); _ = log.Close(); close(p.done) }()
	t.Cleanup(func() {
		if err := p.stop(); err != nil && ctx.Err() == nil {
			t.Errorf("owned chronyd cleanup: %v", err)
		}
		if t.Failed() {
			if data, err := os.ReadFile(logPath); err == nil {
				if len(data) > 12000 {
					data = data[len(data)-12000:]
				}
				t.Logf("owned chronyd log %s:\n%s", logPath, data)
			}
		}
	})
	return p
}
func (p *ntpNativeProcess) running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
func (p *ntpNativeProcess) stop() error {
	if !p.running() {
		return p.err
	}
	_ = p.command.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
		return p.err
	case <-time.After(3 * time.Second):
		_ = p.command.Process.Kill()
	}
	select {
	case <-p.done:
		return p.err
	case <-time.After(3 * time.Second):
		return errors.New("owned chronyd did not terminate")
	}
}

// The runtime directories are shadow mounts inside a verified private mount
// namespace. The existing AppArmor policy remains in force; no host config,
// service, or clock is changed. Both chronyd processes use -x explicitly.
func TestNTPChronyNativeLinuxControllerLifecycle(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("set ROUTERD_RUN_NETNS=1 in an isolated user/network/mount namespace")
	}
	for _, item := range []struct{ path, env string }{{"/proc/self/ns/net", "ROUTERD_NATIVE_HOST_NS"}, {"/proc/self/ns/mnt", "ROUTERD_NATIVE_HOST_MNT"}} {
		current, err := os.Readlink(item.path)
		host := os.Getenv(item.env)
		if err != nil || host == "" || current == host {
			t.Fatalf("private namespace not proven: %s current=%s host=%s err=%v", item.path, current, host, err)
		}
	}
	chronyd, err := exec.LookPath("chronyd")
	if err != nil {
		t.Fatal(err)
	}
	chronyc, err := exec.LookPath("chronyc")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 90*time.Second)
	t.Cleanup(cancel)
	run := func(name string, args ...string) {
		t.Helper()
		if out, err := exec.CommandContext(ctx, name, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
	}
	run("mount", "--make-rprivate", "/")
	base := t.TempDir()
	for _, target := range []string{"/etc/chrony", "/run/chrony", "/var/lib/chrony", "/var/log/chrony"} {
		if info, err := os.Stat(target); err != nil || !info.IsDir() {
			t.Fatalf("existing private mount target %s unavailable: %v", target, err)
		}
		source := filepath.Join(base, strings.ReplaceAll(target[1:], "/", "-"))
		if err := os.Mkdir(source, 0700); err != nil {
			t.Fatal(err)
		}
		run("mount", "--bind", source, target)
	}
	run("ip", "link", "set", "lo", "up")
	write := func(t *testing.T, path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(t, "/etc/chrony/reference.conf", "bindaddress 127.0.0.2\nallow 127.0.0.0/8\nlocal stratum 10\ncmdport 0\nbindcmdaddress /run/chrony/reference.sock\npidfile /run/chrony/reference.pid\ndriftfile /var/lib/chrony/reference.drift\n")
	reference := startNTPNativeProcess(t, ctx, chronyd, "/etc/chrony/reference.conf", filepath.Join(base, "reference.log"))
	waitNTPNative(t, ctx, reference, func() bool { _, err := queryNTPNative("127.0.0.2:123"); return err == nil })
	for _, kind := range []string{"NTPClient", "NTPServer", "SharedTimesyncdChrony"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join("/etc/chrony", kind)
			fragments := filepath.Join(dir, "fragments")
			if err := os.MkdirAll(fragments, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(fragments, "routerd.conf")
			runtime := filepath.Join(dir, "runtime.conf")
			socket := "/run/chrony/" + kind + ".sock"
			write(t, runtime, "confdir "+fragments+"\ncmdport 0\nbindcmdaddress "+socket+"\npidfile /run/chrony/"+kind+".pid\ndriftfile /var/lib/chrony/"+kind+".drift\n")
			var provider *ntpNativeProcess
			command := func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "systemctl" || len(args) < 2 {
					return nil, fmt.Errorf("unexpected native NTP service command: %s %v", name, args)
				}
				unit := args[len(args)-1]
				switch args[0] {
				case "is-enabled":
					return nil, errors.New("no host timesyncd participates")
				case "is-active":
					if unit == "chrony.service" && provider != nil && provider.running() {
						return nil, nil
					}
					return nil, errors.New("private provider is inactive")
				case "restart", "enable":
					if unit != "chrony.service" {
						return nil, fmt.Errorf("foreign service command refused: %v", args)
					}
					if provider != nil {
						if err := provider.stop(); err != nil {
							return nil, err
						}
					}
					provider = startNTPNativeProcess(t, ctx, chronyd, runtime, filepath.Join(base, kind+fmt.Sprintf("-%d.log", time.Now().UnixNano())))
					return nil, nil
				}
				return nil, fmt.Errorf("foreign service command refused: %v", args)
			}
			db := filepath.Join(t.TempDir(), "state.db")
			store, err := routerstate.OpenSQLite(db)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			var router *api.Router
			if kind == "NTPClient" {
				router = ntpRouter(api.NTPClientSpec{Provider: "chrony", Managed: true, Servers: []string{"127.0.0.2"}})
			} else {
				router = ntpServerRouter(api.NTPServerSpec{Provider: "chrony", Managed: true, Servers: []string{"127.0.0.2"}, ListenAddresses: []string{"127.0.0.1"}, AllowCIDRs: []string{"127.0.0.0/8"}})
			}
			if kind == "SharedTimesyncdChrony" {
				// The server's separate upstream is not the working reference.
				// Selection of .2 proves that client inputs reached the sole owner.
				serverSpec, _ := router.Spec.Resources[0].NTPServerSpec()
				serverSpec.Servers = []string{"127.0.0.3"}
				serverSpec.When = api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ntp-role": {Equals: "master"}}}
				router.Spec.Resources[0].Spec = serverSpec
				router.Spec.Resources = append(router.Spec.Resources, ntpRouter(api.NTPClientSpec{Provider: "systemd-timesyncd", Managed: true, Servers: []string{"127.0.0.2"}}).Spec.Resources...)
				if _, err := store.Set("ntp-role", "master", "native fixture role"); err != nil {
					t.Fatal(err)
				}
			}
			router.TypeMeta = api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}
			router.Metadata.Name = "ntp-native"
			if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
				t.Fatal(err)
			}
			declared := router
			reconcile := func() error {
				client := NTPClientController{Router: router, DeclaredRouter: declared, Store: store, ConfigPath: path + ".client", Command: command}
				if kind == "NTPClient" {
					client.ConfigPath = path
					return client.Reconcile(ctx)
				}
				if kind == "SharedTimesyncdChrony" {
					if err := client.Reconcile(ctx); err != nil {
						return err
					}
				}
				if err := (NTPServerController{Router: router, DeclaredRouter: declared, Store: store, ConfigPath: path, Command: command}).Reconcile(ctx); err != nil {
					return err
				}
				if kind == "SharedTimesyncdChrony" {
					return client.Reconcile(ctx)
				}
				return nil
			}
			waitSource := func() {
				var last string
				defer func() {
					if t.Failed() {
						t.Logf("last independent chronyc sources: %s", last)
					}
				}()
				waitNTPNative(t, ctx, provider, func() bool {
					out, err := exec.CommandContext(ctx, chronyc, "-n", "-h", socket, "sources").CombinedOutput()
					last = string(out)
					if err != nil {
						return false
					}
					for _, line := range strings.Split(string(out), "\n") {
						fields := strings.Fields(line)
						if len(fields) > 1 && fields[0] == "^*" && fields[1] == "127.0.0.2" {
							t.Logf("independent chrony reference selected: %s", line)
							return true
						}
					}
					return false
				})
			}
			if err := reconcile(); err != nil {
				t.Fatal(err)
			}
			waitSource()
			if kind != "NTPClient" {
				waitNTPNative(t, ctx, provider, func() bool {
					stratum, err := queryNTPNative("127.0.0.1:123")
					if err == nil {
						t.Logf("independent UDP query accepted: mode=4 stratum=%d exact originate echo", stratum)
					}
					return err == nil
				})
			}
			// Reopen the actual store and recover a stopped provider with unchanged data.
			if err := provider.stop(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = routerstate.OpenSQLite(db)
			if err != nil {
				t.Fatal(err)
			}
			if err := reconcile(); err != nil {
				t.Fatal(err)
			}
			waitSource()
			if kind != "NTPClient" {
				waitNTPNative(t, ctx, provider, func() bool { _, err := queryNTPNative("127.0.0.1:123"); return err == nil })
			}
			if kind == "SharedTimesyncdChrony" {
				if _, err := store.Set("ntp-role", "backup", "native role withdrawal"); err != nil {
					t.Fatal(err)
				}
				router = mustWhenFilteredRouter(t, false, declared, store)
				if err := reconcile(); err != nil {
					t.Fatal(err)
				}
				waitSource()
				// Denial must be no UDP response, including malformed or
				// unsynchronised replies, while upstream selection survives.
				for i := 0; i < 3; i++ {
					if reply, _, err := exchangeNTPNative("127.0.0.1:123"); err == nil {
						t.Fatalf("inactive server replied to mode 3: %x", reply)
					}
				}
				status, err := store.ReadObjectStatus(api.SystemAPIVersion, "NTPClient", "system-time")
				if err != nil || status["phase"] != "Applied" || status["provider"] != "chrony" || status["requestedProvider"] != "systemd-timesyncd" {
					t.Fatalf("shared client status: %v err=%v", status, err)
				}
				t.Log("native-stage=shared-WhenFalse upstream-selected client-Applied response-none")
				if _, err := store.Set("ntp-role", "master", "native role restoration"); err != nil {
					t.Fatal(err)
				}
				router = declared
				if err := reconcile(); err != nil {
					t.Fatal(err)
				}
				waitSource()
				waitNTPNative(t, ctx, provider, func() bool { _, err := queryNTPNative("127.0.0.1:123"); return err == nil })
			}
			router = &api.Router{}
			declared = router
			if err := reconcile(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("owned native fragment remains after withdrawal: %v", err)
			}
			waitNTPNative(t, ctx, provider, func() bool {
				out, err := exec.CommandContext(ctx, chronyc, "-n", "-h", socket, "sources").CombinedOutput()
				return err == nil && !strings.Contains(string(out), "127.0.0.2")
			})
			if kind != "NTPClient" {
				if reply, _, err := exchangeNTPNative("127.0.0.1:123"); err == nil {
					t.Fatalf("withdrawn server still replies: %x", reply)
				}
			}
			if kind == "SharedTimesyncdChrony" {
				for _, item := range []struct{ kind, name string }{{"NTPClient", "system-time"}, {"NTPServer", "lan-time"}} {
					status, err := store.ReadObjectStatus(api.SystemAPIVersion, item.kind, item.name)
					if err != nil || status["phase"] != "Removed" || status["servers"] != nil || status["allowCIDRs"] != nil {
						t.Fatalf("public declaration was not retired: %v err=%v", status, err)
					}
				}
			}
			if err := provider.stop(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func waitNTPNative(t *testing.T, ctx context.Context, process *ntpNativeProcess, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if !process.running() {
			t.Fatalf("private chronyd exited: %v", process.err)
		}
		if ready() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("private chronyd did not reach the independently observed protocol state")
}

// This independent wire query checks the server response without adjusting time.
func queryNTPNative(address string) (byte, error) {
	reply, timestamp, err := exchangeNTPNative(address)
	if err != nil {
		return 0, err
	}
	if len(reply) < 48 || reply[0]&7 != 4 || reply[0]>>6 == 3 || reply[1] < 1 || reply[1] > 15 || binary.BigEndian.Uint64(reply[24:32]) != timestamp || binary.BigEndian.Uint64(reply[40:48]) == 0 {
		return 0, errors.New("independent NTP response did not confirm the request")
	}
	return reply[1], nil
}

func exchangeNTPNative(address string) ([]byte, uint64, error) {
	connection, err := net.DialTimeout("udp", address, 300*time.Millisecond)
	if err != nil {
		return nil, 0, err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(300 * time.Millisecond))
	request := make([]byte, 48)
	request[0] = 4<<3 | 3
	now := time.Now()
	timestamp := uint64(now.Unix()+2208988800)<<32 | uint64(now.Nanosecond())*(1<<32)/1e9
	binary.BigEndian.PutUint64(request[40:], timestamp)
	if _, err := connection.Write(request); err != nil {
		return nil, timestamp, err
	}
	reply := make([]byte, 512)
	n, err := connection.Read(reply)
	return reply[:n], timestamp, err
}
