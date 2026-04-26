package config

import (
	"testing"

	"routerd/pkg/api"
)

func TestValidateRouterLabExample(t *testing.T) {
	router, err := Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load router-lab example: %v", err)
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate router-lab example: %v", err)
	}
}

func TestValidateRejectsMissingInterfaceReference(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "addr"},
				Spec: map[string]any{
					"interface": "missing",
					"address":   "192.168.1.32/24",
				},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected missing interface reference to be rejected")
	}
}

func TestValidateRejectsInvalidStaticAddress(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec: map[string]any{
					"ifname":  "ens19",
					"managed": true,
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "addr"},
				Spec: map[string]any{
					"interface": "lan",
					"address":   "not-a-prefix",
				},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected invalid IPv4 prefix to be rejected")
	}
}

func TestValidateRequiresOverlapReason(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec: map[string]any{
					"ifname":  "ens19",
					"managed": true,
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "addr"},
				Spec: map[string]any{
					"interface":    "lan",
					"address":      "192.168.160.3/24",
					"allowOverlap": true,
				},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected allowOverlap without reason to be rejected")
	}
}

func TestValidateRejectsDuplicateStaticOnSameInterface(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec: map[string]any{
					"ifname":  "ens19",
					"managed": true,
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "addr-a"},
				Spec: map[string]any{
					"interface": "lan",
					"address":   "192.168.160.3/24",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "addr-b"},
				Spec: map[string]any{
					"interface": "lan",
					"address":   "192.168.160.3/24",
				},
			},
		}},
	}

	if err := Validate(router); err == nil {
		t.Fatal("expected duplicate static address on same interface to be rejected")
	}
}
