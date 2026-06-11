// SPDX-License-Identifier: BSD-3-Clause

package main

import (
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

func TestBareIP(t *testing.T) {
	if got := bareIP("10.77.60.13/32"); got != "10.77.60.13" {
		t.Fatalf("bareIP = %q", got)
	}
}
