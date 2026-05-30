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

func TestEventPeerResourceDecoding(t *testing.T) {
	const doc = `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventPeer
      metadata:
        name: cloud-secondary
      spec:
        groupRef: cloudedge
        nodeName: cloud-router
        endpoint: http://10.99.0.7:8787
        direction: push
        types:
          - routerd.client.ipv4.observed
        subjectPrefixes:
          - "10.88."
`

	var router api.Router
	if err := yaml.Unmarshal([]byte(doc), &router); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(router.Spec.Resources) != 1 {
		t.Fatalf("want 1 resource, got %d", len(router.Spec.Resources))
	}
	spec, err := router.Spec.Resources[0].EventPeerSpec()
	if err != nil {
		t.Fatalf("event peer spec: %v", err)
	}
	if spec.GroupRef != "cloudedge" {
		t.Fatalf("groupRef = %q, want cloudedge", spec.GroupRef)
	}
	if spec.NodeName != "cloud-router" {
		t.Fatalf("nodeName = %q, want cloud-router", spec.NodeName)
	}
	if spec.Endpoint != "http://10.99.0.7:8787" {
		t.Fatalf("endpoint = %q", spec.Endpoint)
	}
	if spec.Direction != "push" {
		t.Fatalf("direction = %q, want push", spec.Direction)
	}
	if len(spec.Types) != 1 || spec.Types[0] != "routerd.client.ipv4.observed" {
		t.Fatalf("types = %v", spec.Types)
	}
	if len(spec.SubjectPrefixes) != 1 || spec.SubjectPrefixes[0] != "10.88." {
		t.Fatalf("subjectPrefixes = %v", spec.SubjectPrefixes)
	}
}

func TestEventSubscriptionResourceDecoding(t *testing.T) {
	const doc = `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventSubscription
      metadata:
        name: claim-on-observe
      spec:
        groupRef: cloudedge
        match:
          types:
            - routerd.client.ipv4.observed
          subjectPrefixes:
            - "10.88."
          payload:
            ownerSide: onprem
          sourceNodes:
            - onprem-router
        trigger:
          pluginRef: remote-claim-provisioner
          batchWindow: 5s
          debounce: 2s
`

	var router api.Router
	if err := yaml.Unmarshal([]byte(doc), &router); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(router.Spec.Resources) != 1 {
		t.Fatalf("want 1 resource, got %d", len(router.Spec.Resources))
	}
	spec, err := router.Spec.Resources[0].EventSubscriptionSpec()
	if err != nil {
		t.Fatalf("event subscription spec: %v", err)
	}
	if spec.GroupRef != "cloudedge" {
		t.Fatalf("groupRef = %q, want cloudedge", spec.GroupRef)
	}
	if len(spec.Match.Types) != 1 || spec.Match.Types[0] != "routerd.client.ipv4.observed" {
		t.Fatalf("match.types = %v", spec.Match.Types)
	}
	if len(spec.Match.SubjectPrefixes) != 1 || spec.Match.SubjectPrefixes[0] != "10.88." {
		t.Fatalf("match.subjectPrefixes = %v", spec.Match.SubjectPrefixes)
	}
	if spec.Match.Payload["ownerSide"] != "onprem" {
		t.Fatalf("match.payload[ownerSide] = %q, want onprem", spec.Match.Payload["ownerSide"])
	}
	if len(spec.Match.SourceNodes) != 1 || spec.Match.SourceNodes[0] != "onprem-router" {
		t.Fatalf("match.sourceNodes = %v", spec.Match.SourceNodes)
	}
	if spec.Trigger.PluginRef != "remote-claim-provisioner" {
		t.Fatalf("trigger.pluginRef = %q, want remote-claim-provisioner", spec.Trigger.PluginRef)
	}
	if spec.Trigger.BatchWindow != "5s" {
		t.Fatalf("trigger.batchWindow = %q, want 5s", spec.Trigger.BatchWindow)
	}
	if spec.Trigger.Debounce != "2s" {
		t.Fatalf("trigger.debounce = %q, want 2s", spec.Trigger.Debounce)
	}
}
