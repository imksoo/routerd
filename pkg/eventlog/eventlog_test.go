package eventlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"routerd/pkg/api"
)

func TestPluginSinkWritesEventJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell plugin fixture is Unix-only")
	}
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "log-plugin.sh")
	outputPath := filepath.Join(dir, "event.json")
	script := "#!/bin/sh\ncat > " + outputPath + "\n"
	if err := os.WriteFile(pluginPath, []byte(script), 0755); err != nil {
		t.Fatalf("write plugin: %v", err)
	}

	sink, err := NewSink(api.LogSinkSpec{
		Type:     "plugin",
		MinLevel: "debug",
		Plugin: api.LogSinkPluginSpec{
			Path:    pluginPath,
			Timeout: "2s",
		},
	})
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}

	err = sink.Emit(Event{
		Timestamp: time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC),
		Level:     LevelInfo,
		Message:   "test event",
		Router:    "test-router",
		Command:   "reconcile",
		Fields:    map[string]string{"phase": "Healthy"},
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if event.Message != "test event" || event.Router != "test-router" || event.Fields["phase"] != "Healthy" {
		t.Fatalf("event = %+v", event)
	}
}

func TestPluginSinkHonorsMinLevel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell plugin fixture is Unix-only")
	}
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "log-plugin.sh")
	outputPath := filepath.Join(dir, "event.json")
	script := "#!/bin/sh\ncat > " + outputPath + "\n"
	if err := os.WriteFile(pluginPath, []byte(script), 0755); err != nil {
		t.Fatalf("write plugin: %v", err)
	}

	sink, err := NewSink(api.LogSinkSpec{
		Type:     "plugin",
		MinLevel: "warning",
		Plugin:   api.LogSinkPluginSpec{Path: pluginPath},
	})
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	if err := sink.Emit(Event{Level: LevelInfo, Message: "ignored"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if _, err := os.Stat(outputPath); err == nil || !os.IsNotExist(err) {
		t.Fatalf("output file exists after ignored event: %v", err)
	}
}

func TestFormatEvent(t *testing.T) {
	line := formatEvent(Event{
		Level:   LevelWarning,
		Message: "changed",
		Router:  "r1",
		Command: "reconcile",
		Fields:  map[string]string{"phase": "Drifted"},
	})
	for _, want := range []string{"warning", "changed", "router=r1", "command=reconcile", "phase=Drifted"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line %q does not contain %q", line, want)
		}
	}
}
