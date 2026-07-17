// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestParseConntrackExtendedLinePreservesMark(t *testing.T) {
	line := "ipv4     2 tcp      6 86400 ESTABLISHED src=172.18.1.73 dst=142.251.23.95 sport=52654 dport=443 src=142.251.23.95 dst=192.0.0.2 sport=443 dport=52654 [ASSURED] mark=272 use=1"
	entry, ok, err := parseConntrackExtendedLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("line was not parsed")
	}
	wantInsert := []string{"-I", "-t", "86400", "-u", "SEEN_REPLY,ASSURED", "-s", "172.18.1.73", "-d", "142.251.23.95", "-r", "142.251.23.95", "-q", "192.0.0.2", "-p", "tcp", "--sport", "52654", "--dport", "443", "--reply-port-src", "443", "--reply-port-dst", "52654", "--state", "ESTABLISHED", "-m", "272"}
	if !reflect.DeepEqual(entry.Insert, wantInsert) {
		t.Fatalf("insert = %#v, want %#v", entry.Insert, wantInsert)
	}
	if strings.Join(entry.Delete, " ") != "-D -s 172.18.1.73 -d 142.251.23.95 -r 142.251.23.95 -q 192.0.0.2 -p tcp --sport 52654 --dport 443 --reply-port-src 443 --reply-port-dst 52654" {
		t.Fatalf("delete = %#v", entry.Delete)
	}
}

func TestParseConntrackExtendedLineWithoutFamilyName(t *testing.T) {
	line := "     2 tcp      6 86398 ESTABLISHED src=172.18.1.150 dst=20.194.195.242 sport=65190 dport=443 packets=262 bytes=12258 src=20.194.195.242 dst=192.0.0.2 sport=443 dport=65190 packets=260 bytes=66429 [ASSURED] mark=272 use=1"
	entry, ok, err := parseConntrackExtendedLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("line was not parsed")
	}
	got := strings.Join(entry.Insert, " ")
	for _, want := range []string{"-s 172.18.1.150", "-q 192.0.0.2", "--state ESTABLISHED", "-m 272"} {
		if !strings.Contains(got, want) {
			t.Fatalf("insert = %q, missing %q", got, want)
		}
	}
}

func TestParseConntrackExtendedLineICMP(t *testing.T) {
	line := "ipv4     2 icmp     1 30 src=172.18.1.175 dst=8.8.8.8 type=8 code=0 id=56508 packets=2 bytes=168 src=8.8.8.8 dst=192.0.0.4 type=0 code=0 id=56508 packets=2 bytes=168 [ASSURED] mark=274 use=1"
	entry, ok, err := parseConntrackExtendedLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("line was not parsed")
	}
	got := strings.Join(entry.Insert, " ")
	for _, want := range []string{"-p icmp", "--icmp-type 8", "--icmp-code 0", "--icmp-id 56508", "-m 274"} {
		if !strings.Contains(got, want) {
			t.Fatalf("insert = %q, missing %q", got, want)
		}
	}
}

func TestParseConntrackExtendedLineSkipsSummary(t *testing.T) {
	_, ok, err := parseConntrackExtendedLine("conntrack v1.4.8 (conntrack-tools): 0 flow entries have been shown.")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("summary line should not produce a restore entry")
	}
}

