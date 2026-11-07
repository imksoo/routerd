// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func buildDnsmasqProcessFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	binary := filepath.Join(dir, "dnsmasq-fixture")
	code := `package main
import("os";"os/exec";"os/signal";"strconv";"strings";"syscall")
func main(){
config,pid:="",""
detach:=false
for _,arg:=range os.Args[1:]{if arg=="--test"{return};if arg=="--fixture-detach"{detach=true};if strings.HasPrefix(arg,"--conf-file="){config=strings.TrimPrefix(arg,"--conf-file=")};if strings.HasPrefix(arg,"--pid-file="){pid=strings.TrimPrefix(arg,"--pid-file=")}}
if detach{cmd:=exec.Command(os.Args[0],"--keep-in-foreground","--conf-file="+config,"--pid-file="+pid);cmd.SysProcAttr=&syscall.SysProcAttr{Setsid:true};if cmd.Start()!=nil{os.Exit(5)};if os.WriteFile(pid,[]byte(strconv.Itoa(cmd.Process.Pid)+"\n"),0600)!=nil{os.Exit(6)};return}
data,err:=os.ReadFile(config);if err!=nil{os.Exit(2)}
if strings.Contains(string(data),"fail-start"){os.Exit(33)}
suffix:=strconv.Itoa(os.Getpid())
_ = os.WriteFile(config+".live."+suffix,data,0600)
owner,_:=os.ReadFile(pid+".routerd-owner.json");_ = os.WriteFile(config+".start-owner."+suffix,owner,0600)
activation,_:=os.ReadFile(config+".routerd-activation.json");_ = os.WriteFile(config+".start-activation."+suffix,activation,0600)
ch:=make(chan os.Signal,4);signal.Notify(ch,syscall.SIGTERM,syscall.SIGHUP)
for event:=range ch{f,_:=os.OpenFile(config+".signals",os.O_APPEND|os.O_WRONLY|os.O_CREATE,0600);if f!=nil{_,_=f.WriteString(event.String()+"\n");_ = f.Close()};if event==syscall.SIGTERM{if strings.Contains(string(data),"ignore-term"){continue};return}}
}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", binary, source)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build independent process fixture: %v: %s", err, out)
	}
	return binary
}

func dnsmasqFixtureSpec(t *testing.T, binary, body string) dnsmasqProcessSpec {
	t.Helper()
	dir := t.TempDir()
	spec, err := dnsmasqSpec(binary, filepath.Join(dir, "dnsmasq.conf"), filepath.Join(dir, "dnsmasq.pid"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.Config, []byte(routerdGeneratedDNSMasqMarker+body), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		dnsmasqMu.Lock()
		defer dnsmasqMu.Unlock()
		if err := stopNativeDnsmasq(ctx, spec); err != nil {
			t.Errorf("fixture cleanup: %v", err)
		}
	})
	return spec
}

func ensureDnsmasqFixture(ctx context.Context, spec dnsmasqProcessSpec, changed bool) error {
	dnsmasqMu.Lock()
	defer dnsmasqMu.Unlock()
	return ensureNativeDnsmasq(ctx, spec, changed)
}

func ownedDnsmasqPID(t *testing.T, spec dnsmasqProcessSpec) int {
	t.Helper()
	owner, err := readDnsmasqOwner(spec.PIDFile)
	if err != nil || owner == nil {
		t.Fatalf("owner unavailable: %+v %v", owner, err)
	}
	return owner.Process.PID
}

func TestDnsmasqNativeOwnerLifecycle(t *testing.T) {
	binary := buildDnsmasqProcessFixture(t)
	t.Run("main config restarts after changed flag was lost", func(t *testing.T) {
		spec := dnsmasqFixtureSpec(t, binary, "enable-ra\n")
		if err := ensureDnsmasqFixture(t.Context(), spec, false); err != nil {
			t.Fatal(err)
		}
		oldPID := ownedDnsmasqPID(t, spec)
		if err := os.WriteFile(spec.Config, []byte(routerdGeneratedDNSMasqMarker+"# DHCP only, RA withdrawn\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := ensureDnsmasqFixture(t.Context(), spec, false); err != nil {
			t.Fatal(err)
		}
		newPID := ownedDnsmasqPID(t, spec)
		if newPID == oldPID {
			t.Fatal("main config replacement used HUP or unchanged file acknowledgment")
		}
		if old, err := readNativeDnsmasqProcess(t.Context(), oldPID); err != nil || old.PID != 0 {
			t.Fatalf("old process remained after restart: %+v %v", old, err)
		}
		data, err := os.ReadFile(spec.Config + ".live." + strconv.Itoa(newPID))
		if err != nil || strings.Contains(string(data), "enable-ra") {
			t.Fatalf("replacement did not load new config: %s %v", data, err)
		}
		if err := ensureDnsmasqFixture(t.Context(), spec, false); err != nil {
			t.Fatal(err)
		}
		if ownedDnsmasqPID(t, spec) != newPID {
			t.Fatal("unchanged acknowledged config restarted")
		}
		ownerData, err := os.ReadFile(spec.Config + ".start-owner." + strconv.Itoa(newPID))
		var owner dnsmasqProcessOwner
		if err != nil || json.Unmarshal(ownerData, &owner) != nil || owner.State != "starting" {
			t.Fatalf("process started before durable intent: %s %v", ownerData, err)
		}
		activationData, err := os.ReadFile(spec.Config + ".start-activation." + strconv.Itoa(newPID))
		var activation artifactActivation
		if err != nil || json.Unmarshal(activationData, &activation) != nil || activation.PendingDigest == "" {
			t.Fatalf("process started before activation pending: %s %v", activationData, err)
		}
		if err := reloadOwnedDnsmasqHosts(t.Context(), spec.PIDFile); err != nil {
			t.Fatal(err)
		}
		waitForCondition(t, time.Second, "owned dnsmasq hosts reload", func() bool {
			data, _ := os.ReadFile(spec.Config + ".signals")
			return strings.Contains(string(data), "hangup")
		})
		if ownedDnsmasqPID(t, spec) != newPID {
			t.Fatal("hosts sidecar reload restarted main config")
		}
	})
	t.Run("failed startup retries without changed flag", func(t *testing.T) {
		spec := dnsmasqFixtureSpec(t, binary, "# fail-start\n")
		if err := ensureDnsmasqFixture(t.Context(), spec, false); err == nil {
			t.Fatal("failed startup acknowledged")
		}
		activation, err := artifactActivationReceipt(spec.Config)
		if err != nil || activation.PendingDigest == "" {
			t.Fatalf("startup failure lost pending input: %+v %v", activation, err)
		}
		if err := os.WriteFile(spec.Config, []byte(routerdGeneratedDNSMasqMarker+"# recovered\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := ensureDnsmasqFixture(t.Context(), spec, false); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("interrupted caller keeps recoverable starting process", func(t *testing.T) {
		spec := dnsmasqFixtureSpec(t, binary, "# normal\n")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		observed := make(chan struct{})
		go func() {
			defer close(observed)
			for {
				owner, _ := readDnsmasqOwner(spec.PIDFile)
				if owner != nil && owner.Process.PID > 0 {
					cancel()
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Millisecond):
				}
			}
		}()
		if err := ensureDnsmasqFixture(ctx, spec, false); !errors.Is(err, context.Canceled) {
			t.Fatalf("interrupted startup = %v", err)
		}
		<-observed
		if err := ensureDnsmasqFixture(t.Context(), spec, false); err != nil {
			t.Fatal(err)
		}
		if owner, err := readDnsmasqOwner(spec.PIDFile); err != nil || owner.State != "running" {
			t.Fatalf("starting retry did not converge: %+v %v", owner, err)
		}
	})
	t.Run("stop waits and joins even after TERM ignored", func(t *testing.T) {
		spec := dnsmasqFixtureSpec(t, binary, "# ignore-term\n")
		if err := ensureDnsmasqFixture(t.Context(), spec, false); err != nil {
			t.Fatal(err)
		}
		pid := ownedDnsmasqPID(t, spec)
		dnsmasqMu.Lock()
		err := stopNativeDnsmasq(t.Context(), spec)
		dnsmasqMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		if process, err := readNativeDnsmasqProcess(t.Context(), pid); err != nil || process.PID != 0 {
			t.Fatalf("stop acknowledged live process: %+v %v", process, err)
		}
		if _, err := os.Stat(dnsmasqOwnerPath(spec.PIDFile)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stop retained owner: %v", err)
		}
	})
}

func TestDnsmasqNativeOwnerRefusesForeignAndUnknownPID(t *testing.T) {
	binary := buildDnsmasqProcessFixture(t)
	for _, contents := range []string{"-1\n", "0\n", strconv.Itoa(os.Getpid()) + "\n", "invalid\n"} {
		t.Run(strings.TrimSpace(contents), func(t *testing.T) {
			spec := dnsmasqFixtureSpec(t, binary, "# native\n")
			if err := os.WriteFile(spec.PIDFile, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if err := ensureDnsmasqFixture(t.Context(), spec, false); err == nil {
				t.Fatal("invalid PID authorized service effects")
			}
			if _, err := os.Stat(dnsmasqOwnerPath(spec.PIDFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid PID gained owner: %v", err)
			}
			if err := os.Remove(spec.PIDFile); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("foreign process with different config", func(t *testing.T) {
		spec := dnsmasqFixtureSpec(t, binary, "# managed\n")
		foreignDir := t.TempDir()
		foreignConfig := filepath.Join(foreignDir, "foreign.conf")
		foreignPID := filepath.Join(foreignDir, "foreign.pid")
		if err := os.WriteFile(foreignConfig, []byte("# administrator\n"), 0600); err != nil {
			t.Fatal(err)
		}
		process := exec.Command(binary, "--keep-in-foreground", "--conf-file="+foreignConfig, "--pid-file="+foreignPID)
		if err := process.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = process.Process.Kill(); _ = process.Wait() })
		if err := os.WriteFile(spec.PIDFile, []byte(strconv.Itoa(process.Process.Pid)), 0600); err != nil {
			t.Fatal(err)
		}
		if err := ensureDnsmasqFixture(t.Context(), spec, false); err == nil {
			t.Fatal("foreign process adopted")
		}
		dnsmasqMu.Lock()
		err := stopNativeDnsmasq(t.Context(), spec)
		dnsmasqMu.Unlock()
		if err == nil {
			t.Fatal("foreign process stop succeeded")
		}
		if current, err := readNativeDnsmasqProcess(t.Context(), process.Process.Pid); err != nil || current.PID == 0 {
			t.Fatalf("foreign process was signaled: %+v %v", current, err)
		}
		if err := os.Remove(spec.PIDFile); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDnsmasqNativeUnknownReceiptOrPIDReadDoesNotSignal(t *testing.T) {
	binary := buildDnsmasqProcessFixture(t)
	spec := dnsmasqFixtureSpec(t, binary, "# native\n")
	if err := ensureDnsmasqFixture(t.Context(), spec, false); err != nil {
		t.Fatal(err)
	}
	pid := ownedDnsmasqPID(t, spec)
	ownerPath := dnsmasqOwnerPath(spec.PIDFile)
	owner, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureDnsmasqFixture(t.Context(), spec, false); err == nil {
		t.Fatal("malformed native receipt authorized activation")
	}
	if err := os.WriteFile(ownerPath, owner, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(spec.PIDFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(spec.PIDFile, 0700); err != nil {
		t.Fatal(err)
	}
	if err := reloadOwnedDnsmasqHosts(t.Context(), spec.PIDFile); err == nil {
		t.Fatal("failed PID read authorized a reload")
	}
	if err := os.Remove(spec.PIDFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.PIDFile, []byte(strconv.Itoa(pid)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if live, err := readNativeDnsmasqProcess(t.Context(), pid); err != nil || live.PID != pid {
		t.Fatalf("unknown read signaled process: %+v %v", live, err)
	}
	if data, err := os.ReadFile(spec.Config + ".signals"); !errors.Is(err, os.ErrNotExist) || len(data) != 0 {
		t.Fatalf("unknown read delivered a signal: %s %v", data, err)
	}
}
