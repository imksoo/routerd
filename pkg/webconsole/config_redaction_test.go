// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/state"
	"gopkg.in/yaml.v3"
)

func TestConfigViewsRedactSecretsWithoutChangingSources(t *testing.T) {
	const oldYAML = `metadata:
  name: public-router-old
description: public-description-old
credentials:
  password: canary-password-old-7c26
  privateKey: canary-private-key-old-4e93
  token: canary-token-old-9b15
`
	const newYAML = `metadata:
  name: public-router-new
description: public-description-new
credentials:
  password: canary-password-new-2a84
  privateKey: canary-private-key-new-6d37
  token: canary-token-new-8f51
`
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(newYAML), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := state.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	oldID, err := store.BeginGeneration("redaction-old")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordGenerationConfig(oldID, oldYAML); err != nil {
		t.Fatal(err)
	}
	newID, err := store.BeginGeneration("redaction-new")
	if err != nil {
		t.Fatal(err)
	}
	if newID <= oldID {
		t.Fatal("expected two distinct ordered generations")
	}
	if err := store.RecordGenerationConfig(newID, newYAML); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Options{ConfigPath: configPath, Store: store}))
	t.Cleanup(server.Close)

	get := func(path, accept string) []byte {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", accept)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), accept) {
			t.Errorf("%s: unexpected Content-Type %q", path, resp.Header.Get("Content-Type"))
		}
		for _, secret := range []string{
			"canary-password-old-7c26", "canary-private-key-old-4e93", "canary-token-old-9b15",
			"canary-password-new-2a84", "canary-private-key-new-6d37", "canary-token-new-8f51",
		} {
			if bytes.Contains(body, []byte(secret)) {
				t.Errorf("%s: secret canary leaked", path)
			}
		}
		return body
	}
	checkConfig := func(data []byte, name, description string) {
		t.Helper()
		var doc struct {
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
			Description string `yaml:"description"`
			Credentials struct {
				Password   any `yaml:"password"`
				PrivateKey any `yaml:"privateKey"`
				Token      any `yaml:"token"`
			} `yaml:"credentials"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		if doc.Metadata.Name != name || doc.Description != description {
			t.Error("public configuration fields changed or missing")
		}
		if doc.Credentials.Password != "***REDACTED***" || doc.Credentials.PrivateKey != "***REDACTED***" || doc.Credentials.Token != "***REDACTED***" {
			t.Error("secret fields must be the string ***REDACTED***")
		}
	}
	var current ConfigSnapshot
	if err := json.Unmarshal(get("/api/v1/config", "application/json"), &current); err != nil {
		t.Fatal(err)
	}
	if current.Path != configPath {
		t.Error("current config path changed")
	}
	checkConfig([]byte(current.Text), "public-router-new", "public-description-new")
	checkConfig(get(fmt.Sprintf("/api/v1/generations/%d/config", oldID), "application/yaml"), "public-router-old", "public-description-old")
	checkConfig(get(fmt.Sprintf("/api/v1/generations/%d/config", newID), "application/yaml"), "public-router-new", "public-description-new")
	diffPath := fmt.Sprintf("/api/v1/generations/%d/diff/%d", oldID, newID)
	textDiff := string(get(diffPath, "text/plain"))
	var jsonDiff GenerationDiff
	if err := json.Unmarshal(get(diffPath, "application/json"), &jsonDiff); err != nil {
		t.Fatal(err)
	}
	if jsonDiff.From != oldID || jsonDiff.To != newID {
		t.Error("JSON diff generation IDs changed")
	}
	for _, diff := range []string{textDiff, jsonDiff.Diff} {
		var removedName, addedName, removedDescription, addedDescription bool
		for _, line := range strings.Split(diff, "\n") {
			if strings.HasPrefix(line, "-") {
				removedName = removedName || strings.Contains(line, "name: public-router-old")
				removedDescription = removedDescription || strings.Contains(line, "description: public-description-old")
			}
			if strings.HasPrefix(line, "+") {
				addedName = addedName || strings.Contains(line, "name: public-router-new")
				addedDescription = addedDescription || strings.Contains(line, "description: public-description-new")
			}
		}
		if !removedName || !addedName || !removedDescription || !addedDescription {
			t.Error("diff lost public old/new values or their change direction")
		}
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte(newYAML)) {
		t.Error("source config file changed")
	}
	storedOld, found, err := store.GenerationConfig(oldID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || !bytes.Equal([]byte(storedOld), []byte(oldYAML)) {
		t.Error("old generation source YAML changed or missing")
	}
	storedNew, found, err := store.GenerationConfig(newID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || !bytes.Equal([]byte(storedNew), []byte(newYAML)) {
		t.Error("new generation source YAML changed or missing")
	}
}
