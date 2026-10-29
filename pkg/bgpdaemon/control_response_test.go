// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func controlReply(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
}

func TestPathListRejectsUnusableFactsInsteadOfReportingAbsence(t *testing.T) {
	source := "MobilityPool/edge/node/a"
	for _, body := range []string{"null", "[] {}", `[{"source":"","prefix":"10.77.60.11/32","uuid":"01"}]`, `[{"source":"MobilityPool/other/node/a","prefix":"10.77.60.11/32","uuid":"01"}]`, `[{"source":"MobilityPool/edge/node/a","prefix":"10.77.60.11/32"}]`} {
		if paths, err := decodePathListResponse(controlReply(body), source); err == nil {
			t.Fatalf("unusable reply became path facts: %s -> %+v", body, paths)
		}
	}
	if paths, err := decodePathListResponse(controlReply("[]"), source); err != nil || paths == nil || len(paths) != 0 {
		t.Fatalf("explicit observed empty list=%+v, %v", paths, err)
	}
}

func TestWithdrawalAcknowledgmentRequiresCompleteDurableAbsence(t *testing.T) {
	path := AppliedPath{Source: "MobilityPool/edge/node/a", Prefix: "10.77.60.11/32"}
	for _, body := range []string{"", "null", "{}"} {
		if err := decodePathWithdrawalResponse(controlReply(body), path); err == nil {
			t.Fatalf("incomplete reply authorized withdrawal: %q", body)
		}
	}
	config := Normalize(AppliedConfig{Global: AppliedGlobal{ASN: 64512, RouterID: "192.0.2.1"}, Policy: PolicyIntent{RouterName: "edge"}})
	for _, pending := range []bool{false, true} {
		candidate := config
		if pending {
			candidate.PendingPathRemovals = []AppliedPath{path}
		} else {
			owned := path
			owned.UUID = "01"
			candidate.Paths = []AppliedPath{owned}
		}
		body, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if err := decodePathWithdrawalResponse(controlReply(string(body)), path); err == nil {
			t.Fatalf("remaining native ownership authorized withdrawal: %s", body)
		}
	}
	body, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := decodePathWithdrawalResponse(controlReply(string(body)), path); err != nil {
		t.Fatalf("completed withdrawal=%v", err)
	}
}

func TestUpsertAcknowledgmentRequiresItsExactNativeEffect(t *testing.T) {
	path := AppliedPath{Source: "MobilityPool/edge/node/a", Prefix: "10.77.60.11/32", Attrs: AppliedPathAttrs{LocalPref: 200}}
	for _, candidate := range []AppliedPath{path, {Source: path.Source, Prefix: "10.77.60.12/32", UUID: "01"}, {Source: path.Source, Prefix: path.Prefix, UUID: "01"}} {
		body, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodePathUpsertResponse(controlReply(string(body)), path); err == nil {
			t.Fatalf("unmatched effect acknowledged: %s", body)
		}
	}
	path.UUID = "01"
	body, err := json.Marshal(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodePathUpsertResponse(controlReply(string(body)), path); err != nil {
		t.Fatal(err)
	}
}
