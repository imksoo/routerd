// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	state "github.com/imksoo/routerd/pkg/state"
)

func TestNativeOwnerCLIHelper(t *testing.T) {
	mode := os.Getenv("ROUTERD_NATIVE_OWNER_HELPER")
	if mode == "" {
		return
	}
	platformDefaults.RuntimeDir = os.Getenv("ROUTERD_NATIVE_OWNER_RUNTIME")
	if mode == "retained" || mode == "normal" {
		owner, err := acquireNativeOperationOwner(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.finish(mode == "normal"); err != nil {
			t.Fatal(err)
		}
		runtime.GC()
		if err := os.WriteFile(os.Getenv("ROUTERD_NATIVE_OWNER_MARKER"), []byte("finished"), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Second)
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("ROUTERD_NATIVE_OWNER_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	var err error
	switch mode {
	case "serve":
		err = serveCommand(args, io.Discard, os.Stderr)
	case "apply":
		err = applyCommand(args, io.Discard, os.Stderr)
	case "rollback":
		err = rollbackCommand(args, io.Discard, os.Stderr)
	case "validate":
		err = validateCommand(args, io.Discard)
	default:
		t.Fatalf("unknown private command %q", mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func nativeOwnerChild(t *testing.T, root, mode string, args []string, marker string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	output := &bytes.Buffer{}
	command := exec.Command(os.Args[0], "-test.run=^TestNativeOwnerCLIHelper$")
	command.Env = append(os.Environ(), "ROUTERD_NATIVE_OWNER_HELPER="+mode,
		"ROUTERD_NATIVE_OWNER_RUNTIME="+filepath.Join(root, "runtime"),
		"ROUTERD_NATIVE_OWNER_MARKER="+marker, "ROUTERD_NATIVE_OWNER_ARGS="+string(data),
		"PATH="+filepath.Join(root, "bin")+":/usr/bin:/bin")
	command.Stdout, command.Stderr = output, output
	return command, output
}

func waitNativeOwnerMarker(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("private process did not reach %s", path)
}

func nativeOwnerArguments(root, config, statePath string, once bool) []string {
	args := []string{"--config", config, "--state-file", statePath, "--ledger-file", filepath.Join(root, "ledger.db"),
		"--status-file", filepath.Join(root, "status.json"), "--netplan-file", filepath.Join(root, "netplan.yaml"),
		"--dnsmasq-file", filepath.Join(root, "dnsmasq.conf"), "--dnsmasq-service-file", filepath.Join(root, "dnsmasq.service"),
		"--nftables-file", filepath.Join(root, "nftables.conf")}
	if once {
		args = append(args, "--once")
	}
	return args
}

func TestNativeOwnerRejectsConcurrentCLIAndKeepsPendingReceipt(t *testing.T) {
	for _, once := range []bool{true, false} {
		t.Run(fmt.Sprint(once), func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "bin"), 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(root, "native-boundary")
			script := "#!/bin/sh\ncase \"$*\" in\n 'link set lo up') exit 0;;\n 'link add '*) printf reached > \"$ROUTERD_NATIVE_OWNER_MARKER\"; exec /bin/sleep 30;;\n *) case \" $* \" in *' -j '*) printf '[]\\n'; exit 0;; esac;;\nesac\nexit 97\n"
			if err := os.WriteFile(filepath.Join(root, "bin/ip"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(root, "config.yaml")
			config := `apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: owner-fixture}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: wan}
      spec: {ifname: qa-private-wan, managed: false, owner: external}
    - apiVersion: net.routerd.net/v1alpha1
      kind: DSLiteTunnel
      metadata: {name: ds}
      spec:
        interface: wan
        tunnelName: qa-private-ds
        localAddressSource: static
        localAddress: '2001:db8::1'
        aftrIPv6: '2001:db8::2'
        mtu: 1460
`
			if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(root, "state.db")
			args := nativeOwnerArguments(root, configPath, statePath, once)
			args = append(args, "--controllers", "dslite", "--socket", filepath.Join(root, "control.sock"), "--status-socket", filepath.Join(root, "status.sock"), "--graceful-stop-timeout", "0")
			owner, ownerOutput := nativeOwnerChild(t, root, "serve", args, marker)
			if err := owner.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
			waitNativeOwnerMarker(t, marker)
			store, err := state.OpenSQLiteReadOnly(statePath)
			if err != nil {
				t.Fatal(err)
			}
			before, err := store.ReadObjectStatus("routerd.net/v1alpha1", "Router", "dslite-native")
			_ = store.Close()
			if err != nil {
				t.Fatal(err)
			}
			if before["tunnels"] == nil {
				t.Fatalf("missing pending native receipt: %v %s", before, ownerOutput.String())
			}
			for _, mode := range []string{"serve", "apply", "rollback"} {
				otherConfig := filepath.Join(root, "other.yaml")
				_ = os.WriteFile(otherConfig, []byte(strings.ReplaceAll(config, "2001:db8::2", "2001:db8::3")), 0600)
				otherState := filepath.Join(root, mode+"-different.db")
				childArgs := nativeOwnerArguments(root, otherConfig, otherState, true)
				if mode == "serve" {
					childArgs = append(childArgs, "--controllers", "dslite", "--socket", filepath.Join(root, "other.sock"), "--status-socket", filepath.Join(root, "other-status.sock"))
				}
				if mode == "rollback" {
					// Reading a saved generation is harmless; the common apply entry must
					// still refuse its native admission before changing that generation.
					childArgs = nativeOwnerArguments(root, otherConfig, statePath, false)
					childArgs = append(childArgs, "--to", "1")
				}
				if mode == "rollback" && !once {
					seed, seedErr := state.OpenSQLite(statePath)
					if seedErr != nil {
						t.Fatal(seedErr)
					}
					if _, _, seedErr = seed.BeginGenerationIfChanged("rollback-fixture", config); seedErr != nil {
						t.Fatal(seedErr)
					}
					_ = seed.Close()
				}
				child, output := nativeOwnerChild(t, root, mode, childArgs, filepath.Join(root, "forbidden-native"))
				if err := child.Run(); err == nil || !strings.Contains(output.String(), "another routerd process owns native operations") {
					t.Fatalf("%s was not fenced: %v %s", mode, err, output.String())
				}
				if _, err := os.Stat(filepath.Join(root, "forbidden-native")); !os.IsNotExist(err) {
					t.Fatal("second native writer ran")
				}
				if mode != "rollback" {
					if _, err := os.Stat(otherState); !os.IsNotExist(err) {
						t.Fatalf("%s mutated alternate state before admission", mode)
					}
				}
			}
			for _, observation := range []struct {
				mode string
				args []string
			}{
				{"validate", []string{"--config", configPath}},
				{"rollback", []string{"--list", "--state-file", statePath}},
			} {
				child, output := nativeOwnerChild(t, root, observation.mode, observation.args, "")
				if err := child.Run(); err != nil {
					t.Fatalf("read-only %s was blocked: %v %s", observation.mode, err, output.String())
				}
			}
			afterStore, err := state.OpenSQLiteReadOnly(statePath)
			if err != nil {
				t.Fatal(err)
			}
			after, err := afterStore.ReadObjectStatus("routerd.net/v1alpha1", "Router", "dslite-native")
			_ = afterStore.Close()
			if err != nil {
				t.Fatal(err)
			}
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(after)
			if !bytes.Equal(beforeJSON, afterJSON) {
				t.Fatalf("concurrent CLI overwrote pending owner: %s -> %s", beforeJSON, afterJSON)
			}
			_ = owner.Process.Signal(syscall.SIGKILL)
			_ = owner.Wait()
			// Actual OS exit releases the never-unlinked lock inode.
			previous := platformDefaults.RuntimeDir
			platformDefaults.RuntimeDir = filepath.Join(root, "runtime")
			next, err := acquireNativeOperationOwner(context.Background())
			platformDefaults.RuntimeDir = previous
			if err != nil {
				t.Fatal(err)
			}
			if err := next.finish(true); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeOwnerRetainedRawDescriptorSurvivesGCAndNormalAckReleases(t *testing.T) {
	for _, mode := range []string{"retained", "normal"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "finished")
			child, output := nativeOwnerChild(t, root, mode, nil, marker)
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
			waitNativeOwnerMarker(t, marker)
			old := platformDefaults.RuntimeDir
			platformDefaults.RuntimeDir = filepath.Join(root, "runtime")
			owner, err := acquireNativeOperationOwner(context.Background())
			platformDefaults.RuntimeDir = old
			if mode == "retained" {
				if err == nil {
					_ = owner.finish(true)
					t.Fatal("unacknowledged owner released by finalizer")
				}
			} else {
				if err != nil {
					t.Fatalf("normal owner retained lock: %v %s", err, output.String())
				}
				_ = owner.finish(true)
			}
		})
	}
}
