package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSelftest(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"selftest", "--resource", "cloudflare", "--upstream", "https://1.1.1.1/dns-query"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"command":"cloudflared"`) || !strings.Contains(out.String(), "https://1.1.1.1/dns-query") {
		t.Fatalf("unexpected selftest output: %s", out.String())
	}
}
