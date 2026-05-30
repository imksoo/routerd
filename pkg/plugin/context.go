// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"encoding/json"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

// PluginContext is the least-privilege, secret-redacted slice of configuration
// delivered to a plugin on stdin (Phase 4.0). It carries ONLY the resources a
// Plugin allowlisted via spec.context.resources, and every secret has been
// stripped by BuildPluginContext before it is populated.
//
// SECURITY MODEL (redaction policy A, non-negotiable):
//   - inline secret VALUES are blanked,
//   - secret FILE-PATH fields are OMITTED entirely (the plugin is not even told
//     where the secret lives),
//   - SecretValueSourceSpec-typed / *From secret-source sub-objects are OMITTED,
//   - the full router.yaml / non-allowlisted resources are NEVER included,
//   - no provider credentials flow through routerd to the plugin.
//
// There is no opt-out toggle: redaction always runs.
type PluginContext struct {
	Resources []PluginContextResource `yaml:"resources,omitempty" json:"resources,omitempty"`
}

// PluginContextResource is one redacted resource passed to a plugin. Spec is a
// generic JSON-serializable map produced by redacting the typed resource spec,
// so it is resilient to new Kinds and never leaks unknown secret-shaped fields.
type PluginContextResource struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind" json:"kind"`
	Name       string         `yaml:"name" json:"name"`
	Spec       map[string]any `yaml:"spec,omitempty" json:"spec,omitempty"`
}

// secretKeyFragments are case-insensitive substrings whose presence in a JSON
// object key marks the value as a secret to BLANK (scalar) or OMIT (object).
// This is the generic safety net so a newly added secret-bearing Kind is
// redacted even before anyone updates a type-aware list.
var secretKeyFragments = []string{
	"secret",
	"password",
	"privatekey",
	"presharedkey",
	"psk",
	"token",
	"credential",
	"passphrase",
	"authkey",
}

// secretFileKeySuffixes are case-insensitive key suffixes that name a path to a
// secret. Keys matching these are OMITTED entirely (we do not even tell the
// plugin where the secret lives). They are kept conservative so we drop secret
// file/path pointers without nuking innocuous *File fields like configFile.
var secretFileKeySuffixes = []string{
	"keyfile",
	"secretfile",
	"passwordfile",
}

// BuildPluginContext resolves the allowlist against the effective config and
// returns the redacted context to hand to a plugin. For each allow ref it finds
// the matching resource by apiVersion+kind+name; a missing ref is skipped (no
// error). Each matched resource is DEEP-COPIED (the caller's resource is never
// mutated) and redacted per policy A before inclusion. An empty/absent allowlist
// yields an empty context (default-deny).
func BuildPluginContext(allow []api.PluginContextResourceRef, resources []api.Resource) (PluginContext, error) {
	var ctx PluginContext
	for _, ref := range allow {
		res, found := findResource(ref, resources)
		if !found {
			// Missing ref: simply absent from the passed context. Skip silently;
			// the controller may log this if desired.
			continue
		}
		redacted, err := redactResource(res)
		if err != nil {
			return PluginContext{}, err
		}
		ctx.Resources = append(ctx.Resources, redacted)
	}
	return ctx, nil
}

func findResource(ref api.PluginContextResourceRef, resources []api.Resource) (api.Resource, bool) {
	for _, res := range resources {
		if res.APIVersion == ref.APIVersion && res.Kind == ref.Kind && res.Metadata.Name == ref.Name {
			return res, true
		}
	}
	return api.Resource{}, false
}

// redactResource deep-copies a resource's spec via a JSON round-trip (which also
// makes it generic + serializable) and walks it removing every secret. The JSON
// round-trip both prevents mutating the caller's typed resource and normalizes
// typed specs (including SecretValueSourceSpec sub-objects) into a plain map the
// walker can inspect uniformly.
func redactResource(res api.Resource) (PluginContextResource, error) {
	out := PluginContextResource{
		APIVersion: res.APIVersion,
		Kind:       res.Kind,
		Name:       res.Metadata.Name,
	}
	if res.Spec == nil {
		return out, nil
	}
	raw, err := json.Marshal(res.Spec)
	if err != nil {
		return PluginContextResource{}, err
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		// Spec is not an object (e.g. scalar/array). Nothing key-addressable to
		// redact; omit it rather than risk leaking an un-walkable secret blob.
		return out, nil
	}
	out.Spec = redactMap(generic)
	return out, nil
}

// redactMap walks a generic JSON object, returning a new map with secrets
// removed. Rules:
//   - a key naming a secret file/path (suffix keyFile/secretFile/passwordFile)
//     is OMITTED entirely;
//   - a key whose name contains a secret fragment is OMITTED if its value is an
//     object (a secret-source sub-object like *From), else BLANKED to "" if it
//     is a scalar/array (an inline secret value);
//   - SecretValueSourceSpec-shaped sub-objects (those carrying file/env secret
//     pointers under a *From / *secret* key) are removed by the rule above;
//   - everything else is preserved, recursing into nested maps and slices so
//     non-secret fields stay intact.
func redactMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, val := range in {
		if isSecretFileKey(key) {
			// Omit entirely: do not reveal the secret's location.
			continue
		}
		if isSecretKey(key) {
			switch val.(type) {
			case map[string]any:
				// Secret-source sub-object (e.g. passwordFrom: {file/env}). Omit.
				continue
			default:
				// Inline secret scalar/array value. Blank it.
				out[key] = ""
			}
			continue
		}
		// SecretValueSourceSpec-typed fields are conventionally named with a
		// "From" suffix (passwordFrom, authenticationFrom, ...) and carry a
		// file/env/base64 secret pointer. Their key name need not contain a
		// secret fragment, so detect them structurally and OMIT entirely.
		if obj, ok := val.(map[string]any); ok && isSecretSourceObject(key, obj) {
			continue
		}
		out[key] = redactValue(val)
	}
	return out
}

func redactValue(val any) any {
	switch v := val.(type) {
	case map[string]any:
		return redactMap(v)
	case []any:
		arr := make([]any, len(v))
		for i, elem := range v {
			arr[i] = redactValue(elem)
		}
		return arr
	default:
		return v
	}
}

func isSecretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, frag := range secretKeyFragments {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// isSecretSourceObject reports whether an object value is a SecretValueSourceSpec
// (a {file?, env?, base64?} secret pointer). It is keyed conservatively on the
// "From" suffix convention AND the secret-source shape so ordinary nested
// objects ending in "from" (none exist today, but be safe) are not nuked unless
// they actually carry a file/env secret pointer.
func isSecretSourceObject(key string, obj map[string]any) bool {
	if !strings.HasSuffix(strings.ToLower(key), "from") {
		return false
	}
	if len(obj) == 0 {
		return false
	}
	for k := range obj {
		switch strings.ToLower(k) {
		case "file", "env", "base64":
		default:
			return false
		}
	}
	return true
}

func isSecretFileKey(key string) bool {
	lower := strings.ToLower(key)
	for _, suffix := range secretFileKeySuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}
