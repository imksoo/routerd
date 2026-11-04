// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
)

func TestNativeApplyWithdrawalKeepsUnixAPIs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "router.json")
	t.Setenv("ROUTERD_TEST_WITHDRAW_TOKEN", "local-test-token")
	controlPort, webPort := unusedHTTPPort(t), unusedHTTPPort(t)
	initial := httpConfigRouter(controlPort, webPort, "ROUTERD_TEST_WITHDRAW_TOKEN", "withdraw", true)
	// The selected controllers perform no host networking or service mutations.
	// The DNS declaration forces a generation change when withdrawing to empty.
	dns, err := config.LoadBytes([]byte(dnsRuntimeYAML("127.0.0.1")), "withdrawal")
	if err != nil {
		t.Fatal(err)
	}
	initial.Spec.Resources = append(initial.Spec.Resources, dns.Spec.Resources...)
	initial.Spec.Resources = append(initial.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogRetention"}, Metadata: api.ObjectMeta{Name: "logs"}, Spec: api.LogRetentionSpec{Retention: "7d", Signals: []string{"events"}}})
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	args := nativeOwnerArguments(root, configPath, filepath.Join(root, "state.db"), false)
	socket, statusSocket := filepath.Join(root, "runtime", "control.sock"), filepath.Join(root, "runtime", "status.sock")
	args = append(args, "--socket", socket, "--status-socket", statusSocket, "--controllers", "log-retention,control-http", "--graceful-stop-timeout", "0")
	child, output := nativeOwnerChild(t, root, "serve", args, "")
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	defer func() {
		_ = child.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve exit: %v\n%s", err, output.String())
			}
		case <-time.After(10 * time.Second):
			_ = child.Process.Kill()
			<-done
			t.Errorf("serve did not stop\n%s", output.String())
		}
	}()
	waitNativeOwnerMarker(t, statusSocket)
	for _, port := range []int{controlPort, webPort} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if err != nil {
			t.Fatalf("declared HTTP listener not running: %v", err)
		}
		conn.Close()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	empty := `apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: empty
spec:
  resources: []
`
	result, err := controlapi.NewUnixClient(socket).Apply(ctx, controlapi.ApplyRequest{CandidateYAML: empty, Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result.Phase != "Healthy" {
		t.Fatalf("native apply phase=%s; not the sandbox commit-only path", result.Result.Phase)
	}
	for _, port := range []int{controlPort, webPort} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if err == nil {
			conn.Close()
			t.Fatalf("withdrawn HTTP listener still accepts on %d", port)
		}
	}
	for _, path := range []string{socket, statusSocket} {
		status, err := controlapi.NewUnixClient(path).Status(ctx)
		if err != nil {
			t.Fatalf("Unix API unavailable after native withdrawal: %s: %v", path, err)
		}
		if status.Status.Phase != "Healthy" {
			t.Fatalf("withdrawn policy still affects native health on %s: %+v", path, status.Status)
		}
	}
}
