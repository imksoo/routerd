package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelftest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolver.json")
	if err := os.WriteFile(path, []byte(`{"resource":"lab","spec":{"listen":[{"addresses":["127.0.0.1"],"port":5053}],"sources":[{"name":"default","kind":"upstream","match":["."],"upstreams":["https://1.1.1.1/dns-query"]}]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := run([]string{"selftest", "--resource", "lab", "--config-file", path}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"resource":"lab"`) || !strings.Contains(out.String(), "https://1.1.1.1/dns-query") {
		t.Fatalf("unexpected selftest output: %s", out.String())
	}
}
