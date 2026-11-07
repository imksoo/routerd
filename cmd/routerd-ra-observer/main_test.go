// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommandAcknowledgesOnlySupportedWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &daemon{cancel: cancel}
	response := httptest.NewRecorder()
	d.handleCommand(response, httptest.NewRequest(http.MethodPost, "/v1/commands", strings.NewReader(`{"command":"unknown"}`)))
	var result daemonapi.CommandResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusBadRequest || result.Accepted || ctx.Err() != nil {
		t.Fatalf("unknown command reported success: %+v", result)
	}
	response = httptest.NewRecorder()
	d.handleCommand(response, httptest.NewRequest(http.MethodPost, "/v1/commands", strings.NewReader(`{"command":"stop"}`)))
	if response.Code != http.StatusOK || ctx.Err() != context.Canceled {
		t.Fatal("stop did not cancel the owning observer")
	}
}
