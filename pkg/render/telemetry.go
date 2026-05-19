// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"sort"
	"strings"

	"routerd/pkg/api"
)

func TelemetryEnvironment(router *api.Router) ([]string, error) {
	if router == nil {
		return nil, nil
	}
	var selected *api.TelemetrySpec
	var pipeline *api.ObservabilityPipelineSpec
	for _, res := range router.Spec.Resources {
		if res.Kind == "ObservabilityPipeline" {
			spec, err := res.ObservabilityPipelineSpec()
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(spec.OTLP.Endpoint) == "" {
				continue
			}
			if pipeline != nil || selected != nil {
				return nil, fmt.Errorf("multiple telemetry pipeline resources are not supported")
			}
			pipeline = &spec
			continue
		}
		if res.Kind != "Telemetry" {
			continue
		}
		spec, err := res.TelemetrySpec()
		if err != nil {
			return nil, err
		}
		if selected != nil || pipeline != nil {
			return nil, fmt.Errorf("multiple telemetry pipeline resources are not supported")
		}
		selected = &spec
	}
	if pipeline != nil {
		return observabilityPipelineEnvironment(*pipeline), nil
	}
	if selected == nil {
		return nil, nil
	}
	var env []string
	env = append(env, "OTEL_EXPORTER_OTLP_ENDPOINT="+selected.OTLP.Endpoint)
	if selected.OTLP.Insecure {
		env = append(env, "OTEL_EXPORTER_OTLP_INSECURE=true")
	}
	namespace := strings.TrimSpace(selected.ServiceNamespace)
	if namespace == "" {
		namespace = "routerd"
	}
	env = append(env, "OTEL_SERVICE_NAMESPACE="+namespace)
	if len(selected.Attributes) > 0 {
		var attrs []string
		for _, key := range sortedMapKeysString(selected.Attributes) {
			attrs = append(attrs, key+"="+selected.Attributes[key])
		}
		env = append(env, "OTEL_RESOURCE_ATTRIBUTES="+strings.Join(attrs, ","))
	}
	signals := telemetrySignals(selected.Signals)
	for _, signal := range []string{"logs", "metrics", "traces"} {
		if !signals[signal] {
			env = append(env, telemetrySignalExporterEnv(signal)+"=none")
		}
	}
	sort.Strings(env)
	return env, nil
}

func observabilityPipelineEnvironment(spec api.ObservabilityPipelineSpec) []string {
	var env []string
	env = append(env, "OTEL_EXPORTER_OTLP_ENDPOINT="+strings.TrimSpace(spec.OTLP.Endpoint))
	if spec.OTLP.Insecure || spec.OTLP.TLS.InsecureSkipVerify {
		env = append(env, "OTEL_EXPORTER_OTLP_INSECURE=true")
	}
	if len(spec.OTLP.Headers) > 0 {
		var headers []string
		for _, key := range sortedMapKeysString(spec.OTLP.Headers) {
			headers = append(headers, key+"="+spec.OTLP.Headers[key])
		}
		env = append(env, "OTEL_EXPORTER_OTLP_HEADERS="+strings.Join(headers, ","))
	}
	namespace := strings.TrimSpace(spec.ServiceNamespace)
	if namespace == "" {
		namespace = "routerd"
	}
	env = append(env, "OTEL_SERVICE_NAMESPACE="+namespace)
	if len(spec.Attributes) > 0 {
		var attrs []string
		for _, key := range sortedMapKeysString(spec.Attributes) {
			attrs = append(attrs, key+"="+spec.Attributes[key])
		}
		env = append(env, "OTEL_RESOURCE_ATTRIBUTES="+strings.Join(attrs, ","))
	}
	signals := telemetrySignals(spec.Signals)
	for _, signal := range []string{"logs", "metrics", "traces"} {
		if !signals[signal] {
			env = append(env, telemetrySignalExporterEnv(signal)+"=none")
		}
	}
	sort.Strings(env)
	return env
}

func telemetrySignals(values []string) map[string]bool {
	if len(values) == 0 {
		return map[string]bool{"logs": true, "metrics": true, "traces": true}
	}
	out := map[string]bool{}
	for _, value := range values {
		out[strings.TrimSpace(value)] = true
	}
	return out
}

func telemetrySignalExporterEnv(signal string) string {
	switch signal {
	case "logs":
		return "OTEL_LOGS_EXPORTER"
	case "metrics":
		return "OTEL_METRICS_EXPORTER"
	case "traces":
		return "OTEL_TRACES_EXPORTER"
	default:
		return "OTEL_" + strings.ToUpper(signal) + "_EXPORTER"
	}
}

func mergeEnvironment(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(base)+len(extra))
	for _, value := range append(base, extra...) {
		key, _, _ := strings.Cut(value, "=")
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}