func TestParseConntrackEventLine(t *testing.T) {
	line := "[NEW] ipv4 2 tcp 6 86398 ESTABLISHED src=172.18.1.150 dst=20.194.195.242 sport=65190 dport=443 packets=262 bytes=12258 src=20.194.195.242 dst=192.0.0.2 sport=443 dport=65190 packets=260 bytes=66429 [ASSURED] mark=272 use=1"
	op, ok, err := parseConntrackEventLine(line, []string{"192.0.0.2"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || op.DeleteOnly {
		t.Fatalf("operation = %#v, ok=%v", op, ok)
	}
	got := strings.Join(op.Entry.Insert, " ")
	for _, want := range []string{"-s 172.18.1.150", "-q 192.0.0.2", "--state ESTABLISHED", "-m 272"} {
		if !strings.Contains(got, want) {
			t.Fatalf("insert = %q, missing %q", got, want)
		}
	}
	if _, ok, err := parseConntrackEventLine(line, []string{"192.0.0.9"}); err != nil || ok {
		t.Fatalf("filtered operation ok=%v err=%v", ok, err)
	}
	destroy, ok, err := parseConntrackEventLine(strings.Replace(line, "[NEW]", "[DESTROY]", 1), []string{"192.0.0.2"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !destroy.DeleteOnly {
		t.Fatalf("destroy operation = %#v, ok=%v", destroy, ok)
	}
	ipv6Line := "[UPDATE] ipv6 10 icmpv6 58 29 src=2001:db8::1 dst=2001:db8::2 type=128 code=0 id=1 src=2001:db8::2 dst=2001:db8::1 type=129 code=0 id=1 mark=0 use=1"
	if _, ok, err := parseConntrackEventLine(ipv6Line, []string{"192.0.0.2"}); err != nil || ok {
		t.Fatalf("ipv6 operation ok=%v err=%v", ok, err)
	}
}

func TestParseNAT44SessionSyncRestoreOutput(t *testing.T) {
	result, err := parseNAT44SessionSyncRestoreOutput([]byte("noise\nok_del=1 miss_del=2 ng_del=3 ok_ins=4 dup_ins=5 ng_ins=6\n"))
	if err != nil {
		t.Fatal(err)
	}
	if result != (nat44SessionSyncRestoreResult{OKDel: 1, MissingDel: 2, NGDel: 3, OKIns: 4, DuplicateIns: 5, NGIns: 6}) {
		t.Fatalf("result = %#v", result)
	}
	if _, err := parseNAT44SessionSyncRestoreOutput([]byte("ok_del=1 miss_del=2 ng_del=3 ok_ins=4 dup_ins=5\n")); err == nil {
		t.Fatal("expected missing ng_ins to fail")
	}
	if phase, reason := nat44SessionSyncRestorePhase(2, nat44SessionSyncRestoreResult{OKIns: 0, NGIns: 2}); phase != "Error" || reason != "RestoreFailed" {
		t.Fatalf("all-failed phase = %s/%s", phase, reason)
	}
	if phase, reason := nat44SessionSyncRestorePhase(2, nat44SessionSyncRestoreResult{OKIns: 1, NGIns: 1}); phase != "Degraded" || reason != "RestorePartialFailed" {
		t.Fatalf("partial phase = %s/%s", phase, reason)
	}
	if phase, reason := nat44SessionSyncRestorePhase(2, nat44SessionSyncRestoreResult{MissingDel: 2, OKIns: 2}); phase != "Synced" || reason != "" {
		t.Fatalf("missing-delete phase = %s/%s", phase, reason)
	}
	if phase, reason := nat44SessionSyncRestorePhase(2, nat44SessionSyncRestoreResult{MissingDel: 2, DuplicateIns: 2}); phase != "Synced" || reason != "" {
		t.Fatalf("duplicate-insert phase = %s/%s", phase, reason)
	}
	if phase, reason := nat44SessionSyncRestorePhase(2, nat44SessionSyncRestoreResult{OKIns: 2, NGDel: 1}); phase != "Degraded" || reason != "RestorePartialFailed" {
		t.Fatalf("delete-failed phase = %s/%s", phase, reason)
	}
	deleteOnly := []conntrackRestoreOperation{{DeleteOnly: true}}
	if phase, reason := nat44SessionSyncRestoreOperationsPhase(deleteOnly, nat44SessionSyncRestoreResult{OKDel: 1}); phase != "Synced" || reason != "" {
		t.Fatalf("delete-only phase = %s/%s", phase, reason)
	}
	if phase, reason := nat44SessionSyncRestoreOperationsPhase(deleteOnly, nat44SessionSyncRestoreResult{NGDel: 1}); phase != "Degraded" || reason != "RestorePartialFailed" {
		t.Fatalf("delete-only failed phase = %s/%s", phase, reason)
	}
}

func TestNAT44SessionSyncRestoreScriptClassifiesIdempotentResults(t *testing.T) {
	dir := t.TempDir()
	fakeConntrack := filepath.Join(dir, "conntrack")
	if err := os.WriteFile(fakeConntrack, []byte(`#!/bin/sh
case "$1 $2" in
  "-D missing") echo "conntrack v1.4.8: 0 flow entries have been deleted.";;
  "-I existing") echo "conntrack v1.4.8: File exists" >&2; exit 1;;
  "-I conntrack-exists") echo "conntrack v1.4.8 (conntrack-tools): Operation failed: Such conntrack exists, try -U to update" >&2; exit 1;;
  "-D fail") echo "delete denied" >&2; exit 1;;
  "-I fail") echo "insert denied" >&2; exit 1;;
  *) echo "ok";;
esac
`), 0755); err != nil {
		t.Fatalf("write fake conntrack: %v", err)
	}
	script := nat44SessionSyncRestoreScript([]conntrackRestoreEntry{
		{Delete: []string{"-D", "missing"}, Insert: []string{"-I", "existing"}},
		{Delete: []string{"-D", "ok"}, Insert: []string{"-I", "conntrack-exists"}},
		{Delete: []string{"-D", "ok"}, Insert: []string{"-I", "ok"}},
		{Delete: []string{"-D", "fail"}, Insert: []string{"-I", "fail"}},
	}, []string{fakeConntrack})
	cmd := exec.Command("sh")
	cmd.Stdin = bytes.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore script failed: %v\n%s\nscript:\n%s", err, out, script)
	}
	result, err := parseNAT44SessionSyncRestoreOutput(out)
	if err != nil {
		t.Fatalf("parse restore output: %v\n%s", err, out)
	}
	want := nat44SessionSyncRestoreResult{OKDel: 2, MissingDel: 1, NGDel: 1, OKIns: 1, DuplicateIns: 2, NGIns: 1}
	if result != want {
		t.Fatalf("result = %#v, want %#v\n%s", result, want, out)
	}
	if !strings.Contains(string(out), "delete failed: delete denied") || !strings.Contains(string(out), "insert failed: insert denied") {
		t.Fatalf("restore output missing representative errors:\n%s", out)
	}
}

func TestNAT44SessionSyncRunsSnapshotOverSSH(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/NAT44Rule/lan-to-dslite-b": {"snatAddress": "192.0.0.3"},
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-dslite-a"}, Spec: api.NAT44RuleSpec{Type: "snat", SNATAddress: "192.0.0.2"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-dslite-b"}, Spec: api.NAT44RuleSpec{Type: "snat", SNATAddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/ds-lite-b-source", Field: "address"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-dslite-ra"}, Spec: api.NAT44RuleSpec{Type: "snat", SNATAddress: "192.0.0.5"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44SessionSync"}, Metadata: api.ObjectMeta{Name: "dslite-abc"}, Spec: api.NAT44SessionSyncSpec{
			NATRules:        []string{"lan-to-dslite-a", "NAT44Rule/lan-to-dslite-b", "lan-to-dslite-ra"},
			ExcludeNATRules: []string{"lan-to-dslite-ra"},
			Targets: []api.NAT44SessionSyncTargetSpec{{
				Host:           "homert03.lain.local",
				User:           "routerd",
				SSHOptions:     []string{"-o", "ConnectTimeout=3"},
				RestoreCommand: []string{"sudo", "conntrack"},
			}},
		}},
	}}}
	dumps := map[string]string{
		"192.0.0.2": "ipv4 2 tcp 6 86400 ESTABLISHED src=172.18.1.73 dst=142.251.23.95 sport=52654 dport=443 src=142.251.23.95 dst=192.0.0.2 sport=443 dport=52654 [ASSURED] mark=272 use=1\n",
		"192.0.0.3": "ipv4 2 udp 17 171 src=172.18.1.78 dst=35.72.114.176 sport=18535 dport=32100 src=35.72.114.176 dst=192.0.0.3 sport=32100 dport=18535 [ASSURED] mark=273 use=1\n",
	}
	var sshArgs []string
	var sshScript string
	controller := NAT44SessionSyncController{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return time.Date(2026, 6, 4, 23, 0, 0, 0, time.UTC) },
		Command: func(_ context.Context, name string, args []string, stdin []byte) ([]byte, error) {
			switch name {
			case "conntrack":
				if len(args) == 5 && args[0] == "--dump" && args[3] == "-n" {
					return []byte(dumps[args[4]]), nil
				}
				t.Fatalf("unexpected conntrack args: %#v", args)
			case "ssh":
				sshArgs = append([]string(nil), args...)
				sshScript = string(stdin)
				return []byte("ok_del=0 miss_del=2 ng_del=0 ok_ins=2 dup_ins=0 ng_ins=0\n"), nil
			default:
				t.Fatalf("unexpected command %q", name)
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sshArgs, []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=3", "routerd@homert03.lain.local", "sh", "-s"}) {
		t.Fatalf("ssh args = %#v", sshArgs)
	}
	for _, want := range []string{"'sudo' 'conntrack' '-I'", "'-m' '272'", "'-m' '273'", "'-D'"} {
		if !strings.Contains(sshScript, want) {
			t.Fatalf("restore script missing %q:\n%s", want, sshScript)
		}
	}
	status := store.ObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "dslite-abc")
	if status["phase"] != "Synced" || status["sessionCount"] != 2 || status["targetCount"] != 1 || status["deleteMissing"] != 2 || status["insertOK"] != 2 || status["insertFailed"] != 0 {
		t.Fatalf("status = %#v", status)
	}
	if !reflect.DeepEqual(status["snatAddresses"], []string{"192.0.0.2", "192.0.0.3"}) {
		t.Fatalf("snatAddresses = %#v", status["snatAddresses"])
	}
	targets, ok := status["targets"].([]map[string]any)
	if !ok || len(targets) != 1 || targets[0]["phase"] != "Synced" || targets[0]["deleteMissing"] != 2 || targets[0]["insertOK"] != 2 || targets[0]["insertFailed"] != 0 {
		t.Fatalf("targets = %#v", status["targets"])
	}
}

