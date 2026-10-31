//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Notification is a best-effort fast path; bootstrap and periodic role
// observation remain authoritative while the router service is not ready.
func notifyRouterdVRRPTransition(command commandRunner) error {
	pid, err := readyRouterdMainPID(command)
	if err != nil || pid == 0 {
		return err
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return fmt.Errorf("open ready routerd process: %w", err)
	}
	defer unix.Close(fd)
	current, err := readyRouterdMainPID(command)
	if err != nil {
		return err
	}
	if current != pid {
		return errors.New("routerd readiness or main process changed before notification")
	}
	// Bind the signal to the ready process, not a service name that can resolve
	// to a new activating process after a concurrent restart.
	if err := unix.PidfdSendSignal(fd, unix.SIGUSR1, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("notify ready routerd process: %w", err)
	}
	return nil
}

func readyRouterdMainPID(command commandRunner) (int, error) {
	out, err := command("systemctl", "show", "--property=ActiveState", "--property=SubState", "--property=MainPID", "routerd.service")
	if err != nil {
		return 0, fmt.Errorf("observe routerd notification readiness: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var state, substate, pidText string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			return 0, errors.New("incomplete routerd notification readiness")
		}
		switch key {
		case "ActiveState":
			if state != "" {
				return 0, errors.New("duplicate routerd active state")
			}
			state = value
		case "SubState":
			if substate != "" {
				return 0, errors.New("duplicate routerd substate")
			}
			substate = value
		case "MainPID":
			if pidText != "" {
				return 0, errors.New("duplicate routerd main PID")
			}
			pidText = value
		default:
			return 0, errors.New("unexpected routerd notification readiness field")
		}
	}
	pid, err := strconv.Atoi(pidText)
	if state == "" || substate == "" || err != nil || pid < 0 || pid > 2147483647 {
		return 0, errors.New("invalid routerd notification readiness")
	}
	if state != "active" || substate != "running" {
		return 0, nil
	}
	if pid <= 1 {
		return 0, errors.New("ready routerd service has no valid main PID")
	}
	return pid, nil
}
