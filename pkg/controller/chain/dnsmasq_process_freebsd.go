// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/subprocessio"
	"golang.org/x/sys/unix"
)

func dnsmasqFreeBSDPS(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/ps", args...)
	command.Env = append(os.Environ(), "LC_ALL=C")
	out, stderr := subprocessio.NewCapture(subprocessio.StdoutLimit), subprocessio.NewCapture(subprocessio.StderrLimit)
	command.Stdout, command.Stderr = out, stderr
	err := subprocessio.Run(command)
	if limitErr := subprocessio.LimitError(out, stderr); limitErr != nil {
		return nil, errors.Join(err, limitErr)
	}
	if err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) && exited.ExitCode() == 1 && len(out.Bytes()) == 0 && len(stderr.Bytes()) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("observe dnsmasq native process: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}

func dnsmasqFreeBSDIdentity(ctx context.Context, pid int) (birth, jail string, exists bool, err error) {
	out, err := dnsmasqFreeBSDPS(ctx, "-ww", "-p", strconv.Itoa(pid), "-o", "pid=", "-o", "state=", "-o", "lstart=", "-o", "jid=")
	if err != nil {
		return "", "", false, err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", "", false, nil
	}
	if len(fields) != 8 || fields[0] != strconv.Itoa(pid) {
		return "", "", false, errors.New("invalid FreeBSD dnsmasq process identity")
	}
	if strings.HasPrefix(fields[1], "Z") {
		return "", "", false, nil
	}
	if _, err := strconv.ParseUint(fields[7], 10, 32); err != nil {
		return "", "", false, err
	}
	birth = strings.Join(fields[2:7], " ")
	if _, err := time.Parse("Mon Jan 2 15:04:05 2006", birth); err != nil {
		return "", "", false, err
	}
	return birth, fields[7], true, nil
}

func readNativeDnsmasqProcess(ctx context.Context, pid int) (dnsmasqNativeProcess, error) {
	var missing dnsmasqNativeProcess
	birth, jail, exists, err := dnsmasqFreeBSDIdentity(ctx, pid)
	if err != nil || !exists {
		return missing, err
	}
	args, err := unix.SysctlRaw("kern.proc.args", pid)
	if errors.Is(err, unix.ESRCH) {
		return missing, nil
	}
	if err != nil {
		return missing, err
	}
	path, err := unix.SysctlRaw("kern.proc.pathname", pid)
	if errors.Is(err, unix.ESRCH) {
		return missing, nil
	}
	if err != nil {
		return missing, err
	}
	if len(args) == 0 || len(args) > 64<<10 || len(path) == 0 {
		return missing, errors.New("incomplete FreeBSD dnsmasq native command")
	}
	scope, err := dnsmasqProcessScope(ctx)
	if err != nil {
		return missing, err
	}
	_, ourJail, _, err := dnsmasqFreeBSDIdentity(ctx, os.Getpid())
	if err != nil {
		return missing, err
	}
	if jail != ourJail {
		scope = ""
	}
	after, afterJail, exists, err := dnsmasqFreeBSDIdentity(ctx, pid)
	if err != nil || !exists {
		return missing, err
	}
	if birth != after || jail != afterJail {
		return missing, errors.New("FreeBSD dnsmasq process changed during observation")
	}
	return dnsmasqNativeProcess{PID: pid, Birth: birth, Scope: scope, Executable: strings.TrimRight(string(path), "\x00"), Args: strings.Split(strings.TrimRight(string(args), "\x00"), "\x00")}, nil
}

func nativeDnsmasqProcessExited(ctx context.Context, expected dnsmasqNativeProcess) (bool, error) {
	scope, err := dnsmasqProcessScope(ctx)
	if err != nil {
		return false, err
	}
	if scope != expected.Scope {
		return false, errors.New("dnsmasq process scope changed while stopping")
	}
	birth, _, exists, err := dnsmasqFreeBSDIdentity(ctx, expected.PID)
	return err == nil && (!exists || birth != expected.Birth), err
}

func listNativeDnsmasqProcesses(ctx context.Context, spec dnsmasqProcessSpec) ([]dnsmasqNativeProcess, error) {
	out, err := dnsmasqFreeBSDPS(ctx, "-axww", "-o", "pid=", "-o", "comm=")
	if err != nil {
		return nil, err
	}
	var found []dnsmasqNativeProcess
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 2 {
			return nil, errors.New("incomplete FreeBSD process list")
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid < 1 {
			return nil, errors.New("invalid FreeBSD process PID")
		}
		if pid == os.Getpid() {
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		if filepath.Base(command) != filepath.Base(spec.Command) {
			continue
		}
		process, err := readNativeDnsmasqProcess(ctx, pid)
		if err != nil {
			return nil, err
		}
		if process.PID != 0 && dnsmasqProcessMatches(process, spec) {
			found = append(found, process)
		}
	}
	return found, nil
}

func signalNativeDnsmasqProcess(ctx context.Context, expected dnsmasqNativeProcess, signal syscall.Signal) error {
	current, err := readNativeDnsmasqProcess(ctx, expected.PID)
	if err != nil {
		return err
	}
	if current.PID == 0 {
		return nil
	}
	if !sameDnsmasqNativeProcess(current, expected) {
		return errors.New("FreeBSD dnsmasq identity changed before signal")
	}
	process, err := os.FindProcess(expected.PID)
	if err != nil {
		return err
	}
	if err := process.Signal(signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func dnsmasqProcessScope(ctx context.Context) (string, error) {
	return platform.KernelNetworkScope(ctx)
}
