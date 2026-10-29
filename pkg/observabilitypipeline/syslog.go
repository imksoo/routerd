// SPDX-License-Identifier: BSD-3-Clause

package observabilitypipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/syslog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func (e *Exporter) exportSyslog(ctx context.Context, sink api.ObservabilityPipelineLogSink, event daemonapi.DaemonEvent) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	network, address := strings.TrimSpace(sink.Syslog.Network), strings.TrimSpace(sink.Syslog.Address)
	conn, local, err := dialSyslog(ctx, network, address)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	priority := syslog.LOG_INFO
	switch event.Severity {
	case daemonapi.SeverityError:
		priority = syslog.LOG_ERR
	case daemonapi.SeverityWarning:
		priority = syslog.LOG_WARNING
	case daemonapi.SeverityDebug:
		priority = syslog.LOG_DEBUG
	}
	priority |= syslogFacility(sink.Syslog.Facility)
	tag := defaultString(sink.Syslog.Tag, "routerd")
	if local {
		_, err = fmt.Fprintf(conn, "<%d>%s %s[%d]: %s\n", priority, time.Now().Format(time.Stamp), tag, os.Getpid(), line)
	} else {
		hostname, _ := os.Hostname()
		_, err = fmt.Fprintf(conn, "<%d>%s %s %s[%d]: %s\n", priority, time.Now().Format(time.RFC3339), defaultString(hostname, "localhost"), tag, os.Getpid(), line)
	}
	return err
}

func dialSyslog(ctx context.Context, network, address string) (net.Conn, bool, error) {
	var dialer net.Dialer
	if network != "" {
		conn, err := dialer.DialContext(ctx, network, address)
		return conn, network == "unix" || network == "unixgram", err
	}
	// Match the base system's local syslog socket discovery. Every attempt is
	// owned by the same cancellation/deadline as the eventual write.
	var lastErr error
	for _, transport := range []string{"unixgram", "unix"} {
		for _, path := range []string{"/dev/log", "/var/run/syslog", "/var/run/log"} {
			conn, err := dialer.DialContext(ctx, transport, path)
			if err == nil {
				return conn, true, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, true, ctx.Err()
			}
		}
	}
	return nil, true, fmt.Errorf("local syslog delivery: %w", lastErr)
}