func TestNAT44SessionSyncEventStreamStartsWithSnapshotAndConsumesEvents(t *testing.T) {
	store := newSyncMapStore()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44SessionSync"}, Metadata: api.ObjectMeta{Name: "dslite-abc"}, Spec: api.NAT44SessionSyncSpec{
			Mode:          "event-stream",
			SNATAddresses: []string{"192.0.0.2"},
			Targets:       []api.NAT44SessionSyncTargetSpec{{Name: "standby", Host: "homert03.lain.local"}},
		}},
	}}}
	reader, writer := io.Pipe()
	defer writer.Close()
	var mu sync.Mutex
	var sshScripts []string
	controller := NAT44SessionSyncController{
		Router:  router,
		Store:   store,
		Workers: newNAT44SessionSyncWorkerManager(),
		Command: func(_ context.Context, name string, args []string, stdin []byte) ([]byte, error) {
			switch name {
			case "conntrack":
				return []byte("ipv4 2 tcp 6 86400 ESTABLISHED src=172.18.1.73 dst=142.251.23.95 sport=52654 dport=443 src=142.251.23.95 dst=192.0.0.2 sport=443 dport=52654 [ASSURED] mark=272 use=1\n"), nil
			case "ssh":
				script := string(stdin)
				mu.Lock()
				sshScripts = append(sshScripts, script)
				mu.Unlock()
				inserts := strings.Count(script, "'-I'")
				deletes := strings.Count(script, "'-D'")
				return []byte(fmt.Sprintf("ok_del=%d miss_del=0 ng_del=0 ok_ins=%d dup_ins=0 ng_ins=0\n", deletes, inserts)), nil
			default:
				return nil, fmt.Errorf("unexpected command %q", name)
			}
		},
		EventCommand: func(ctx context.Context, name string, args []string) (io.ReadCloser, func() error, error) {
			if name != "conntrack" || !reflect.DeepEqual(args, []string{"-E", "-o", "extended"}) {
				return nil, nil, fmt.Errorf("unexpected event command: %s %#v", name, args)
			}
			go func() {
				<-ctx.Done()
				writer.Close()
			}()
			return reader, func() error { return ctx.Err() }, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	waitForNAT44Status(t, store, controller, ctx, func(status map[string]any) bool {
		return status["phase"] == "Synced" && status["streamState"] == "running" && status["resyncCount"] == 1
	})
	savesAfterResync := store.SaveCount()
	event := "[NEW] ipv4 2 tcp 6 86398 ESTABLISHED src=172.18.1.150 dst=20.194.195.242 sport=65190 dport=443 packets=262 bytes=12258 src=20.194.195.242 dst=192.0.0.2 sport=443 dport=65190 packets=260 bytes=66429 [ASSURED] mark=272 use=1\n"
	for i := 0; i < defaultNAT44SessionSyncEventBatchMax; i++ {
		if _, err := io.WriteString(writer, event); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		scripts := len(sshScripts)
		mu.Unlock()
		if scripts >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sshScripts) < 2 {
		t.Fatalf("ssh scripts = %d", len(sshScripts))
	}
	if !strings.Contains(sshScripts[0], "'-I'") || !strings.Contains(sshScripts[1], "'-I'") {
		t.Fatalf("unexpected restore scripts:\n--- snapshot ---\n%s\n--- event ---\n%s", sshScripts[0], sshScripts[1])
	}
	if saves := store.SaveCount(); saves != savesAfterResync {
		t.Fatalf("event batch should not persist transient-only status: saves before=%d after=%d", savesAfterResync, saves)
	}
}

func TestNAT44SessionSyncEventStreamPersistsRunningStatusWithoutAnotherReconcile(t *testing.T) {
	store := newSyncMapStore()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44SessionSync"}, Metadata: api.ObjectMeta{Name: "dslite-abc"}, Spec: api.NAT44SessionSyncSpec{
			Mode:          "event-stream",
			SNATAddresses: []string{"192.0.0.2"},
			Targets:       []api.NAT44SessionSyncTargetSpec{{Name: "standby", Host: "homert03.lain.local"}},
		}},
	}}}
	reader, writer := io.Pipe()
	defer writer.Close()
	controller := NAT44SessionSyncController{
		Router:  router,
		Store:   store,
		Workers: newNAT44SessionSyncWorkerManager(),
		Command: func(_ context.Context, name string, _ []string, _ []byte) ([]byte, error) {
			switch name {
			case "conntrack":
				return []byte("ipv4 2 tcp 6 86400 ESTABLISHED src=172.18.1.73 dst=142.251.23.95 sport=52654 dport=443 src=142.251.23.95 dst=192.0.0.2 sport=443 dport=52654 [ASSURED] mark=272 use=1\n"), nil
			case "ssh":
				return []byte("ok_del=0 miss_del=1 ng_del=0 ok_ins=1 dup_ins=0 ng_ins=0\n"), nil
			default:
				return nil, fmt.Errorf("unexpected command %q", name)
			}
		},
		EventCommand: func(ctx context.Context, _ string, _ []string) (io.ReadCloser, func() error, error) {
			go func() {
				<-ctx.Done()
				writer.Close()
			}()
			return reader, func() error { return ctx.Err() }, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := store.ObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "dslite-abc")
		if status["phase"] == "Synced" && status["streamState"] == "running" && status["resyncCount"] == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("event stream status was not persisted: %#v", store.ObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "dslite-abc"))
}

