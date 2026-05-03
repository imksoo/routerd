package main

import (
	"bytes"
	"encoding/json"
	"net"
	"testing"
)

func TestSelftestTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()
	host, portValue, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	dir := t.TempDir()
	if err := run([]string{"selftest", "--target", host, "--port", portValue, "--protocol", "tcp", "--state-file", dir + "/state.json", "--event-file", dir + "/events.jsonl"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		State struct {
			Phase string `json:"phase"`
		} `json:"state"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.State.Phase != "Healthy" {
		t.Fatalf("phase = %q, output:\n%s", decoded.State.Phase, stdout.String())
	}
}
