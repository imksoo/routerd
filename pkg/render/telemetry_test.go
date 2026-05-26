// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestTelemetryEnvironment(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.ObservabilityAPIVersion, Kind: "Telemetry"},
		Metadata: api.ObjectMeta{Name: "otlp"},
		Spec: api.TelemetrySpec{
			OTLP:             api.TelemetryOTLPSpec{Endpoint: "http://collector.lan:4317", Insecure: true},
			ServiceNamespace: "routerd",
			Attributes:       map[string]string{"site": "lab"},
			Signals:          []string{"logs", "metrics"},
		},
	}}}}
	env, err := TelemetryEnvironment(router)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(env, "\n")
	for _, want := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://collector.lan:4317",
		"OTEL_EXPORTER_OTLP_INSECURE=true",
		"OTEL_SERVICE_NAMESPACE=routerd",
		"OTEL_RESOURCE_ATTRIBUTES=site=lab",
		"OTEL_TRACES_EXPORTER=none",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("env missing %q:\n%s", want, got)
		}
	}
}