func TestNAT44SessionSyncEventStreamDoesNotLetReconcileOverwriteWorkerStatus(t *testing.T) {
	store := newBlockedInitialStatusStore()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44SessionSync"}, Metadata: api.ObjectMeta{Name: "dslite-abc"}, Spec: api.NAT44SessionSyncSpec{
			Mode:          "event-stream",
			SNATAddresses: []string{"192.0.0.2"},
			Targets:       []api.NAT44SessionSyncTargetSpec{{Name: "standby", Host: "homert03.lain.local"}},
		}},
	}}}
	reader, writer := io.Pipe()
	defer writer.Close()
	controller := NAT44SessionSyncController{
		Router:  router,
		Store:   store,
		Workers: newNAT44SessionSyncWorkerManager(),
		Command: func(_ context.Context, name string, _ []string, _ []byte) ([]byte, error) {
			switch name {
			case "conntrack":
				return []byte("ipv4 2 tcp 6 86400 ESTABLISHED src=172.18.1.73 dst=142.251.23.95 sport=52654 dport=443 src=142.251.23.95 dst=192.0.0.2 sport=443 dport=52654 [ASSURED] mark=272 use=1\n"), nil
			case "ssh":
				return []byte("ok_del=0 miss_del=1 ng_del=0 ok_ins=1 dup_ins=0 ng_ins=0\n"), nil
			default:
				return nil, fmt.Errorf("unexpected command %q", name)
			}
		},
		EventCommand: func(ctx context.Context, _ string, _ []string) (io.ReadCloser, func() error, error) {
			go func() {
				<-ctx.Done()
				writer.Close()
			}()
			return reader, func() error { return ctx.Err() }, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reconciled := make(chan error, 1)
	go func() { reconciled <- controller.Reconcile(ctx) }()
	select {
	case err := <-reconciled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Reconcile blocked while saving the worker's initial status")
	}
	select {
	case <-store.initialSaveStarted:
	case <-time.After(time.Second):
		t.Fatal("worker did not save its initial status")
	}
	close(store.unblockInitialSave)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := store.ObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "dslite-abc")
		if status["phase"] == "Synced" && status["streamState"] == "running" && status["resyncCount"] == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("event stream status was overwritten: %#v", store.ObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "dslite-abc"))
}

