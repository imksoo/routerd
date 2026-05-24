// SPDX-License-Identifier: BSD-3-Clause

package eventlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/syslog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"routerd/pkg/api"
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
}

type Sink interface {
	Emit(Event) error
	Close() error
}

type Logger struct {
	router string
	sinks  []Sink
}

func New(router *api.Router) (*Logger, error) {
	var sinks []Sink
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.SystemAPIVersion || res.Kind != "LogSink" {
			continue
		}
		spec, err := res.LogSinkSpec()
		if err != nil {
			return nil, err
		}
		if !api.BoolDefault(spec.Enabled, true) {
			continue
		}
		sink, err := NewSink(spec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		sinks = append(sinks, sink)
	}
	return &Logger{router: router.Metadata.Name, sinks: sinks}, nil
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
		if strings.TrimSpace(spec.OTLP.TelemetryRef) == "" && strings.TrimSpace(spec.OTLP.Endpoint) == "" {
			return nil, fmt.Errorf("otlp.telemetryRef or otlp.endpoint is required")
		}
		return &OTLPSink{minLevel: minLevel}, nil
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
	if l == nil || len(l.sinks) == 0 {
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

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
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
	network  string
	address  string
	facility syslog.Priority
	tag      string
	writer   *syslog.Writer
	minLevel int
}

func (s *SyslogSink) Emit(event Event) error {
	if !shouldEmit(event.Level, s.minLevel) {
		return nil
	}
	if s.writer == nil {
		writer, err := syslog.Dial(s.network, s.address, s.facility|syslog.LOG_INFO, s.tag)
		if err != nil {
			return err
		}
		s.writer = writer
	}
	line := formatEvent(event)
	switch event.Level {
	case LevelDebug:
		return s.writer.Debug(line)
	case LevelWarning:
		return s.writer.Warning(line)
	case LevelError:
		return s.writer.Err(line)
	default:
		return s.writer.Info(line)
	}
}

func (s *SyslogSink) Close() error {
	if s.writer == nil {
		return nil
	}
	return s.writer.Close()
}

type WebhookSink struct {
	url      string
	headers  map[string]string
	timeout  time.Duration
	minLevel int
	client   *http.Client
}

func (s *WebhookSink) Emit(event Event) error {
	if !shouldEmit(event.Level, s.minLevel) {
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
	if !shouldEmit(event.Level, s.minLevel) {
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
	if !shouldEmit(event.Level, s.minLevel) {
		return nil
	}
	log.Printf("%s %s", s.identifier, formatEvent(event))
	return nil
}

func (s *JournaldSink) Close() error {
	return nil
}

type OTLPSink struct {
	minLevel int
}

func (s *OTLPSink) Emit(event Event) error {
	if !shouldEmit(event.Level, s.minLevel) {
		return nil
	}
	return nil
}

func (s *OTLPSink) Close() error {
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

func shouldEmit(level Level, minLevel int) bool {
	if override := levelOverride.Load(); override > 0 {
		minLevel = int(override - 1)
	}
	return parseLevel(string(level)) >= minLevel
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
