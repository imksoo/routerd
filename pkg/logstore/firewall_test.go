package logstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFirewallLogRecordAndList(t *testing.T) {
	log, err := OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	entry := FirewallLogEntry{
		Timestamp:  time.Now().UTC(),
		Action:     "drop",
		SrcAddress: "172.18.0.10",
		DstAddress: "198.51.100.1",
		Protocol:   "tcp",
		L3Proto:    "ipv4",
		RuleName:   "deny-test",
	}
	if err := log.Record(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	rows, err := log.List(context.Background(), FirewallLogFilter{Action: "drop", Src: "172.18.0.10", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RuleName != "deny-test" {
		t.Fatalf("rows = %#v", rows)
	}
}
