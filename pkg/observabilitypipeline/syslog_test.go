// SPDX-License-Identifier: BSD-3-Clause

package observabilitypipeline

import (
	"bufio"
	"context"
	"fmt"
	"log/syslog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestSyslogExportUsesConfiguredFacilityAndErrorSeverity(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	line := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			line <- ""
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		value, _ := bufio.NewReader(conn).ReadString('\n')
		line <- value
	}()
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test"}, "routerd.test", daemonapi.SeverityError)
	err = (&Exporter{}).exportSyslog(t.Context(), api.ObservabilityPipelineLogSink{Type: "syslog", Syslog: api.LogSinkSyslogSpec{Network: "tcp", Address: listener.Addr().String(), Facility: "daemon", Tag: "routerd-test"}}, event)
	if err != nil {
		t.Fatal(err)
	}
	if got := <-line; !strings.HasPrefix(got, fmt.Sprintf("<%d>", syslog.LOG_DAEMON|syslog.LOG_ERR)) || !strings.Contains(got, `"type":"routerd.test"`) {
		t.Fatalf("syslog wire record = %q", got)
	}
}

func TestSyslogExportHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := (&Exporter{}).exportSyslog(ctx, api.ObservabilityPipelineLogSink{Type: "syslog", Syslog: api.LogSinkSyslogSpec{Network: "tcp", Address: "127.0.0.1:1"}}, daemonapi.DaemonEvent{})
	if err == nil {
		t.Fatal("canceled export reported delivery")
	}
}
