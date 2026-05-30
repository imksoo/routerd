// SPDX-License-Identifier: BSD-3-Clause

package provideraction

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNoCloudSDKImports is the Phase 5.0 no-real-cloud invariant for the engine
// core: pkg/provideraction must import no cloud SDK. (It DOES use os/exec — that
// is the single, audited executor launch point in executor.go — so os/exec is
// allowed here, unlike in the executor plugins.)
func TestNoCloudSDKImports(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	dir := filepath.Dir(thisFile)
	forbidden := []string{
		"github.com/aws/",
		"github.com/Azure/",
		"github.com/oracle/oci-go-sdk",
		"cloud.google.com/go",
	}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatal(perr)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if p == bad || strings.HasPrefix(p, bad) {
					t.Errorf("%s imports forbidden cloud SDK %q (Phase 5.0 no-real-cloud invariant)", name, p)
				}
			}
		}
	}
}
