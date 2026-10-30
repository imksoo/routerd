// SPDX-License-Identifier: BSD-3-Clause

package ha

import (
	"errors"
	"os"
)

// OwnsPath observes the actual held descriptor and the configured pathname.
// A cached Leader flag cannot authorize a replaced file or a closed owner.
func (l *AcquiredLease) OwnsPath(path string) (bool, error) {
	if l == nil {
		return false, ErrLeaseClosed
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ownsPathLocked(path)
}
func (l *AcquiredLease) ownsPathLocked(path string) (bool, error) {
	if l.File == nil {
		return false, ErrLeaseClosed
	}
	held, err := l.File.Stat()
	if err != nil {
		return false, err
	}
	current, err := os.Stat(l.Config.LeasePath)
	if err != nil {
		return false, err
	}
	if !os.SameFile(held, current) {
		return false, errors.New("held cluster lease pathname was replaced")
	}
	target, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(held, target), nil
}

// Transfer moves the same locked open file description to its next concrete
// generation owner. No unlock/reacquire gap or mutable shared Config is created.
// The recipient must Refresh after committed configuration changes and Close
// only after its own effects have stopped.
func (l *AcquiredLease) Transfer(cfg Config) (*AcquiredLease, error) {
	if l == nil {
		return nil, ErrLeaseClosed
	}
	cfg = normalizeConfig(cfg)
	l.mu.Lock()
	defer l.mu.Unlock()
	same, err := l.ownsPathLocked(cfg.LeasePath)
	if err != nil {
		return nil, err
	}
	if !same {
		return nil, errors.New("cluster lease transfer identifies another file")
	}
	next := &AcquiredLease{Config: cfg, File: l.File, Lease: l.Lease}
	l.File = nil
	return next, nil
}
