// SPDX-License-Identifier: BSD-3-Clause

package api_test

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"gopkg.in/yaml.v3"
)

func TestEventGroupResourceDecoding(t *testing.T) {
	const doc = `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventGroup
      metadata:
        name: cloudedge
      spec:
        nodeName: onprem-router
        retention:
          maxEvents: 1000
          maxAge: 30m
        auth:
          mode: hmac
          secretFile: /etc/routerd/federation.key
`

	var router api.Router
	if err := yaml.Unmarshal([]byte(doc), &router); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(router.Spec.Resources) != 1 {
		t.Fatalf("want 1 resource, got %d", len(router.Spec.Resources))
	}
	spec, err := router.Spec.Resources[0].EventGroupSpec()
	if err != nil {
		t.Fatalf("event group spec: %v", err)
	}
	if spec.NodeName != "onprem-router" {
		t.Fatalf("nodeName = %q, want onprem-router", spec.NodeName)
	}
	if spec.Retention.MaxEvents != 1000 {
		t.Fatalf("maxEvents = %d, want 1000", spec.Retention.MaxEvents)
	}
	if spec.Retention.MaxAge != "30m" {
		t.Fatalf("maxAge = %q, want 30m", spec.Retention.MaxAge)
	}
	if spec.Auth.Mode != "hmac" {
		t.Fatalf("auth.mode = %q, want hmac", spec.Auth.Mode)
	}
}
