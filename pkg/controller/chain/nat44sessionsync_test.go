// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"reflect"
	"strings"
	"testing"

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

func TestParseConntrackExtendedLinesSkipsSSHNoise(t *testing.T) {
	lines := []byte("Could not create directory '/root/.ssh' (Read-only file system).\r\nipv4 2 unknown 112 599 src=172.18.0.2 dst=172.18.0.3 packets=1 bytes=40 [UNREPLIED] src=172.18.0.3 dst=172.18.0.2 packets=0 bytes=0 mark=0 use=1\nipv4 2 tcp 6 86400 ESTABLISHED src=172.18.1.73 dst=142.251.23.95 sport=52654 dport=443 src=142.251.23.95 dst=192.0.0.2 sport=443 dport=52654 [ASSURED] mark=272 use=1\n")
	entries, err := parseConntrackExtendedLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
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
	stateLessTCP := strings.Replace(line, " ESTABLISHED", "", 1)
	if _, ok, err := parseConntrackEventLine(strings.Replace(stateLessTCP, "[NEW]", "[UPDATE]", 1), []string{"192.0.0.2"}); err != nil || ok {
		t.Fatalf("state-less TCP update operation ok=%v err=%v", ok, err)
	}
	stateLessDestroy, ok, err := parseConntrackEventLine(strings.Replace(stateLessTCP, "[NEW]", "[DESTROY]", 1), []string{"192.0.0.2"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !stateLessDestroy.DeleteOnly || strings.Contains(strings.Join(stateLessDestroy.Entry.Insert, " "), "--state") {
		t.Fatalf("state-less destroy operation = %#v, ok=%v", stateLessDestroy, ok)
	}
	ipv6Line := "[UPDATE] ipv6 10 icmpv6 58 29 src=2001:db8::1 dst=2001:db8::2 type=128 code=0 id=1 src=2001:db8::2 dst=2001:db8::1 type=129 code=0 id=1 mark=0 use=1"
	if _, ok, err := parseConntrackEventLine(ipv6Line, []string{"192.0.0.2"}); err != nil || ok {
		t.Fatalf("ipv6 operation ok=%v err=%v", ok, err)
	}
}

func TestParseNAT44SessionSyncRestoreOutput(t *testing.T) {
	result, err := parseNAT44SessionSyncRestoreOutput([]byte("noise\nok_del=1 miss_del=2 ng_del=3 ok_prune=4 miss_prune=5 ng_prune=6 ok_ins=7 dup_ins=8 ng_ins=9\n"))
	if err != nil {
		t.Fatal(err)
	}
	if result != (nat44SessionSyncRestoreResult{OKDel: 1, MissingDel: 2, NGDel: 3, OKPrune: 4, MissingPrune: 5, NGPrune: 6, OKIns: 7, DuplicateIns: 8, NGIns: 9}) {
		t.Fatalf("result = %#v", result)
	}
	if _, err := parseNAT44SessionSyncRestoreOutput([]byte("ok_del=1 miss_del=2 ng_del=3 ok_prune=4 miss_prune=5 ng_prune=6 ok_ins=7 dup_ins=8\n")); err == nil {
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
	if phase, reason := nat44SessionSyncRestorePhase(2, nat44SessionSyncRestoreResult{OKIns: 2, NGPrune: 1}); phase != "Degraded" || reason != "RestorePartialFailed" {
		t.Fatalf("prune-failed phase = %s/%s", phase, reason)
	}
	deleteOnly := []conntrackRestoreOperation{{DeleteOnly: true}}
	if phase, reason := nat44SessionSyncRestoreOperationsPhase(deleteOnly, nat44SessionSyncRestoreResult{OKDel: 1}); phase != "Synced" || reason != "" {
		t.Fatalf("delete-only phase = %s/%s", phase, reason)
	}
	if phase, reason := nat44SessionSyncRestoreOperationsPhase(deleteOnly, nat44SessionSyncRestoreResult{NGDel: 1}); phase != "Degraded" || reason != "RestorePartialFailed" {
		t.Fatalf("delete-only failed phase = %s/%s", phase, reason)
	}
	if phase, reason := nat44SessionSyncRestoreOperationsPhase(deleteOnly, nat44SessionSyncRestoreResult{NGPrune: 1}); phase != "Degraded" || reason != "RestorePartialFailed" {
		t.Fatalf("prune-only failed phase = %s/%s", phase, reason)
	}
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
