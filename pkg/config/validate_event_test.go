// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func eventPeerRouter(spec api.EventPeerSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventPeer"},
				Metadata: api.ObjectMeta{Name: "peer"},
				Spec:     spec,
			},
		}},
	}
}

func TestValidateEventPeerOK(t *testing.T) {
	router := eventPeerRouter(api.EventPeerSpec{
		GroupRef:        "cloudedge",
		NodeName:        "cloud-router",
		Endpoint:        "http://10.99.0.7:8787",
		Direction:       "push",
		Types:           []string{"routerd.client.ipv4.observed"},
		SubjectPrefixes: []string{"10.88."},
	})
	if err := Validate(router); err != nil {
		t.Fatalf("validate EventPeer: %v", err)
	}
}

func TestValidateEventPeerDefaultsDirection(t *testing.T) {
	router := eventPeerRouter(api.EventPeerSpec{
		GroupRef: "cloudedge",
		NodeName: "cloud-router",
		Endpoint: "http://10.99.0.7:8787",
		// Direction empty -> defaults to push, must still pass.
	})
	if err := Validate(router); err != nil {
		t.Fatalf("validate EventPeer default direction: %v", err)
	}
}

func TestValidateEventPeerRejects(t *testing.T) {
	tests := []struct {
		name string
		spec api.EventPeerSpec
		want string
	}{
		{
			name: "missing groupRef",
			spec: api.EventPeerSpec{NodeName: "n", Endpoint: "http://x"},
			want: "spec.groupRef is required",
		},
		{
			name: "missing nodeName",
			spec: api.EventPeerSpec{GroupRef: "g", Endpoint: "http://x"},
			want: "spec.nodeName is required",
		},
		{
			name: "bad direction",
			spec: api.EventPeerSpec{GroupRef: "g", NodeName: "n", Endpoint: "http://x", Direction: "pull"},
			want: "spec.direction must be empty or push",
		},
		{
			name: "missing endpoint",
			spec: api.EventPeerSpec{GroupRef: "g", NodeName: "n"},
			want: "spec.endpoint is required",
		},
		{
			name: "empty type entry",
			spec: api.EventPeerSpec{GroupRef: "g", NodeName: "n", Endpoint: "http://x", Types: []string{"  "}},
			want: "spec.types[0] must not be empty",
		},
		{
			name: "empty subject prefix entry",
			spec: api.EventPeerSpec{GroupRef: "g", NodeName: "n", Endpoint: "http://x", SubjectPrefixes: []string{""}},
			want: "spec.subjectPrefixes[0] must not be empty",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(eventPeerRouter(tc.spec))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateEventPeerWrongAPIVersion(t *testing.T) {
	router := eventPeerRouter(api.EventPeerSpec{GroupRef: "g", NodeName: "n", Endpoint: "http://x"})
	router.Spec.Resources[0].APIVersion = api.RouterAPIVersion
	err := Validate(router)
	if err == nil || !strings.Contains(err.Error(), api.FederationAPIVersion) {
		t.Fatalf("Validate error = %v, want apiVersion complaint", err)
	}
}
