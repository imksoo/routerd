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

func TestTelemetrySkipVerifyKeepsTLSTransport(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.ObservabilityAPIVersion, Kind: "ObservabilityPipeline"}, Metadata: api.ObjectMeta{Name: "pipeline"},
		Spec: api.ObservabilityPipelineSpec{OTLP: api.ObservabilityPipelineOTLPSpec{Endpoint: "https://collector.lan:4317", TLS: api.OTELTLSSpec{InsecureSkipVerify: true, CAFile: "/ca.pem", CertFile: "/client.pem", KeyFile: "/client.key"}, Headers: map[string]string{"authorization": "Bearer a,b%+c"}}, Attributes: map[string]string{"site": "edge, a+b%20"}},
	}}}}
	env, err := TelemetryEnvironment(router)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(env, "\n")
	if !strings.Contains(got, "ROUTERD_OTLP_TLS_SKIP_VERIFY=true") || strings.Contains(got, "OTEL_EXPORTER_OTLP_INSECURE=true") {
		t.Fatalf("certificate verification policy changed transport encryption:\n%s", got)
	}
	for _, want := range []string{"OTEL_EXPORTER_OTLP_CERTIFICATE=/ca.pem", "OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE=/client.pem", "OTEL_EXPORTER_OTLP_CLIENT_KEY=/client.key", "OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer%20a%2Cb%25+c", "OTEL_RESOURCE_ATTRIBUTES=site=edge%2C%20a+b%2520"} {
		if !strings.Contains(got, want) {
			t.Fatalf("TLS or value serialization lost concrete configuration %q:\n%s", want, got)
		}
	}
}
