// SPDX-License-Identifier: BSD-3-Clause

package provideraction

import (
	"os"
	"strings"
	"testing"
)

func TestExecutorEnvironmentNoInherit(t *testing.T) {
	os.Setenv("ROUTERD_SECRET_PROBE", "leak")
	defer os.Unsetenv("ROUTERD_SECRET_PROBE")
	env := executorEnvironment(map[string]string{"FOO": "bar"})
	for _, kv := range env {
		if strings.HasPrefix(kv, "ROUTERD_SECRET_PROBE=") {
			t.Fatalf("parent env leaked: %q", kv)
		}
	}
	var sawFoo, sawPath bool
	for _, kv := range env {
		if kv == "FOO=bar" {
			sawFoo = true
		}
		if strings.HasPrefix(kv, "PATH=") {
			sawPath = true
		}
	}
	if !sawFoo || !sawPath {
		t.Fatalf("env missing FOO/PATH: %v", env)
	}
}