func TestNAT44SessionSyncReportsRestoreInsertFailures(t *testing.T) {
	store := mapStore{}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44SessionSync"}, Metadata: api.ObjectMeta{Name: "dslite-abc"}, Spec: api.NAT44SessionSyncSpec{
			SNATAddresses: []string{"192.0.0.2"},
			Targets:       []api.NAT44SessionSyncTargetSpec{{Name: "standby", Host: "homert03.lain.local"}},
		}},
	}}}
	controller := NAT44SessionSyncController{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return time.Date(2026, 6, 4, 23, 10, 0, 0, time.UTC) },
		Command: func(_ context.Context, name string, args []string, stdin []byte) ([]byte, error) {
			switch name {
			case "conntrack":
				return []byte("ipv4 2 tcp 6 86400 ESTABLISHED src=172.18.1.73 dst=142.251.23.95 sport=52654 dport=443 src=142.251.23.95 dst=192.0.0.2 sport=443 dport=52654 [ASSURED] mark=272 use=1\n"), nil
			case "ssh":
				if !strings.Contains(string(stdin), "ok_ins") {
					t.Fatalf("restore script missing counters:\n%s", stdin)
				}
				return []byte("insert failed: conntrack v1.4.8: Operation failed\nok_del=0 miss_del=0 ng_del=1 ok_ins=0 dup_ins=0 ng_ins=1\n"), nil
			default:
				t.Fatalf("unexpected command %q", name)
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "dslite-abc")
	if status["phase"] != "Error" || status["reason"] != "RestoreFailed" || status["insertOK"] != 0 || status["insertFailed"] != 1 {
		t.Fatalf("status = %#v", status)
	}
	targets, ok := status["targets"].([]map[string]any)
	if !ok || len(targets) != 1 {
		t.Fatalf("targets = %#v", status["targets"])
	}
	if targets[0]["phase"] != "Error" || targets[0]["reason"] != "RestoreFailed" || targets[0]["insertOK"] != 0 || targets[0]["insertFailed"] != 1 {
		t.Fatalf("target status = %#v", targets[0])
	}
	if !strings.Contains(fmt.Sprint(targets[0]["output"]), "Operation failed") {
		t.Fatalf("target output = %#v", targets[0]["output"])
	}
}

