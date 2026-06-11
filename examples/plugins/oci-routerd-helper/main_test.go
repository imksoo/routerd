// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseArgsKeepsOCICommandAndConsumesGlobals(t *testing.T) {
	req, err := parseArgs([]string{
		"--config-file", "/ignored",
		"--profile", "DEFAULT",
		"--region", "ap-tokyo-1",
		"--auth", "instance_principal",
		"network", "private-ip", "list",
		"--subnet-id", "subnet-1",
		"--all",
		"--output", "json",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if req.Region != "ap-tokyo-1" {
		t.Fatalf("region = %q", req.Region)
	}
	if !reflect.DeepEqual(req.Words, []string{"network", "private-ip", "list"}) {
		t.Fatalf("words = %#v", req.Words)
	}
	if req.Flags["subnet-id"] != "subnet-1" || req.Flags["all"] != "true" {
		t.Fatalf("flags = %#v", req.Flags)
	}
}

func TestParseArgsRejectsNonInstancePrincipalAuth(t *testing.T) {
	if _, err := parseArgs([]string{"--auth", "security_token", "network", "vnic", "get", "--vnic-id", "v"}); err == nil {
		t.Fatal("expected non-instance-principal auth to be rejected")
	}
}

func TestVersionCommandOutputShape(t *testing.T) {
	var out bytes.Buffer
	old := stdout
	stdout = &out
	t.Cleanup(func() { stdout = old })

	if err := run(t.Context(), []string{"version"}); err != nil {
		t.Fatalf("run version: %v", err)
	}
	var body struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("decode version output: %v\n%s", err, out.String())
	}
	if body.Data["version"] != helperVersion {
		t.Fatalf("version = %q, want %q", body.Data["version"], helperVersion)
	}
}

func TestBareIP(t *testing.T) {
	if got := bareIP("10.77.60.13/32"); got != "10.77.60.13" {
		t.Fatalf("bareIP = %q", got)
	}
}
