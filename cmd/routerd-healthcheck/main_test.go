package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
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

func TestRestoreStateIgnoresEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("\n"), 0644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	daemon := &daemon{opts: options{stateFile: path}}
	if err := daemon.restoreState(); err != nil {
		t.Fatalf("restore empty state: %v", err)
	}
}
