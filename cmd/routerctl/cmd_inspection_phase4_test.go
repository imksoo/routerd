// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
)

func startInspectionTestServer(t *testing.T, handler controlapi.Handler) string {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "routerd.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	server := &http.Server{Handler: handler}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	return socketPath
}

func TestGetAndDescribeUseInspectionControlAPI(t *testing.T) {
	var gotSubject string
	var gotDescribeTarget string
	socketPath := startInspectionTestServer(t, controlapi.Handler{
		Get: func(r *http.Request, req controlapi.GetRequest) (*controlapi.GetResult, error) {
			gotSubject = req.Subject
			result := controlapi.NewGetResult(req.Subject)
			result.Items = []controlapi.ResourceView{{
				APIVersion: api.NetAPIVersion,
				Kind:       "Interface",
				Name:       "wan",
				Spec:       map[string]any{"ifname": "ens18"},
				Status:     map[string]any{"phase": "Ready"},
			}}
			return &result, nil
		},
		Describe: func(r *http.Request, req controlapi.DescribeRequest) (*controlapi.DescribeResult, error) {
			gotDescribeTarget = req.Target
			result := controlapi.NewDescribeResult(req.Target, controlapi.ResourceView{
				APIVersion: api.NetAPIVersion,
				Kind:       "Interface",
				Name:       "wan",
				Spec:       map[string]any{"ifname": "ens18"},
				Status:     map[string]any{"phase": "Ready"},
			})
			return &result, nil
		},
	})

	var out bytes.Buffer
	if err := run([]string{"get", "if/wan", "--socket", socketPath, "-o", "table"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("get if/wan: %v", err)
	}
	if gotSubject != "Interface/wan" {
		t.Fatalf("get subject = %q, want Interface/wan", gotSubject)
	}
	for _, want := range []string{"KIND", "Interface", "wan", "ifname=ens18"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("get output missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := run([]string{"describe", "if/wan", "-o", "json", "--socket", socketPath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("describe if/wan: %v", err)
	}
	if gotDescribeTarget != "Interface/wan" {
		t.Fatalf("describe target = %q, want Interface/wan", gotDescribeTarget)
	}
	var decoded controlapi.DescribeResult
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("decode describe: %v\n%s", err, out.String())
	}
	if decoded.Resource.Kind != "Interface" || decoded.Resource.Name != "wan" {
		t.Fatalf("describe resource = %#v", decoded.Resource)
	}
}

func TestGetRuntimeSubjectsAndDoctorProbeUseControlAPI(t *testing.T) {
	var gotEventsLimit int
	var gotProbe controlapi.ProbeRequest
	socketPath := startInspectionTestServer(t, controlapi.Handler{
		Get: func(r *http.Request, req controlapi.GetRequest) (*controlapi.GetResult, error) {
			gotEventsLimit = req.Limit
			result := controlapi.NewGetResult(req.Subject)
			switch req.Subject {
			case "events":
				result.Events = nil
			case "ledger":
				result.Ledger = &controlapi.LedgerReport{Integrity: "ok"}
			default:
				t.Fatalf("unexpected get subject %q", req.Subject)
			}
			return &result, nil
		},
		Probe: func(r *http.Request, req controlapi.ProbeRequest) (*controlapi.ProbeResult, error) {
			gotProbe = req
			result := controlapi.NewProbeResult(req.Subject, req.Target, []controlapi.ProbeCheck{{
				Name:   "EgressRoutePolicy/ipv4-default",
				Status: "pass",
				Detail: "Ready",
			}})
			return &result, nil
		},
	})

	var out bytes.Buffer
	if err := run([]string{"get", "events", "--socket", socketPath, "--limit", "7", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("get events: %v", err)
	}
	if gotEventsLimit != 7 {
		t.Fatalf("events limit = %d, want 7", gotEventsLimit)
	}
	out.Reset()
	if err := run([]string{"get", "ledger", "--socket", socketPath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if !strings.Contains(out.String(), "INTEGRITY") || !strings.Contains(out.String(), "ok") {
		t.Fatalf("ledger output = %s", out.String())
	}

	out.Reset()
	if err := run([]string{"doctor", "--probe", "egress", "ipv4-default", "--socket", socketPath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor --probe: %v", err)
	}
	if gotProbe.Subject != "egress" || gotProbe.Target != "ipv4-default" {
		t.Fatalf("probe request = %#v", gotProbe)
	}
	if !strings.Contains(out.String(), "EgressRoutePolicy/ipv4-default") || !strings.Contains(out.String(), "pass") {
		t.Fatalf("probe output = %s", out.String())
	}
}
