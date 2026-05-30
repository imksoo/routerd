// SPDX-License-Identifier: BSD-3-Clause

package eventd_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/imksoo/routerd/pkg/eventd"
)

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// postRaw POSTs body to url with the given timestamp/signature headers and
// returns the response status code, draining the body.
func postRaw(t *testing.T, url string, ts int64, sig string, body []byte) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(eventd.HeaderTimestamp, strconv.FormatInt(ts, 10))
	req.Header.Set(eventd.HeaderSignature, sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}
