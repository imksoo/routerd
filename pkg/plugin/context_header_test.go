// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestPluginContextRemovesCredentialHeadersAtWireBoundary(t *testing.T) {
	resource := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.ObservabilityAPIVersion, Kind: "ObservabilityPipeline"},
		Metadata: api.ObjectMeta{Name: "telemetry"},
		Spec:     api.ObservabilityPipelineSpec{OTLP: api.ObservabilityPipelineOTLPSpec{Endpoint: "https://collector.example", Headers: map[string]string{"Authorization": "Bearer PRIVATE-AUTH", "Cookie": "session=PRIVATE-COOKIE", "X-Router": "edge"}}},
	}
	result, err := BuildPluginContext([]api.PluginContextResourceRef{{APIVersion: resource.APIVersion, Kind: resource.Kind, Name: resource.Metadata.Name}}, []api.Resource{resource})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRIVATE-") || !strings.Contains(string(raw), "edge") {
		t.Fatalf("credential header leak or public header lost: %s", raw)
	}
	if resource.Spec.(api.ObservabilityPipelineSpec).OTLP.Headers["Authorization"] != "Bearer PRIVATE-AUTH" {
		t.Fatal("source configuration mutated")
	}
}
