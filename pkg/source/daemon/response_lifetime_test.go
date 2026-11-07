// SPDX-License-Identifier: BSD-3-Clause

package daemon

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func eventLoopbackClient(server *httptest.Server) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}}
}

func TestDaemonSourceIncompleteResponseCannotPublishOrAdvance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"cursor":"new","events":[{"type":"routerd.daemon.lifecycle.ready"}]} {`))
	}))
	defer server.Close()
	client := eventLoopbackClient(server)
	defer client.CloseIdleConnections()
	publisher := &capturePublisher{}
	source := DaemonSource{Publisher: publisher}
	if next, err := source.poll(context.Background(), client, "prior", 0); err == nil || next != "" || len(publisher.events) != 0 {
		t.Fatalf("partial observation accepted cursor=%q events=%d err=%v", next, len(publisher.events), err)
	}
}

func TestDaemonSourceWorkerCannotHoldFastForwardForever(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client := eventLoopbackClient(server)
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancel()
	started := time.Now()
	if _, _, err := (DaemonSource{}).fastForward(ctx, client); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("worker timeout=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 7*time.Second {
		t.Fatalf("worker consumed whole source lifetime: %s", elapsed)
	}
}

func TestDaemonSourceRejectsOversizedCompleteResponse(t *testing.T) {
	body := `{"cursor":"` + strings.Repeat("x", 4<<20) + `","events":[]}`
	if _, err := readEventsResponse(strings.NewReader(body)); err == nil {
		t.Fatal("oversized IPC response accepted")
	}
}
