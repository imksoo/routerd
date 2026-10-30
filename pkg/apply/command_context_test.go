// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNativeObservationStopsWithOperationContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := New(ctx).Command("sh", "-c", "exec sleep 30"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("native observation ignored operation deadline: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("native observer outlived its request")
	}
	if result, err := New(ctx).Observe(&api.Router{}); !errors.Is(err, context.DeadlineExceeded) || result != nil {
		t.Fatalf("canceled observation published a result: result=%+v err=%v", result, err)
	}
}

func TestNativeCommandErrorDoesNotExposeDumpStdout(t *testing.T) {
	out, err := New(t.Context()).Command("sh", "-c", "printf 'private-key-from-native-dump'; printf 'safe failure' >&2; exit 7")
	if err == nil || out != nil || strings.Contains(err.Error(), "private-key-from-native-dump") || !strings.Contains(err.Error(), "safe failure") {
		t.Fatalf("native error mixed dump stdout into diagnostics: out=%q err=%v", out, err)
	}
}
