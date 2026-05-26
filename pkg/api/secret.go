// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

const RedactedSecret = "***REDACTED***"

var exactSecretKeys = map[string]struct{}{
	"password":         {},
	"passwordfile":     {},
	"privatekey":       {},
	"privatekeyfile":   {},
	"presharedkey":     {},
	"presharedkeyfile": {},
	"authkey":          {},
	"authkeyenv":       {},
	"authkeyfile":      {},
	"initialpassword":  {},
	"psk":              {},
	"secret":           {},
	"bearer":           {},
	"token":            {},
	"apikey":           {},
	"clientsecret":     {},
}

var suffixSecretKeys = []string{
	"password",
	"passwordfile",
	"passwordfrom",
	"privatekey",
	"privatekeyfile",
	"presharedkey",
	"presharedkeyfile",
	"authkey",
	"authkeyenv",
	"authkeyfile",
	"initialpassword",
	"psk",
	"secret",
	"bearer",
	"token",
	"apikey",
	"clientsecret",
}

var nonSecretKeys = map[string]struct{}{
	"passwordauthentication": {},
	"wheelneedspassword":     {},
	"secretencoding":         {},
}

// IsSecretKey reports whether a config key should be redacted when exposed
// through unprivileged read-only surfaces.
func IsSecretKey(key string) bool {
	normalized := normalizeSecretKey(key)
	if normalized == "" {
		return false
	}
	if _, ok := nonSecretKeys[normalized]; ok {
		return false
	}
	if _, ok := exactSecretKeys[normalized]; ok {
		return true
	}
	for _, suffix := range suffixSecretKeys {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func normalizeSecretKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', '.', ' ':
			return -1
		default:
			return r
		}
	}, key)
	return key
}

// RedactSecrets returns a JSON-compatible copy of value with secret-looking
// fields replaced by RedactedSecret.
func RedactSecrets(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	redactJSONLike(out)
	return out, nil
}

// RedactYAMLSecrets returns YAML with secret-looking fields replaced by
// RedactedSecret while preserving keys for read-only config viewers.
func RedactYAMLSecrets(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return text, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return "", err
	}
	redactYAMLNode(&doc)
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func redactJSONLike(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if IsSecretKey(key) {
				typed[key] = RedactedSecret
				continue
			}
			redactJSONLike(child)
		}
	case []any:
		for _, child := range typed {
			redactJSONLike(child)
		}
	}
}

func redactYAMLNode(node *yaml.Node) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			redactYAMLNode(child)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			value := node.Content[i+1]
			if key.Kind == yaml.ScalarNode && IsSecretKey(key.Value) {
				redactYAMLValue(value)
				continue
			}
			redactYAMLNode(value)
		}
	}
}

func redactYAMLValue(node *yaml.Node) {
	if node == nil {
		return
	}
	node.Kind = yaml.ScalarNode
	node.Tag = "!!str"
	node.Value = RedactedSecret
	node.Style = yaml.DoubleQuotedStyle
	node.Content = nil
	node.Anchor = ""
	node.Alias = nil
}
