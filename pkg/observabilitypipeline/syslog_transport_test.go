// SPDX-License-Identifier: BSD-3-Clause

package observabilitypipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

// Preserve the explicit UDP and Unix transports supported by routerd 1.0.
// The receiver uses only standard socket/JSON APIs. This checks actual wire
// delivery, not local socket discovery, journal recovery or remote durability.
func TestSyslogExistingTransportDelivery(t *testing.T) {
	for _, network := range []string{"udp", "unixgram", "unix"} {
		t.Run(network, func(t *testing.T) {
			address := "127.0.0.1:0"
			if network != "udp" {
				address = filepath.Join(t.TempDir(), "syslog.sock")
			}
			deadline := time.Now().Add(5 * time.Second)
			var receive func() ([]byte, error)
			if network == "unix" {
				listener, err := net.ListenUnix(network, &net.UnixAddr{Name: address, Net: network})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := listener.Close(); err != nil {
						t.Error(err)
					}
				})
				if err := listener.SetDeadline(deadline); err != nil {
					t.Fatal(err)
				}
				receive = func() ([]byte, error) {
					conn, err := listener.Accept()
					if err != nil {
						return nil, err
					}
					defer conn.Close()
					if err := conn.SetReadDeadline(deadline); err != nil {
						return nil, err
					}
					return io.ReadAll(io.LimitReader(conn, 8193))
				}
			} else {
				listener, err := net.ListenPacket(network, address)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := listener.Close(); err != nil {
						t.Error(err)
					}
				})
				address = listener.LocalAddr().String()
				if err := listener.SetReadDeadline(deadline); err != nil {
					t.Fatal(err)
				}
				receive = func() ([]byte, error) {
					data := make([]byte, 8193)
					n, _, err := listener.ReadFrom(data)
					return data[:n], err
				}
			}

			const message = "quoted \"value\", Unicode 日本語\nand another line"
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "transport-test"}, "routerd.syslog.preserve", daemonapi.SeverityWarning)
			event.Cursor = "41"
			event.Message = message
			sink := api.ObservabilityPipelineLogSink{Type: "syslog", Syslog: api.LogSinkSyslogSpec{
				Network: network, Address: address, Facility: "local0", Tag: "routerd-preserve",
			}}
			ctx, cancel := context.WithDeadline(t.Context(), deadline)
			defer cancel()
			if err := (&Exporter{}).export(ctx, sink, event); err != nil {
				t.Fatalf("export: %v", err)
			}
			data, err := receive()
			if err != nil {
				t.Fatalf("receiver did not observe delivery: %v", err)
			}
			// local0 (16*8) + warning (4); one newline frames the JSON record.
			if len(data) > 8192 || !bytes.HasPrefix(data, []byte("<132>")) || !bytes.Contains(data, []byte(" routerd-preserve[")) || !bytes.HasSuffix(data, []byte{'\n'}) || bytes.Count(data, []byte{'\n'}) != 1 {
				t.Fatalf("unexpected syslog framing: %q", data)
			}
			start := bytes.IndexByte(data, '{')
			if start < 0 {
				t.Fatalf("missing JSON event: %q", data)
			}
			var got struct {
				Cursor   string `json:"cursor"`
				Type     string `json:"type"`
				Severity string `json:"severity"`
				Message  string `json:"message"`
			}
			if err := json.Unmarshal(data[start:], &got); err != nil {
				t.Fatal(err)
			}
			if got.Cursor != "41" || got.Type != "routerd.syslog.preserve" || got.Severity != "warning" || got.Message != message {
				t.Fatalf("receiver lost event identity or content: %+v", got)
			}
		})
	}
}
