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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventconsumer"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
}

type Controller struct {
	Router     *api.Router
	Bus        *bus.Bus
	Store      Store
	Events     eventconsumer.Store
	HTTPClient *http.Client
	Stdout     io.Writer
	Poll       time.Duration

	mu      sync.Mutex
	running map[string]runningExporter
}

func (c *Controller) Start(ctx context.Context) error {
	return c.Reconcile(ctx)
}

// Stop acknowledges that every exporter owned by this controller has exited.
// A failed stop retains the worker entry so a generation cannot mistake a
// cancellation request for completed delivery teardown.
func (c *Controller) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, current := range c.running {
		current.cancel()
	}
	for name, current := range c.running {
		select {
		case <-current.done:
			delete(c.running, name)
			continue
		default:
		}
		select {
		case <-current.done:
			delete(c.running, name)
		case <-ctx.Done():
			return fmt.Errorf("stop pipeline %s: %w", name, ctx.Err())
		}
	}
	return nil
}

func (c *Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Bus == nil || c.Events == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running == nil {
		c.running = map[string]runningExporter{}
	}
	desired := map[string]string{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.SystemAPIVersion || resource.Kind != "ObservabilityPipeline" {
			continue
		}
		spec, err := resource.ObservabilityPipelineSpec()
		if err != nil {
			return err
		}
		exporter, err := c.exporter(resource.Metadata.Name, spec)
		if err != nil {
			return err
		}
		if !exporter.enabled {
			continue
		}
		signature := observabilityPipelineSignature(spec)
		desired[resource.Metadata.Name] = signature
		if current, ok := c.running[resource.Metadata.Name]; ok && current.signature == signature {
			if err := c.saveStatus(resource.Metadata.Name, "Running", exporter); err != nil {
				return err
			}
			continue
		}
		if current, ok := c.running[resource.Metadata.Name]; ok {
			if err := stopExporter(ctx, current); err != nil {
				return fmt.Errorf("stop pipeline %s before replacement: %w", resource.Metadata.Name, err)
			}
		}
		runCtx, cancel := context.WithCancel(ctx)
		ch, _ := c.Bus.Subscribe(runCtx, bus.Subscription{Topics: []string{"routerd.**"}}, 256)
		done := make(chan struct{})
		go func() { defer close(done); exporter.run(runCtx, ch, c.Events, c.Poll) }()
		c.running[resource.Metadata.Name] = runningExporter{cancel: cancel, signature: signature, done: done}
		if err := c.saveStatus(resource.Metadata.Name, "Running", exporter); err != nil {
			return err
		}
	}
	for name, current := range c.running {
		if _, ok := desired[name]; ok {
			continue
		}
		if err := stopExporter(ctx, current); err != nil {
			return fmt.Errorf("stop pipeline %s: %w", name, err)
		}
		if err := c.saveStoppedStatus(name); err != nil {
			return err
		}
		delete(c.running, name)
	}
	return nil
}

type runningExporter struct {
	cancel    context.CancelFunc
	signature string
	done      <-chan struct{}
}

func stopExporter(ctx context.Context, current runningExporter) error {
	current.cancel()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-current.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("exporter did not stop before deadline")
	}
}

func observabilityPipelineSignature(spec api.ObservabilityPipelineSpec) string {
	data, _ := json.Marshal(spec)
	return string(data)
}

func (c *Controller) exporter(name string, spec api.ObservabilityPipelineSpec) (*Exporter, error) {
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
	if !exporter.enabled {
		return exporter, nil
	}
	for _, sink := range spec.Logs.Sinks {
		if !api.BoolDefault(sink.Enabled, true) {
			continue
		}
		switch sink.Type {
		case "stdout", "syslog", "loki":
		default:
			return nil, fmt.Errorf("pipeline %s: unsupported log sink %q", name, sink.Type)
		}
		exporter.sinks = append(exporter.sinks, sink)
	}
	return exporter, nil
}

func (c *Controller) saveStatus(name, phase string, exporter *Exporter) error {
	if c.Store == nil {
		return nil
	}
	sinkTypes := []string{}
	for _, sink := range exporter.sinks {
		sinkTypes = append(sinkTypes, sink.Type)
	}
	return c.Store.SaveObjectStatus(api.SystemAPIVersion, "ObservabilityPipeline", name, map[string]any{
		"phase":      phase,
		"backend":    "builtin",
		"sinks":      sinkTypes,
		"sampleRate": exporter.sampleRate,
		"observedAt": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (c *Controller) saveStoppedStatus(name string) error {
	if c.Store == nil {
		return nil
	}
	return c.Store.SaveObjectStatus(api.SystemAPIVersion, "ObservabilityPipeline", name, map[string]any{
		"phase":      "Stopped",
		"backend":    "builtin",
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
}

func (e *Exporter) run(ctx context.Context, wakeEvents <-chan bus.Event, store eventconsumer.Store, poll time.Duration) {
	if poll <= 0 {
		poll = time.Second
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	wake := make(chan struct{}, 1)
	var retry eventconsumer.Backoff
	wake <- struct{}{}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-wakeEvents:
			if !ok {
				return
			}
			select {
			case wake <- struct{}{}:
			default:
			}
		case <-ticker.C:
			select {
			case wake <- struct{}{}:
			default:
			}
		case <-wake:
			if !retry.Ready() {
				continue
			}
			if err := eventconsumer.Drain(ctx, store, "observabilitypipeline/"+e.name, e.processStoredEvent); err != nil {
				retry.Failure()
			} else {
				retry.Success()
			}
		}
	}
}

func (e *Exporter) processStoredEvent(ctx context.Context, event daemonapi.DaemonEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	selected, err := e.sample(event.Cursor)
	if err != nil {
		return err
	}
	if !selected {
		return nil
	}
	for _, sink := range e.sinks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if severityRank(event.Severity) < severityRank(defaultString(sink.MinLevel, "info")) {
			continue
		}
		if err := e.export(ctx, sink, event); err != nil {
			return err
		}
	}
	return nil
}

func (e *Exporter) sample(cursor string) (bool, error) {
	if e.sampleRate >= 1 {
		return true, nil
	}
	if e.sampleRate <= 0 {
		return false, nil
	}
	id, err := strconv.ParseUint(cursor, 10, 64)
	if err != nil {
		return false, fmt.Errorf("sampling requires stored event cursor: %w", err)
	}
	return float64(id%1000)/1000 < e.sampleRate, nil
}

func (e *Exporter) export(ctx context.Context, sink api.ObservabilityPipelineLogSink, event daemonapi.DaemonEvent) error {
	switch sink.Type {
	case "stdout":
		return json.NewEncoder(e.stdout).Encode(event)
	case "syslog":
		return e.exportSyslog(ctx, sink, event)
	case "loki":
		return e.exportLoki(ctx, sink, event)
	default:
		return fmt.Errorf("unsupported log sink %q", sink.Type)
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
	case "kern":
		return syslog.LOG_KERN
	case "user":
		return syslog.LOG_USER
	case "mail":
		return syslog.LOG_MAIL
	case "daemon":
		return syslog.LOG_DAEMON
	case "auth":
		return syslog.LOG_AUTH
	case "syslog":
		return syslog.LOG_SYSLOG
	case "lpr":
		return syslog.LOG_LPR
	case "news":
		return syslog.LOG_NEWS
	case "uucp":
		return syslog.LOG_UUCP
	case "cron":
		return syslog.LOG_CRON
	case "authpriv":
		return syslog.LOG_AUTHPRIV
	case "ftp":
		return syslog.LOG_FTP
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
