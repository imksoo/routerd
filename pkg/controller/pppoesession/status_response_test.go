// SPDX-License-Identifier: BSD-3-Clause

package pppoesession

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStatusEndpointErrorCannotPublishConnectedState(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`{"resources":[{"resource":{"kind":"PPPoESession","name":"wan"},"phase":"Connected"}]}`))}
	status, err := decodeDaemonStatus(response)
	if err == nil || len(status.Resources) != 0 {
		t.Fatalf("error response supplied operational state: %+v err=%v", status, err)
	}
}

func TestStatusEndpointAcceptsSuccessfulDaemonPayload(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"resources":[{"resource":{"kind":"PPPoESession","name":"wan"},"phase":"Connected"}]}`))}
	status, err := decodeDaemonStatus(response)
	if err != nil || len(status.Resources) != 1 || status.Resources[0].Phase != "Connected" {
		t.Fatalf("successful daemon payload: %+v err=%v", status, err)
	}
}
