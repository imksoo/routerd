// SPDX-License-Identifier: BSD-3-Clause

package observabilitypipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
)

func TestExporterPushesLokiEvent(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("X-Scope-OrgID") != "tenant-a" {
			t.Fatalf("tenant header = %q", r.Header.Get("X-Scope-OrgID"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	exporter := &Exporter{name: "remote", sampleRate: 1, httpClient: server.Client(), attrs: map[string]string{"site": "lab"}}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "routerd.test", daemonapi.SeverityInfo)
	event.Message = "hello"
	err := exporter.exportLoki(context.Background(), api.ObservabilityPipelineLogSink{
		Type: "loki",
		Loki: api.ObservabilityLokiSinkSpec{URL: server.URL + "/loki/api/v1/push", Tenant: "tenant-a"},
	}, event)
	if err != nil {
		t.Fatalf("export loki: %v", err)
	}
	streams, ok := got["streams"].([]any)
	if !ok || len(streams) != 1 {
		t.Fatalf("payload streams = %#v", got["streams"])
	}
	stream := streams[0].(map[string]any)["stream"].(map[string]any)
	if stream["site"] != "lab" || stream["topic"] != "routerd_test" {
		t.Fatalf("stream labels = %#v", stream)
	}
}
