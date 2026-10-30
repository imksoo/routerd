// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type disconnectedEventWriter struct{ header http.Header }

func (w *disconnectedEventWriter) Header() http.Header     { return w.header }
func (*disconnectedEventWriter) WriteHeader(int)           {}
func (*disconnectedEventWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (*disconnectedEventWriter) Flush()                    {}

func TestEventStreamStopsWhenClientWriteFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := New(Options{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(&disconnectedEventWriter{header: http.Header{}}, request)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("failed SSE connection remained alive waiting for a later request cancellation")
	}
}
