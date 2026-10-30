// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/ipsec"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/subprocessio"
)

const freeBSDStrongSwanStartupTimeout = 15 * time.Second
const freeBSDStrongSwanLoadTimeout = 20 * time.Second
const freeBSDStrongSwanProbeTimeout = 2 * time.Second

func applyIPsecConnections(ctx context.Context, router *api.Router) error {
	directory := ipsecConfigDir()
	needed, err := ipsec.Needed(router, directory)
	if err != nil || !needed {
		return err
	}
	operation, cancel := context.WithTimeout(ctx, freeBSDStrongSwanLoadTimeout)
	defer cancel()
	if err := ensureFreeBSDStrongSwan(operation); err != nil {
		return err
	}
	session, err := ipsec.OpenSession(operation, ipsecVICISocketPath())
	if err != nil {
		return fmt.Errorf("connect to IPsec daemon: %w", err)
	}
	defer session.Close()
	return (ipsec.Controller{Session: session, Directory: directory}).Reconcile(operation, router)
}

func ensureFreeBSDStrongSwan(ctx context.Context) error {
	if platformDefaults.OS != platform.OSFreeBSD {
		return nil
	}
	moduleCtx, cancelModule := context.WithTimeout(ctx, freeBSDStrongSwanStartupTimeout)
	defer cancelModule()
	if err := ensureFreeBSDIPsecKernel(moduleCtx); err != nil {
		return err
	}
	serviceCtx, cancel := context.WithTimeout(ctx, freeBSDStrongSwanStartupTimeout)
	defer cancel()
	statusOutput, statusErr := runIPsecCommand(serviceCtx, "service", "strongswan", "status")
	if statusErr == nil {
		if output, err := runIPsecCommand(serviceCtx, "sysrc", "strongswan_enable=YES"); err != nil {
			return fmt.Errorf("enable strongswan service: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	if err := serviceCtx.Err(); err != nil {
		return err
	}
	var exitError *exec.ExitError
	if !errors.As(statusErr, &exitError) {
		return fmt.Errorf("strongswan provider status is unknown: %w: %s", statusErr, strings.TrimSpace(string(statusOutput)))
	}
	// The package's VICI status command is swanctl --stats. Its failure does
	// not establish that charon is stopped. Existing or unreadable endpoints
	// retain their owner; never unlink them or launch a second provider.
	for _, path := range []string{ipsecVICISocketPath(), filepath.Join(filepath.Dir(platformDefaults.RuntimeDir), "charon.pid")} {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("strongswan provider status is unknown while %s exists: %w", path, statusErr)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect strongswan provider ownership: %w", err)
		}
	}
	if output, err := runIPsecCommand(serviceCtx, "sysrc", "strongswan_enable=YES"); err != nil {
		return fmt.Errorf("enable strongswan service: %w: %s", err, strings.TrimSpace(string(output)))
	}
	// FreeBSD daemon acquires this package pidfile lease before daemonizing.
	// The OS supervisor retains it for the provider lifetime. A competing
	// package/routerd start cannot create another supervisor under that lease.
	// Calling the rc start hook would also perform a destructive global load-all.
	output, err := runIPsecCommand(serviceCtx, "/usr/sbin/daemon", "-f", "-S", "-P", filepath.Join(filepath.Dir(platformDefaults.RuntimeDir), "daemon-charon.pid"), "/usr/local/libexec/ipsec/charon", "--use-syslog")
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 3 {
			return fmt.Errorf("strongswan provider startup outcome is unconfirmed: %w: %s", err, strings.TrimSpace(string(output)))
		}
		// Exit 3 identifies another holder of daemon's pidfile. Only a real
		// readiness response may acknowledge that concurrently started owner.
	}
	if err := waitForFreeBSDStrongSwanVICI(serviceCtx); err != nil {
		return fmt.Errorf("strongswan provider startup outcome is unconfirmed: %w", err)
	}
	return nil
}

func ensureFreeBSDIPsecKernel(ctx context.Context) error {
	out, err := runIPsecCommand(ctx, "kldload", "-n", "ipsec")
	if err != nil {
		return fmt.Errorf("load FreeBSD IPsec kernel module: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func waitForFreeBSDStrongSwanVICI(ctx context.Context) error {
	var lastErr error
	for {
		probeCtx, cancel := context.WithTimeout(ctx, freeBSDStrongSwanProbeTimeout)
		_, err := runIPsecCommand(probeCtx, ipsecSwanctlPath(), "--stats")
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for strongswan VICI: %w: %v", ctx.Err(), lastErr)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func runIPsecCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	stdout, stderr := subprocessio.NewCapture(subprocessio.StdoutLimit), subprocessio.NewCapture(subprocessio.StderrLimit)
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout, command.Stderr = stdout, stderr
	err := subprocessio.Run(command)
	err = errors.Join(err, subprocessio.LimitError(stdout, stderr), ctx.Err())
	output := append([]byte(nil), stdout.Bytes()...)
	output = append(output, stderr.Bytes()...)
	return output, err
}

func ipsecVICISocketPath() string {
	return filepath.Join(filepath.Dir(platformDefaults.RuntimeDir), "charon.vici")
}

func ipsecConfigDir() string {
	return filepath.Join(platformDefaults.SysconfDir, "swanctl")
}

func ipsecSwanctlPath() string {
	if platformDefaults.OS == platform.OSFreeBSD {
		return "/usr/local/sbin/swanctl"
	}
	return "swanctl"
}
