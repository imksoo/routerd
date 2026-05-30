// SPDX-License-Identifier: BSD-3-Clause

// Package eventd holds the transport core for the CloudEdge Event Federation
// daemon (routerd-eventd, ADR 0006 Phase 2). It contains the receiver HTTP
// handler, the peer push client, and the retention prune loop, factored out of
// cmd/routerd-eventd so they are unit-testable with httptest without spawning a
// process.
//
// SCOPE: transport only. This package does NOT implement EventSubscription,
// plugin triggering, DynamicConfigPart generation, ARP observers, provider
// mutation, or controller/systemd auto-supervision; those belong to a later
// chunk.
package eventd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Default tunables used when the config leaves a field zero.
const (
	DefaultReplayWindow  = 5 * time.Minute
	DefaultPruneInterval = time.Minute
	DefaultPushInterval  = 10 * time.Second
	DefaultMaxAttempts   = 3
	DefaultBaseBackoff   = 200 * time.Millisecond
	DefaultMaxBackoff    = 5 * time.Second
)

// Listen is the HTTP receive bind. Address is bound verbatim; it is NOT
// defaulted to 0.0.0.0 — an empty address means "no TCP receiver" (the daemon
// can still push). Bind to the specific configured address only.
type Listen struct {
	Address string `json:"address,omitempty"`
	Port    int    `json:"port,omitempty"`
}

// PeerConfig is one push target. Filters mirror EventPeerSpec: empty Types means
// "all types", empty SubjectPrefixes means "all subjects".
type PeerConfig struct {
	NodeName        string   `json:"nodeName"`
	Endpoint        string   `json:"endpoint"`
	Types           []string `json:"types,omitempty"`
	SubjectPrefixes []string `json:"subjectPrefixes,omitempty"`
}

// Retention mirrors EventGroupSpec.Retention.
type Retention struct {
	MaxEvents int           `json:"maxEvents,omitempty"`
	MaxAge    time.Duration `json:"maxAge,omitempty"`
}

// PushRetry controls the bounded exponential backoff used by the push client.
type PushRetry struct {
	MaxAttempts int           `json:"maxAttempts,omitempty"`
	BaseBackoff time.Duration `json:"baseBackoff,omitempty"`
	MaxBackoff  time.Duration `json:"maxBackoff,omitempty"`
}

// Config is the routerd-eventd transport runtime, loaded from the JSON config
// file (--config-file). Durations accept Go duration strings ("5m", "200ms")
// because the duration fields use UnmarshalJSON-friendly time.Duration via the
// custom decode in LoadConfig.
type Config struct {
	NodeName      string        `json:"nodeName"`
	Group         string        `json:"group"`
	Listen        Listen        `json:"listen"`
	SecretFile    string        `json:"secretFile"`
	ReplayWindow  time.Duration `json:"replayWindow,omitempty"`
	Peers         []PeerConfig  `json:"peers,omitempty"`
	Retention     Retention     `json:"retention,omitempty"`
	PushRetry     PushRetry     `json:"pushRetry,omitempty"`
	PruneInterval time.Duration `json:"pruneInterval,omitempty"`
	PushInterval  time.Duration `json:"pushInterval,omitempty"`
	StatePath     string        `json:"statePath"`
}

// configWire is the on-disk shape with duration fields as strings so operators
// write human durations ("5m") rather than nanosecond integers.
type configWire struct {
	NodeName     string `json:"nodeName"`
	Group        string `json:"group"`
	Listen       Listen `json:"listen"`
	SecretFile   string `json:"secretFile"`
	ReplayWindow string `json:"replayWindow,omitempty"`
	Peers        []struct {
		NodeName        string   `json:"nodeName"`
		Endpoint        string   `json:"endpoint"`
		Types           []string `json:"types,omitempty"`
		SubjectPrefixes []string `json:"subjectPrefixes,omitempty"`
	} `json:"peers,omitempty"`
	Retention struct {
		MaxEvents int    `json:"maxEvents,omitempty"`
		MaxAge    string `json:"maxAge,omitempty"`
	} `json:"retention,omitempty"`
	PushRetry struct {
		MaxAttempts int    `json:"maxAttempts,omitempty"`
		BaseBackoff string `json:"baseBackoff,omitempty"`
		MaxBackoff  string `json:"maxBackoff,omitempty"`
	} `json:"pushRetry,omitempty"`
	PruneInterval string `json:"pruneInterval,omitempty"`
	PushInterval  string `json:"pushInterval,omitempty"`
	StatePath     string `json:"statePath"`
}

func parseDuration(field, value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s duration %q: %w", field, value, err)
	}
	return d, nil
}

