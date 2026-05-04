package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelftestCreatesDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	var out bytes.Buffer
	if err := run([]string{"selftest", "--path", path}, &out, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"ok":true`) && !strings.Contains(out.String(), `"ok": true`) {
		t.Fatalf("output = %s", out.String())
	}
}

func TestDaemonReadsKeyValueLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	input := strings.NewReader(`action=drop src=172.18.0.10 dst=198.51.100.10 proto=tcp rule_name=test
`)
	if err := run([]string{"daemon", "--path", path}, &bytes.Buffer{}, input); err != nil {
		t.Fatal(err)
	}
}
