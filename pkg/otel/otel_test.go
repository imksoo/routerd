// SPDX-License-Identifier: BSD-3-Clause

package otel

import (
	"runtime"
	"testing"

	"go.opentelemetry.io/otel/attribute"

	"github.com/imksoo/routerd/pkg/version"
)

func TestResourceAttributesMergeDefaultsEnvAndExplicit(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAMESPACE", "routerd")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=lab,routerd.node=router02,routerd.service.name=from-env")

	attrs := resourceAttributes("routerd", attribute.String("routerd.node", "override"))
	got := map[attribute.Key]string{}
	for _, attr := range attrs {
		if attr.Value.AsString() != "" {
			got[attr.Key] = attr.Value.AsString()
		}
	}

	for key, want := range map[attribute.Key]string{
		"service.name":           "routerd",
		"service.namespace":      "routerd",
		"service.version":        version.Version,
		"os.type":                runtime.GOOS,
		"deployment.environment": "lab",
		"routerd.node":           "override",
		"routerd.service.name":   "from-env",
	} {
		if got[key] != want {
			t.Fatalf("%s = %q, want %q; attrs=%v", key, got[key], want, attrs)
		}
	}
}

func TestParseResourceAttributesIgnoresMalformedFields(t *testing.T) {
	attrs := parseResourceAttributes("a=b,missing,=empty,c= d ")
	got := map[attribute.Key]string{}
	for _, attr := range attrs {
		got[attr.Key] = attr.Value.AsString()
	}
	if got["a"] != "b" || got["c"] != "d" {
		t.Fatalf("parsed attrs = %v", attrs)
	}
	if _, ok := got["missing"]; ok {
		t.Fatalf("malformed field was not ignored: %v", attrs)
	}
}

func TestRuntimeCachesMetricInstruments(t *testing.T) {
	r := &Runtime{ServiceName: "routerd-test"}
	if r.Counter("events") != r.Counter("events") {
		t.Fatal("counter was not cached")
	}
	if r.Gauge("state") != r.Gauge("state") {
		t.Fatal("gauge was not cached")
	}
	if r.Float64Gauge("ratio") != r.Float64Gauge("ratio") {
		t.Fatal("float64 gauge was not cached")
	}
}
