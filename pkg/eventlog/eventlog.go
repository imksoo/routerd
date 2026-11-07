// SPDX-License-Identifier: BSD-3-Clause

package eventlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/syslog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

type Level string

const (
	LevelDebug   Level = "debug"
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

var levelOverride atomic.Int32

type Event struct {
	Timestamp  time.Time         `json:"timestamp"`
	Level      Level             `json:"level"`
	Message    string            `json:"message"`
	Router     string            `json:"router,omitempty"`
	Command    string            `json:"command,omitempty"`
	ResourceID string            `json:"resourceID,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"`

	// Archive delivery is required before local deletion. Operational verbosity
	// overrides must not silently acknowledge an archive that was not sent.
	archive bool
}

type Sink interface {
	Emit(Event) error
	Close() error
}

type Logger struct {
	mu            sync.RWMutex
	configuration *api.Router
	closed        bool
	router        string
	sinks         []Sink
}

func New(router *api.Router) (*Logger, error) {
	return NewSelected(router, nil)
}

// NewSelected builds a logger from named LogSink resources. An empty selection
// selects every enabled sink for ordinary logging. Explicit selections are
// archive destinations: each requested sink must exist, be enabled, and accept
// the info records emitted before retention deletes the original rows.
func NewSelected(router *api.Router, refs []string) (*Logger, error) {
	if router == nil {
		return nil, fmt.Errorf("logger router is required")
	}
	explicitSelection := len(refs) > 0
	selected := map[string]bool{}
	for _, ref := range refs {
		const prefix = "LogSink/"
		name := strings.TrimPrefix(strings.TrimSpace(ref), prefix)
		if name == "" {
			return nil, fmt.Errorf("selected LogSink reference is empty")
		}
		selected[name] = true
	}
	var sinks []Sink
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.SystemAPIVersion || res.Kind != "LogSink" {
			continue
		}
		if explicitSelection && !selected[res.Metadata.Name] {
			continue
		}
		spec, err := res.LogSinkSpec()
		if err != nil {
			return nil, err
		}
		if !api.BoolDefault(spec.Enabled, true) {
			if explicitSelection {
				return nil, fmt.Errorf("selected %s is disabled; archive cannot be delivered", res.ID())
			}
			continue
		}
		if explicitSelection && parseLevel(defaultString(spec.MinLevel, "info")) > parseLevel("info") {
			return nil, fmt.Errorf("selected %s minLevel %q suppresses archived info records", res.ID(), spec.MinLevel)
		}
		sink, err := NewSink(spec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		sinks = append(sinks, sink)
		delete(selected, res.Metadata.Name)
	}
	if explicitSelection && len(selected) > 0 {
		missing := make([]string, 0, len(selected))
		for name := range selected {
			missing = append(missing, name)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("selected LogSink not found: %s", strings.Join(missing, ", "))
	}
	return &Logger{configuration: router, router: router.Metadata.Name, sinks: sinks}, nil
}

func NewSink(spec api.LogSinkSpec) (Sink, error) {
	minLevel := parseLevel(defaultString(spec.MinLevel, "info"))
	switch spec.Type {
	case "syslog":
		facility, err := syslogFacility(defaultString(spec.Syslog.Facility, "local6"))
		if err != nil {
			return nil, err
		}
		tag := defaultString(spec.Syslog.Tag, "routerd")
		return &SyslogSink{
			network:  spec.Syslog.Network,
			address:  spec.Syslog.Address,
			facility: facility,
			tag:      tag,
			minLevel: minLevel,
		}, nil
	case "otlp":
		return nil, fmt.Errorf("LogSink type otlp is unsupported; use Telemetry or ObservabilityPipeline OTLP export")
	case "webhook":
		timeout := 5 * time.Second
		if spec.Webhook.Timeout != "" {
			parsed, err := time.ParseDuration(spec.Webhook.Timeout)
			if err != nil {
				return nil, fmt.Errorf("webhook.timeout: %w", err)
			}
			timeout = parsed
		}
		return &WebhookSink{url: spec.Webhook.URL, headers: spec.Webhook.Headers, timeout: timeout, minLevel: minLevel}, nil
	case "file":
		name := strings.TrimSpace(spec.File.Name)
		if name == "" {
			name = "routerd-events"
		}
		return &FileSink{path: filepath.Join("/var/lib/routerd/logs", name+".jsonl"), minLevel: minLevel}, nil
	case "journald":
		return &JournaldSink{identifier: defaultString(spec.Journald.Identifier, "routerd"), minLevel: minLevel}, nil
	default:
		return nil, fmt.Errorf("unsupported log sink type %q", spec.Type)
	}
}

func (l *Logger) Emit(level Level, command, message string, fields map[string]string) {
	if l == nil {
		return
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed || len(l.sinks) == 0 {
		return
	}
	event := Event{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Message:   message,
		Router:    l.router,
		Command:   command,
		Fields:    fields,
	}
	for _, sink := range l.sinks {
		_ = sink.Emit(event)
	}
}

// EmitRecord forwards one structured operational-log row without imposing a
// schema on sinks that only understand Event. The encoded record is preserved
// as JSON so downstream log systems can parse it independently.
func (l *Logger) EmitRecord(signal string, record map[string]any) error {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return errors.New("logger is closed")
	}
	if len(l.sinks) == 0 {
		return nil
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	event := Event{
		Timestamp: time.Now().UTC(),
		Level:     LevelInfo,
		Message:   "routerd operational log archived",
		Router:    l.router,
		Command:   "log-retention",
		archive:   true,
		Fields: map[string]string{
			"signal": signal,
			"record": string(data),
		},
	}
	for _, sink := range l.sinks {
		if err := sink.Emit(event); err != nil {
			return err
		}
	}
	return nil
}

// Reconfigure publishes sinks for a confirmed active configuration. Holding the
// writer lock joins existing deliveries before old destinations are closed.
func (l *Logger) Reconfigure(router *api.Router) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errors.New("logger is closed")
	}
	if l.configuration == router {
		return nil
	}
	next, err := New(router)
	if err != nil {
		return err
	}
	previous := l.sinks
	l.configuration, l.router, l.sinks = router, next.router, next.sinks
	var failures []error
	for _, sink := range previous {
		if err := sink.Close(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	var errs []string
	for _, sink := range l.sinks {
		if err := sink.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

type SyslogSink struct {
	mu       sync.Mutex
	closed   bool
	network  string
	address  string
	facility syslog.Priority
	tag      string
	conn     net.Conn
	minLevel int
}

func (s *SyslogSink) Emit(event Event) error {
	if !shouldEmit(event, s.minLevel) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("syslog sink is closed")
	}
	deadline := time.Now().Add(5 * time.Second)
	priority := syslog.LOG_INFO
	switch event.Level {
	case LevelDebug:
		priority = syslog.LOG_DEBUG
	case LevelWarning:
		priority = syslog.LOG_WARNING
	case LevelError:
		priority = syslog.LOG_ERR
	}
	message := formatEvent(event)
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	var err error
	// Reconnect once after a failed write, sharing the original deadline.
	for attempt := 0; attempt < 2; attempt++ {
		if s.conn == nil {
			s.conn, err = s.dial(deadline)
			if err != nil {
				return err
			}
		}
		local := s.network == "" || s.network == "unix" || s.network == "unixgram"
		header := time.Now().Format(time.Stamp)
		if !local {
			host, _ := os.Hostname()
			if host == "" {
				host = s.conn.LocalAddr().String()
			}
			header = time.Now().Format(time.RFC3339) + " " + host
		}
		frame := fmt.Sprintf("<%d>%s %s[%d]: %s", s.facility|priority, header, s.tag, os.Getpid(), message)
		err = s.conn.SetDeadline(deadline)
		if err == nil {
			var n int
			n, err = s.conn.Write([]byte(frame))
			if err == nil && n != len(frame) {
				err = io.ErrShortWrite
			}
		}
		if err == nil {
			return nil
		}
		_ = s.conn.Close()
		s.conn = nil
		if !time.Now().Before(deadline) {
			return err
		}
	}
	return err
}

// dial preserves local socket discovery and bounds all attempts by one deadline.
func (s *SyslogSink) dial(deadline time.Time) (net.Conn, error) {
	dialer := net.Dialer{Deadline: deadline}
	if s.network != "" {
		return dialer.Dial(s.network, s.address)
	}
	var lastErr error
	for _, network := range []string{"unixgram", "unix"} {
		for _, path := range []string{"/dev/log", "/var/run/syslog", "/var/run/log"} {
			conn, err := dialer.Dial(network, path)
			if err == nil {
				return conn, nil
			}
			lastErr = err
			if !time.Now().Before(deadline) {
				return nil, err
			}
		}
	}
	return nil, fmt.Errorf("local syslog delivery: %w", lastErr)
}

func (s *SyslogSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

type WebhookSink struct {
	url      string
	headers  map[string]string
	timeout  time.Duration
	minLevel int
	client   *http.Client
}

func (s *WebhookSink) Emit(event Event) error {
	if !shouldEmit(event, s.minLevel) {
		return nil
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	client := s.client
	if client == nil {
		client = &http.Client{Timeout: s.timeout}
	}
	req, err := http.NewRequest(http.MethodPost, s.url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range s.headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook log sink returned %s", resp.Status)
	}
	return nil
}

func (s *WebhookSink) Close() error {
	return nil
}

type FileSink struct {
	path     string
	minLevel int
}

func (s *FileSink) Emit(event Event) error {
	if !shouldEmit(event, s.minLevel) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

func (s *FileSink) Close() error {
	return nil
}

type JournaldSink struct {
	identifier string
	minLevel   int
}

func (s *JournaldSink) Emit(event Event) error {
	if !shouldEmit(event, s.minLevel) {
		return nil
	}
	log.Printf("%s %s", s.identifier, formatEvent(event))
	return nil
}

func (s *JournaldSink) Close() error {
	return nil
}

func formatEvent(event Event) string {
	parts := []string{string(event.Level), event.Message}
	if event.Router != "" {
		parts = append(parts, "router="+event.Router)
	}
	if event.Command != "" {
		parts = append(parts, "command="+event.Command)
	}
	if event.ResourceID != "" {
		parts = append(parts, "resource="+event.ResourceID)
	}
	for key, value := range event.Fields {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, " ")
}

func syslogFacility(value string) (syslog.Priority, error) {
	switch value {
	case "kern":
		return syslog.LOG_KERN, nil
	case "user":
		return syslog.LOG_USER, nil
	case "mail":
		return syslog.LOG_MAIL, nil
	case "daemon":
		return syslog.LOG_DAEMON, nil
	case "auth":
		return syslog.LOG_AUTH, nil
	case "syslog":
		return syslog.LOG_SYSLOG, nil
	case "lpr":
		return syslog.LOG_LPR, nil
	case "news":
		return syslog.LOG_NEWS, nil
	case "uucp":
		return syslog.LOG_UUCP, nil
	case "cron":
		return syslog.LOG_CRON, nil
	case "authpriv":
		return syslog.LOG_AUTHPRIV, nil
	case "ftp":
		return syslog.LOG_FTP, nil
	case "local0":
		return syslog.LOG_LOCAL0, nil
	case "local1":
		return syslog.LOG_LOCAL1, nil
	case "local2":
		return syslog.LOG_LOCAL2, nil
	case "local3":
		return syslog.LOG_LOCAL3, nil
	case "local4":
		return syslog.LOG_LOCAL4, nil
	case "local5":
		return syslog.LOG_LOCAL5, nil
	case "local6":
		return syslog.LOG_LOCAL6, nil
	case "local7":
		return syslog.LOG_LOCAL7, nil
	default:
		return 0, fmt.Errorf("unsupported syslog facility %q", value)
	}
}

func parseLevel(value string) int {
	switch value {
	case "debug":
		return 0
	case "info":
		return 1
	case "warning":
		return 2
	case "error":
		return 3
	default:
		return 1
	}
}

func SetLevelOverride(level *Level) {
	if level == nil {
		levelOverride.Store(0)
		return
	}
	levelOverride.Store(int32(parseLevel(string(*level)) + 1))
}

func LevelOverride() *Level {
	switch levelOverride.Load() {
	case 1:
		level := LevelDebug
		return &level
	case 2:
		level := LevelInfo
		return &level
	case 3:
		level := LevelWarning
		return &level
	case 4:
		level := LevelError
		return &level
	default:
		return nil
	}
}

func shouldEmit(event Event, minLevel int) bool {
	if override := levelOverride.Load(); override > 0 && !event.archive {
		minLevel = int(override - 1)
	}
	return parseLevel(string(event.Level)) >= minLevel
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
