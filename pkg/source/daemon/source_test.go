package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"routerd/pkg/daemonapi"
)

type capturePublisher struct {
	events []daemonapi.DaemonEvent
}

func (p *capturePublisher) Publish(_ context.Context, event daemonapi.DaemonEvent) error {
	p.events = append(p.events, event)
	return nil
}

func TestDaemonSourcePollsUnixSocketEvents(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "daemon.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/events" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(EventsResponse{
			Cursor: "1",
			Events: []daemonapi.DaemonEvent{{
				Cursor:   "1",
				Type:     daemonapi.EventDaemonReady,
				Severity: daemonapi.SeverityInfo,
			}},
		})
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	publisher := &capturePublisher{}
	source := DaemonSource{
		Daemon:    daemonapi.DaemonRef{Name: "wan-pd", Kind: "routerd-dhcp6-client"},
		Socket:    socket,
		Publisher: publisher,
	}
	if _, err := source.poll(context.Background(), httpClientForUnixSocket(socket), "", 0); err != nil {
		t.Fatal(err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("events = %d", len(publisher.events))
	}
	if publisher.events[0].Daemon.Name != "wan-pd" {
		t.Fatalf("daemon ref not filled: %+v", publisher.events[0].Daemon)
	}
}