// DecodeConfig parses the JSON config bytes into a Config, converting the
// string duration fields. It does NOT apply defaults or validate (call
// ApplyDefaults / Validate).
func DecodeConfig(unmarshal func(any) error) (Config, error) {
	var w configWire
	if err := unmarshal(&w); err != nil {
		return Config{}, err
	}
	cfg := Config{
		NodeName:   w.NodeName,
		Group:      w.Group,
		Listen:     w.Listen,
		SecretFile: w.SecretFile,
		StatePath:  w.StatePath,
	}
	var err error
	if cfg.ReplayWindow, err = parseDuration("replayWindow", w.ReplayWindow); err != nil {
		return Config{}, err
	}
	if cfg.PruneInterval, err = parseDuration("pruneInterval", w.PruneInterval); err != nil {
		return Config{}, err
	}
	if cfg.PushInterval, err = parseDuration("pushInterval", w.PushInterval); err != nil {
		return Config{}, err
	}
	if cfg.Retention.MaxAge, err = parseDuration("retention.maxAge", w.Retention.MaxAge); err != nil {
		return Config{}, err
	}
	cfg.Retention.MaxEvents = w.Retention.MaxEvents
	if cfg.PushRetry.BaseBackoff, err = parseDuration("pushRetry.baseBackoff", w.PushRetry.BaseBackoff); err != nil {
		return Config{}, err
	}
	if cfg.PushRetry.MaxBackoff, err = parseDuration("pushRetry.maxBackoff", w.PushRetry.MaxBackoff); err != nil {
		return Config{}, err
	}
	cfg.PushRetry.MaxAttempts = w.PushRetry.MaxAttempts
	for _, p := range w.Peers {
		cfg.Peers = append(cfg.Peers, PeerConfig{
			NodeName:        p.NodeName,
			Endpoint:        p.Endpoint,
			Types:           p.Types,
			SubjectPrefixes: p.SubjectPrefixes,
		})
	}
	return cfg, nil
}

// marshalDuration renders a time.Duration as the wire string form, emitting ""
// for non-positive values so omitempty drops the field.
func marshalDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}

// MarshalConfigJSON renders a Config to the on-disk wire form (duration fields
// as Go duration strings). It is the symmetric inverse of DecodeConfig: feeding
// its output back through DecodeConfig yields an equal Config.
func MarshalConfigJSON(c Config) ([]byte, error) {
	w := configWire{
		NodeName:      c.NodeName,
		Group:         c.Group,
		Listen:        c.Listen,
		SecretFile:    c.SecretFile,
		ReplayWindow:  marshalDuration(c.ReplayWindow),
		PruneInterval: marshalDuration(c.PruneInterval),
		PushInterval:  marshalDuration(c.PushInterval),
		StatePath:     c.StatePath,
	}
	w.Retention.MaxEvents = c.Retention.MaxEvents
	w.Retention.MaxAge = marshalDuration(c.Retention.MaxAge)
	w.PushRetry.MaxAttempts = c.PushRetry.MaxAttempts
	w.PushRetry.BaseBackoff = marshalDuration(c.PushRetry.BaseBackoff)
	w.PushRetry.MaxBackoff = marshalDuration(c.PushRetry.MaxBackoff)
	for _, p := range c.Peers {
		w.Peers = append(w.Peers, struct {
			NodeName        string   `json:"nodeName"`
			Endpoint        string   `json:"endpoint"`
			Types           []string `json:"types,omitempty"`
			SubjectPrefixes []string `json:"subjectPrefixes,omitempty"`
		}{
			NodeName:        p.NodeName,
			Endpoint:        p.Endpoint,
			Types:           p.Types,
			SubjectPrefixes: p.SubjectPrefixes,
		})
	}
	return json.MarshalIndent(w, "", "  ")
}

// LoadConfig reads the JSON config file at path, decodes durations, applies
// defaults, and validates. It is the entry point used by cmd/routerd-eventd.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg, err := DecodeConfig(func(v any) error { return json.Unmarshal(data, v) })
	if err != nil {
		return Config{}, err
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ApplyDefaults fills zero-valued tunables with package defaults.
func (c *Config) ApplyDefaults() {
	if c.ReplayWindow <= 0 {
		c.ReplayWindow = DefaultReplayWindow
	}
	if c.PruneInterval <= 0 {
		c.PruneInterval = DefaultPruneInterval
	}
	if c.PushInterval <= 0 {
		c.PushInterval = DefaultPushInterval
	}
	if c.PushRetry.MaxAttempts <= 0 {
		c.PushRetry.MaxAttempts = DefaultMaxAttempts
	}
	if c.PushRetry.BaseBackoff <= 0 {
		c.PushRetry.BaseBackoff = DefaultBaseBackoff
	}
	if c.PushRetry.MaxBackoff <= 0 {
		c.PushRetry.MaxBackoff = DefaultMaxBackoff
	}
}

// Validate reports whether the config has the fields required to run.
func (c Config) Validate() error {
	if strings.TrimSpace(c.NodeName) == "" {
		return fmt.Errorf("eventd config: nodeName is required")
	}
	if strings.TrimSpace(c.Group) == "" {
		return fmt.Errorf("eventd config: group is required")
	}
	if strings.TrimSpace(c.SecretFile) == "" {
		return fmt.Errorf("eventd config: secretFile is required")
	}
	if strings.TrimSpace(c.StatePath) == "" {
		return fmt.Errorf("eventd config: statePath is required")
	}
	for i, p := range c.Peers {
		if strings.TrimSpace(p.NodeName) == "" {
			return fmt.Errorf("eventd config: peers[%d].nodeName is required", i)
		}
		if strings.TrimSpace(p.Endpoint) == "" {
			return fmt.Errorf("eventd config: peers[%d].endpoint is required", i)
		}
	}
	return nil
}

// ReadSecretFile reads an HMAC shared secret from path, trimming surrounding
// whitespace and rejecting an empty file. It mirrors pkg/wireguard.readSecretFile.
func ReadSecretFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return nil, fmt.Errorf("empty secret file %s", path)
	}
	return []byte(value), nil
}
