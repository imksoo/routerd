package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/invopop/jsonschema"

	"routerd/pkg/api"
)

func main() {
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://routerd.net/schemas/routerd-config-v1alpha1.schema.json",
		"title":   "routerd config v1alpha1",
		"type":    "object",
		"required": []string{
			"apiVersion",
			"kind",
			"metadata",
			"spec",
		},
		"additionalProperties": false,
		"properties": map[string]any{
			"apiVersion": constString(api.RouterAPIVersion),
			"kind":       constString("Router"),
			"metadata":   metadataSchema(),
			"spec": map[string]any{
				"type":                 "object",
				"required":             []string{"resources"},
				"additionalProperties": false,
				"properties": map[string]any{
					"resources": map[string]any{
						"type":  "array",
						"items": resourceUnionSchema(),
					},
				},
			},
		},
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(schema); err != nil {
		fmt.Fprintf(os.Stderr, "encode schema: %v\n", err)
		os.Exit(1)
	}
}

func resourceUnionSchema() map[string]any {
	return map[string]any{
		"oneOf": []any{
			resourceSchema(api.SystemAPIVersion, "Sysctl", api.SysctlSpec{}),
			resourceSchema(api.NetAPIVersion, "Interface", api.InterfaceSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4StaticAddress", api.IPv4StaticAddressSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4DHCPAddress", api.IPv4DHCPAddressSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4DHCPServer", api.IPv4DHCPServerSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4DHCPScope", api.IPv4DHCPScopeSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv6DHCPAddress", api.IPv6DHCPAddressSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4DefaultRoute", api.IPv4DefaultRouteSpec{}),
			resourceSchema(api.NetAPIVersion, "IPv4SourceNAT", api.IPv4SourceNATSpec{}),
			resourceSchema(api.NetAPIVersion, "Hostname", api.HostnameSpec{}),
		},
	}
}

func resourceSchema(apiVersion, kind string, spec any) map[string]any {
	return map[string]any{
		"type": "object",
		"required": []string{
			"apiVersion",
			"kind",
			"metadata",
			"spec",
		},
		"additionalProperties": false,
		"properties": map[string]any{
			"apiVersion": constString(apiVersion),
			"kind":       constString(kind),
			"metadata":   metadataSchema(),
			"spec":       reflectedSchema(spec),
			"status": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
	}
}

func reflectedSchema(value any) map[string]any {
	reflector := jsonschema.Reflector{
		Anonymous:                 true,
		DoNotReference:            true,
		ExpandedStruct:            true,
		AllowAdditionalProperties: false,
	}
	data, err := json.Marshal(reflector.Reflect(value))
	if err != nil {
		panic(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		panic(err)
	}
	delete(schema, "$schema")
	delete(schema, "$id")
	return schema
}

func metadataSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"required":             []string{"name"},
		"additionalProperties": false,
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "minLength": 1},
		},
	}
}

func constString(value string) map[string]any {
	return map[string]any{
		"type":  "string",
		"const": value,
	}
}
