// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestUnsupportedExecutionIntentRejectedBeforeApply(t *testing.T) {
	for _, resource := range []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "ProviderActionPolicy"}, Metadata: api.ObjectMeta{Name: "provider"}, Spec: api.ProviderActionPolicySpec{ExecutionWindow: "0 2 * * *"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ObservabilityPipeline"}, Metadata: api.ObjectMeta{Name: "logs"}, Spec: api.ObservabilityPipelineSpec{Logs: api.ObservabilityPipelineLogsSpec{Sinks: []api.ObservabilityPipelineLogSink{{Type: "kafka", Kafka: api.ObservabilityKafkaSinkSpec{Brokers: []string{"broker:9092"}, Topic: "routerd"}}}}}},
	} {
		router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
		for _, target := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
			if err := ValidateForOS(router, target); err == nil || !strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("%s %s accepted inactive intent: %v", target, resource.Kind, err)
			}
		}
	}
}

func TestOTLPPlaintextCannotDiscardExplicitTLSIntent(t *testing.T) {
	for _, tls := range []api.OTELTLSSpec{{InsecureSkipVerify: true}, {CAFile: "ca.pem"}, {CertFile: "client.pem", KeyFile: "client.key"}} {
		router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ObservabilityPipeline"}, Metadata: api.ObjectMeta{Name: "telemetry"}, Spec: api.ObservabilityPipelineSpec{OTLP: api.ObservabilityPipelineOTLPSpec{Insecure: true, TLS: tls}}}}}}
		if err := ValidateForOS(router, platform.OSLinux); err == nil || !strings.Contains(err.Error(), "insecure cannot be combined with TLS") {
			t.Fatalf("plaintext silently discarded TLS intent %+v: %v", tls, err)
		}
	}
}

func TestOTLPDelimitedMapsCannotInjectAnotherEntry(t *testing.T) {
	for _, spec := range []api.ObservabilityPipelineSpec{
		{Attributes: map[string]string{"service.name,other": "value"}},
		{Attributes: map[string]string{"service.name=other": "value"}},
		{OTLP: api.ObservabilityPipelineOTLPSpec{Headers: map[string]string{"Authorization,other": "token"}}},
		{OTLP: api.ObservabilityPipelineOTLPSpec{Headers: map[string]string{"Authorization\r\nOther": "token"}}},
	} {
		router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ObservabilityPipeline"}, Metadata: api.ObjectMeta{Name: "telemetry"}, Spec: spec}}}}
		if err := ValidateForOS(router, platform.OSLinux); err == nil {
			t.Fatalf("delimiter-containing key admitted: %+v", spec)
		}
	}
}
