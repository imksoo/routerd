// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"reflect"
	"strings"
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
				return []byte("ok_del=0 ng_del=2 ok_ins=2 ng_ins=0\n"), nil
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
	if status["phase"] != "Synced" || status["sessionCount"] != 2 || status["targetCount"] != 1 {
		t.Fatalf("status = %#v", status)
	}
	if !reflect.DeepEqual(status["snatAddresses"], []string{"192.0.0.2", "192.0.0.3"}) {
		t.Fatalf("snatAddresses = %#v", status["snatAddresses"])
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
