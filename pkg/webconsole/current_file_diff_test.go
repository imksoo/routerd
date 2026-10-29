// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestCurrentFileDiffUsesBoundedServerDiffAndRedactsBothInputs(t *testing.T) {
	dir := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	common := strings.Repeat("# unchanged configuration context\n", 10000)
	previous := common + "metadata:\n  name: previous-router\nprivateKey: previous-private-value\n"
	current := common + "metadata:\n  name: current-router\nprivateKey: current-private-value\n"
	generation, err := store.BeginGeneration("fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordGenerationConfig(generation, previous); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(path, []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Options{Store: store, ConfigPath: path}))
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/generations/1/diff/current")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"--- generation-1.yaml\n+++ current-file.yaml\n@@", "previous-router", "current-router"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing diff content %q", want)
		}
	}
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/plain") || strings.Contains(text, "previous-private-value") || strings.Contains(text, "current-private-value") {
		t.Fatalf("current diff failed or exposed a secret: status=%d type=%s length=%d", response.StatusCode, response.Header.Get("Content-Type"), len(body))
	}
}