type syncMapStore struct {
	mu    sync.Mutex
	m     map[string]map[string]any
	saves int
}

func newSyncMapStore() *syncMapStore {
	return &syncMapStore{m: map[string]map[string]any{}}
}

type blockedInitialStatusStore struct {
	*syncMapStore
	initialSaveStarted chan struct{}
	unblockInitialSave chan struct{}
	initialSaveOnce    sync.Once
}

func newBlockedInitialStatusStore() *blockedInitialStatusStore {
	return &blockedInitialStatusStore{
		syncMapStore:       newSyncMapStore(),
		initialSaveStarted: make(chan struct{}),
		unblockInitialSave: make(chan struct{}),
	}
}

func (s *blockedInitialStatusStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if status["phase"] == "Pending" && status["reason"] == "Starting" {
		s.initialSaveOnce.Do(func() {
			close(s.initialSaveStarted)
			<-s.unblockInitialSave
		})
	}
	return s.syncMapStore.SaveObjectStatus(apiVersion, kind, name, status)
}

func (s *syncMapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	s.m[apiVersion+"/"+kind+"/"+name] = cloneStatusMap(status)
	return nil
}

func (s *syncMapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status := s.m[apiVersion+"/"+kind+"/"+name]; status != nil {
		return cloneStatusMap(status)
	}
	return map[string]any{}
}

