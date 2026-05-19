// SPDX-License-Identifier: BSD-3-Clause

package observabilitypipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/syslog"
	"net/http"
	"os"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
}

type Controller struct {
	Router     *api.Router
	Bus        *bus.Bus
	Store      Store
	HTTPClient *http.Client
	Stdout     io.Writer
}

func (c *Controller) Start(ctx context.Context) error {
	if c.Router == nil || c.Bus == nil {
		return nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.SystemAPIVersion || resource.Kind != "ObservabilityPipeline" {
			continue
		}
		spec, err := resource.ObservabilityPipelineSpec()
		if err != nil {
			return err
		}
		exporter := c.exporter(resource.Metadata.Name, spec)
		if !exporter.enabled {
			continue
		}
		ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.**"}}, 256)
		go exporter.run(ctx, ch)
		c.saveStatus(resource.Metadata.Name, "Running", exporter)
	}
	return nil
}

func (c *Controller) exporter(name string, spec api.ObservabilityPipelineSpec) *Exporter {
	stdout := c.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	exporter := &Exporter{
		name:       name,
		enabled:    api.BoolDefault(spec.Logs.Enabled, true),
		sampleRate: spec.Sampling.Rate,
		httpClient: httpClient,
		stdout:     stdout,
		attrs:      spec.Attributes,
	}
	if exporter.sampleRate == 0 {
		exporter.sampleRate = 1
	}
	for _, sink := range spec.Logs.Sinks {
		if !api.BoolDefault(sink.Enabled, true) || sink.Type == "kafka" {
			continue
		}
		exporter.sinks = append(exporter.sinks, sink)
	}
	return exporter
}

func (c *Controller) saveStatus(name, phase string, exporter *Exporter) {
	if c.Store == nil {
		return
	}
	sinkTypes := []string{}
	for _, sink := range exporter.sinks {
		sinkTypes = append(sinkTypes, sink.Type)
	}
	_ = c.Store.SaveObjectStatus(api.SystemAPIVersion, "ObservabilityPipeline", name, map[string]any{
		"phase":      phase,
		"backend":    "builtin",
		"sinks":      sinkTypes,
		"sampleRate": exporter.sampleRate,
		"observedAt": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

type Exporter struct {
	name       string
	enabled    bool
	sampleRate float64
	httpClient *http.Client
	stdout     io.Writer
	attrs      map[string]string
	sinks      []api.ObservabilityPipelineLogSink
	seq        uint64
}

func (e *Exporter) run(ctx context.Context, events <-chan bus.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if !e.sample() {
				continue
			}
			for _, sink := range e.sinks {
				if severityRank(event.Severity) < severityRank(defaultString(sink.MinLevel, "info")) {
					continue
				}
				_ = e.export(ctx, sink, event)
			}
		}
	}
}

func (e *Exporter) sample() bool {
	if e.sampleRate >= 1 {
		return true
	}
	if e.sampleRate <= 0 {
		return false
	}
	e.seq++
	return float64(e.seq%1000)/1000 < e.sampleRate
}

func (e *Exporter) export(ctx context.Context, sink api.ObservabilityPipelineLogSink, event daemonapi.DaemonEvent) error {
	switch sink.Type {
	case "stdout":
		return json.NewEncoder(e.stdout).Encode(event)
	case "syslog":
		return e.exportSyslog(sink, event)
	case "loki":
		return e.exportLoki(ctx, sink, event)
	default:
		return nil
	}
}

func (e *Exporter) exportSyslog(sink api.ObservabilityPipelineLogSink, event daemonapi.DaemonEvent) error {
	network := strings.TrimSpace(sink.Syslog.Network)
	address := strings.TrimSpace(sink.Syslog.Address)
	priority := syslog.LOG_INFO | syslogFacility(sink.Syslog.Facility)
	writer, err := syslog.Dial(network, address, priority, defaultString(sink.Syslog.Tag, "routerd"))
	if err != nil {
		return err
	}
	defer writer.Close()
	line, _ := json.Marshal(event)
	switch event.Severity {
	case daemonapi.SeverityError:
		return writer.Err(string(line))
	case daemonapi.SeverityWarning:
		return writer.Warning(string(line))
	default:
		return writer.Info(string(line))
	}
}

func (e *Exporter) exportLoki(ctx context.Context, sink api.ObservabilityPipelineLogSink, event daemonapi.DaemonEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	labels := map[string]string{
		"job":      "routerd",
		"pipeline": e.name,
		"severity": defaultString(event.Severity, "info"),
		"topic":    sanitizeLabel(event.Type),
	}
	for key, value := range e.attrs {
		labels[sanitizeLabel(key)] = value
	}
	for key, value := range sink.Labels {
		labels[sanitizeLabel(key)] = value
	}
	body, _ := json.Marshal(map[string]any{
		"streams": []any{map[string]any{
			"stream": labels,
			"values": [][]string{{fmt.Sprintf("%d", eventTime(event).UnixNano()), string(payload)}},
		}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sink.Loki.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if sink.Loki.Tenant != "" {
		req.Header.Set("X-Scope-OrgID", sink.Loki.Tenant)
	}
	for key, value := range sink.Loki.Headers {
		req.Header.Set(key, value)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("loki push status %s", resp.Status)
	}
	return nil
}

func eventTime(event daemonapi.DaemonEvent) time.Time {
	if event.Time.IsZero() {
		return time.Now().UTC()
	}
	return event.Time
}

func severityRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return 0
	case "", "info":
		return 1
	case "warning", "warn":
		return 2
	case "error":
		return 3
	default:
		return 1
	}
}

func syslogFacility(value string) syslog.Priority {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "local0":
		return syslog.LOG_LOCAL0
	case "local1":
		return syslog.LOG_LOCAL1
	case "local2":
		return syslog.LOG_LOCAL2
	case "local3":
		return syslog.LOG_LOCAL3
	case "local4":
		return syslog.LOG_LOCAL4
	case "local5":
		return syslog.LOG_LOCAL5
	case "local7":
		return syslog.LOG_LOCAL7
	default:
		return syslog.LOG_LOCAL6
	}
}

func sanitizeLabel(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "value"
	}
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
