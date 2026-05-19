// SPDX-License-Identifier: BSD-3-Clause

package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Config struct {
	Name      string
	Identity  string
	Peers     []string
	LeasePath string
	TTL       time.Duration
}

type Lease struct {
	Holder    string    `json:"holder"`
	ExpiresAt time.Time `json:"expiresAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type AcquiredLease struct {
	Config Config
	File   *os.File
	Lease  Lease
}

type Decision struct {
	Enabled   bool
	Leader    bool
	Identity  string
	Holder    string
	LeasePath string
	ExpiresAt time.Time
	Reason    string
	Lease     *AcquiredLease
}

func Acquire(ctx context.Context, cfg Config) (Decision, error) {
	cfg = normalizeConfig(cfg)
	if cfg.LeasePath == "" || len(cfg.Peers) == 0 {
		return Decision{Enabled: false, Leader: true, Identity: cfg.Identity, Reason: "disabled"}, nil
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LeasePath), 0o755); err != nil {
		return Decision{}, err
	}
	file, err := os.OpenFile(cfg.LeasePath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return Decision{}, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		current := readLease(file)
		_ = file.Close()
		if (err == syscall.EWOULDBLOCK || err == syscall.EAGAIN) && current.Holder != "" {
			return Decision{Enabled: true, Leader: false, Identity: cfg.Identity, Holder: current.Holder, LeasePath: cfg.LeasePath, ExpiresAt: current.ExpiresAt, Reason: "locked-by-peer"}, nil
		}
		if ctx.Err() != nil {
			return Decision{}, fmt.Errorf("acquire lease lock: %w", ctx.Err())
		}
		return Decision{}, err
	}
	now := time.Now().UTC()
	current := readLease(file)
	if current.Holder != "" && current.Holder != cfg.Identity && now.Before(current.ExpiresAt) {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return Decision{Enabled: true, Leader: false, Identity: cfg.Identity, Holder: current.Holder, LeasePath: cfg.LeasePath, ExpiresAt: current.ExpiresAt, Reason: "held-by-peer"}, nil
	}
	lease := Lease{Holder: cfg.Identity, UpdatedAt: now, ExpiresAt: now.Add(cfg.TTL)}
	if err := writeLease(file, lease); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return Decision{}, err
	}
	acquired := &AcquiredLease{Config: cfg, File: file, Lease: lease}
	return Decision{Enabled: true, Leader: true, Identity: cfg.Identity, Holder: cfg.Identity, LeasePath: cfg.LeasePath, ExpiresAt: lease.ExpiresAt, Reason: "acquired", Lease: acquired}, nil
}

func (l *AcquiredLease) Refresh() error {
	if l == nil || l.File == nil {
		return nil
	}
	now := time.Now().UTC()
	lease := Lease{Holder: l.Config.Identity, UpdatedAt: now, ExpiresAt: now.Add(l.Config.TTL)}
	if err := writeLease(l.File, lease); err != nil {
		return err
	}
	l.Lease = lease
	return nil
}

func (l *AcquiredLease) Close() error {
	if l == nil || l.File == nil {
		return nil
	}
	err := syscall.Flock(int(l.File.Fd()), syscall.LOCK_UN)
	if closeErr := l.File.Close(); err == nil {
		err = closeErr
	}
	l.File = nil
	return err
}

func (l *AcquiredLease) Heartbeat(ctx context.Context, onError func(error)) {
	if l == nil || l.File == nil {
		return
	}
	interval := l.Config.TTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := l.Refresh(); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}

func normalizeConfig(cfg Config) Config {
	if cfg.TTL <= 0 {
		cfg.TTL = 30 * time.Second
	}
	cfg.Identity = strings.TrimSpace(cfg.Identity)
	if cfg.Identity == "" {
		cfg.Identity, _ = os.Hostname()
	}
	cfg.Identity = strings.TrimSpace(cfg.Identity)
	cfg.LeasePath = strings.TrimSpace(cfg.LeasePath)
	return cfg
}

func readLease(file *os.File) Lease {
	_, _ = file.Seek(0, 0)
	var lease Lease
	_ = json.NewDecoder(file).Decode(&lease)
	return lease
}

func writeLease(file *os.File, lease Lease) error {
	data, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Sync()
}