func (s *syncMapStore) SaveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves
}

func waitForNAT44Status(t *testing.T, store *syncMapStore, controller NAT44SessionSyncController, ctx context.Context, match func(map[string]any) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := controller.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		status := store.ObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "dslite-abc")
		if match(status) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "dslite-abc")
	t.Fatalf("timed out waiting for NAT44SessionSync status: %#v", status)
}

func TestNAT44SessionSyncEventStreamSkipsTransientOnlyStatusSave(t *testing.T) {
	current := map[string]any{
		"phase":            "Synced",
		"mode":             "event-stream",
		"streamState":      "running",
		"snatAddresses":    []string{"192.0.2.10"},
		"snatAddressCount": 1,
		"targetCount":      1,
		"dryRun":           false,
	}
	next := cloneStatusMap(current)
	next["lastEventAt"] = "2026-07-07T14:25:36Z"
	next["queuedEventCount"] = 7
	next["lastBatchAt"] = "2026-07-07T14:25:40Z"
	next["lastBatchEvents"] = 7
	next["insertOK"] = 7
	next["targets"] = []map[string]any{{"host": "standby", "phase": "Synced", "insertOK": 7}}

	if nat44SessionSyncPersistentStatusChanged(current, next) {
		t.Fatalf("transient event-stream status should not require a DB save: %#v", statusChangedFieldsForEvent(api.NetAPIVersion, "NAT44SessionSync", current, next))
	}

	next["phase"] = "Degraded"
	next["reason"] = "RestoreFailed"
	if !nat44SessionSyncPersistentStatusChanged(current, next) {
		t.Fatal("phase/reason change should still require a DB save")
	}
}

func TestNAT44SessionSyncPendingWhenRuleSNATUnresolved(t *testing.T) {
	store := mapStore{}
	controller := NAT44SessionSyncController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-dslite-b"}, Spec: api.NAT44RuleSpec{Type: "snat", SNATAddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/ds-lite-b-source", Field: "address"}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44SessionSync"}, Metadata: api.ObjectMeta{Name: "dslite-abc"}, Spec: api.NAT44SessionSyncSpec{
				NATRules: []string{"lan-to-dslite-b"},
				Targets:  []api.NAT44SessionSyncTargetSpec{{Host: "homert03.lain.local"}},
			}},
		}}},
		Store: store,
		Command: func(context.Context, string, []string, []byte) ([]byte, error) {
			t.Fatal("command should not run while SNAT address is pending")
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "dslite-abc")
	if status["phase"] != "Pending" || status["reason"] != "SNATAddressPending" || status["pending"] != "lan-to-dslite-b" {
		t.Fatalf("status = %#v", status)
	}
}
