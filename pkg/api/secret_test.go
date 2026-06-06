// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type specsSecretField struct {
	Struct string
	Field  string
	JSON   string
	YAML   string
}

func (f specsSecretField) id() string {
	return f.Struct + "." + f.Field
}

func TestSpecsSecretFieldsAreReviewedAndRedacted(t *testing.T) {
	fields := collectSpecsSecretFields(t)

	expected := map[string]specsSecretField{
		"BGPPeerSpec.Password":                     {Struct: "BGPPeerSpec", Field: "Password", JSON: "password", YAML: "password"},
		"BGPPeerSpec.PasswordFrom":                 {Struct: "BGPPeerSpec", Field: "PasswordFrom", JSON: "passwordFrom", YAML: "passwordFrom"},
		"IPsecConnectionSpec.PreSharedKey":         {Struct: "IPsecConnectionSpec", Field: "PreSharedKey", JSON: "preSharedKey", YAML: "preSharedKey"},
		"NixOSUserSpec.InitialPassword":            {Struct: "NixOSUserSpec", Field: "InitialPassword", JSON: "initialPassword", YAML: "initialPassword"},
		"PPPoESessionSpec.Password":                {Struct: "PPPoESessionSpec", Field: "Password", JSON: "password", YAML: "password"},
		"PPPoESessionSpec.PasswordFile":            {Struct: "PPPoESessionSpec", Field: "PasswordFile", JSON: "passwordFile", YAML: "passwordFile"},
		"SAMTransportPeerWgSpec.PresharedKey":      {Struct: "SAMTransportPeerWgSpec", Field: "PresharedKey", JSON: "presharedKey", YAML: "presharedKey"},
		"SAMTransportPeerWgSpec.PresharedKeyFile":  {Struct: "SAMTransportPeerWgSpec", Field: "PresharedKeyFile", JSON: "presharedKeyFile", YAML: "presharedKeyFile"},
		"SAMTransportWireGuardSpec.PrivateKey":     {Struct: "SAMTransportWireGuardSpec", Field: "PrivateKey", JSON: "privateKey", YAML: "privateKey"},
		"SAMTransportWireGuardSpec.PrivateKeyFile": {Struct: "SAMTransportWireGuardSpec", Field: "PrivateKeyFile", JSON: "privateKeyFile", YAML: "privateKeyFile"},
		"TailscaleNodeSpec.AuthKey":                {Struct: "TailscaleNodeSpec", Field: "AuthKey", JSON: "authKey", YAML: "authKey"},
		"TailscaleNodeSpec.AuthKeyEnv":             {Struct: "TailscaleNodeSpec", Field: "AuthKeyEnv", JSON: "authKeyEnv", YAML: "authKeyEnv"},
		"TailscaleNodeSpec.AuthKeyFile":            {Struct: "TailscaleNodeSpec", Field: "AuthKeyFile", JSON: "authKeyFile", YAML: "authKeyFile"},
		"WireGuardInterfaceSpec.PrivateKey":        {Struct: "WireGuardInterfaceSpec", Field: "PrivateKey", JSON: "privateKey", YAML: "privateKey"},
		"WireGuardInterfaceSpec.PrivateKeyFile":    {Struct: "WireGuardInterfaceSpec", Field: "PrivateKeyFile", JSON: "privateKeyFile", YAML: "privateKeyFile"},
		"WireGuardPeerSpec.PresharedKey":           {Struct: "WireGuardPeerSpec", Field: "PresharedKey", JSON: "presharedKey", YAML: "presharedKey"},
		"WireGuardPeerSpec.PresharedKeyFile":       {Struct: "WireGuardPeerSpec", Field: "PresharedKeyFile", JSON: "presharedKeyFile", YAML: "presharedKeyFile"},
	}

	got := map[string]specsSecretField{}
	for _, field := range fields {
		got[field.id()] = field
	}
	if diff := diffSpecsSecretFields(expected, got); diff != "" {
		t.Fatalf("secret-looking fields in specs.go changed; review redaction coverage and update this test:\n%s", diff)
	}

	for _, field := range fields {
		t.Run(field.id(), func(t *testing.T) {
			sentinel := "REDACTION_SENTINEL_" + field.Struct + "_" + field.Field
			jsonKey := exposedKey(field.JSON, field.Field)
			redacted, err := RedactSecrets(map[string]any{
				"spec": map[string]any{
					jsonKey: sentinel,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(redacted)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), sentinel) {
				t.Fatalf("RedactSecrets leaked %s via JSON key %q: %s", sentinel, jsonKey, data)
			}
			if !strings.Contains(string(data), RedactedSecret) {
				t.Fatalf("RedactSecrets did not emit %q for JSON key %q: %s", RedactedSecret, jsonKey, data)
			}

			yamlKey := exposedKey(field.YAML, field.Field)
			redactedYAML, err := RedactYAMLSecrets(fmt.Sprintf("spec:\n  %s: %s\n", yamlKey, sentinel))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(redactedYAML, sentinel) {
				t.Fatalf("RedactYAMLSecrets leaked %s via YAML key %q:\n%s", sentinel, yamlKey, redactedYAML)
			}
			for _, want := range []string{yamlKey + ":", RedactedSecret} {
				if !strings.Contains(redactedYAML, want) {
					t.Fatalf("RedactYAMLSecrets missing %q for YAML key %q:\n%s", want, yamlKey, redactedYAML)
				}
			}
		})
	}
}

func collectSpecsSecretFields(t *testing.T) []specsSecretField {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "specs.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var out []specsSecretField
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range structType.Fields.List {
				for _, name := range field.Names {
					if name == nil {
						continue
					}
					jsonKey, yamlKey := structFieldKeys(t, field, name.Name)
					if !IsSecretKey(name.Name) && !IsSecretKey(jsonKey) && !IsSecretKey(yamlKey) {
						continue
					}
					out = append(out, specsSecretField{
						Struct: typeSpec.Name.Name,
						Field:  name.Name,
						JSON:   jsonKey,
						YAML:   yamlKey,
					})
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].id() < out[j].id() })
	return out
}

func structFieldKeys(t *testing.T, field *ast.Field, fieldName string) (string, string) {
	t.Helper()

	jsonKey := fieldName
	yamlKey := fieldName
	if field.Tag == nil {
		return jsonKey, yamlKey
	}
	tagText, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		t.Fatalf("invalid struct tag on %s: %v", fieldName, err)
	}
	tag := reflect.StructTag(tagText)
	if value, ok := tag.Lookup("json"); ok {
		jsonKey = strings.Split(value, ",")[0]
	}
	if value, ok := tag.Lookup("yaml"); ok {
		yamlKey = strings.Split(value, ",")[0]
	}
	return jsonKey, yamlKey
}

func exposedKey(tagKey, fieldName string) string {
	if tagKey == "" || tagKey == "-" {
		return fieldName
	}
	return tagKey
}

func diffSpecsSecretFields(expected, got map[string]specsSecretField) string {
	var lines []string
	for id := range got {
		if _, ok := expected[id]; !ok {
			lines = append(lines, "unexpected: "+id)
		}
	}
	for id := range expected {
		if _, ok := got[id]; !ok {
			lines = append(lines, "missing: "+id)
		}
	}
	for id, gotField := range got {
		expectedField, ok := expected[id]
		if !ok {
			continue
		}
		if gotField.JSON != expectedField.JSON || gotField.YAML != expectedField.YAML {
			lines = append(lines, fmt.Sprintf("changed tags: %s json=%q yaml=%q, want json=%q yaml=%q", id, gotField.JSON, gotField.YAML, expectedField.JSON, expectedField.YAML))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
