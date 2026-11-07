// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestFreeBSDRouterdReceivesTelemetryEnvironment(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.ObservabilityAPIVersion, Kind: "Telemetry"}, Metadata: api.ObjectMeta{Name: "otlp"}, Spec: api.TelemetrySpec{OTLP: api.TelemetryOTLPSpec{Endpoint: "https://collector.example:4317"}}}}}}
	config, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config.RCDScripts["routerd"]), "OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example:4317") {
		t.Fatalf("native routerd service lost configured telemetry:\n%s", config.RCDScripts["routerd"])
	}
}
