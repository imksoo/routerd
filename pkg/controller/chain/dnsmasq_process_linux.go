// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/imksoo/routerd/pkg/platform"
	"golang.org/x/sys/unix"
)

func checkNativeDnsmasqDescriptorView() error { return nil }

func dnsmasqLinuxProcLocalPID(base string) (int, error) {
	ours, err := os.Stat("/proc/self/ns/pid")
	if err != nil {
		return 0, err
	}
	theirs, err := os.Stat(filepath.Join(base, "ns/pid"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !os.SameFile(ours, theirs) {
		return 0, nil
	}
	data, err := readDnsmasqFile(filepath.Join(base, "status"), 64<<10)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "NSpid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, errors.New("invalid dnsmasq native PID namespace mapping")
		}
		pid, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil || pid < 1 {
			return 0, errors.New("invalid dnsmasq namespace PID")
		}
		return pid, nil
	}
	return 0, errors.New("dnsmasq native process has no PID namespace identity")
}

func dnsmasqLinuxProcPath(fd int) (string, error) {
	data, err := readDnsmasqFile(filepath.Join("/proc/self/fdinfo", strconv.Itoa(fd)), 4096)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "Pid:") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pid:")))
		if err != nil {
			return "", err
		}
		if pid < 1 {
			if dnsmasqLinuxProcessExited(fd) {
				return "", nil
			}
			return "", errors.New("live pidfd has no procfs identity")
		}
		return filepath.Join("/proc", strconv.Itoa(pid)), nil
	}
	return "", errors.New("pidfd has no procfs PID mapping")
}

func dnsmasqProcessScope(ctx context.Context) (string, error) {
	scope, err := platform.KernelNetworkScope(ctx)
	if err != nil {
		return "", err
	}
	var namespace unix.Stat_t
	if err := unix.Stat("/proc/self/ns/pid", &namespace); err != nil {
		return "", err
	}
	return scope + ":pidns:" + strconv.FormatUint(namespace.Ino, 10), nil
}

func dnsmasqLinuxProcessExited(fd int) bool {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	_, err := unix.Poll(fds, 0)
	return err == nil && fds[0].Revents&unix.POLLIN != 0
}

func dnsmasqLinuxBirth(data []byte) (string, bool, error) {
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", false, errors.New("invalid dnsmasq native process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return "", false, errors.New("incomplete dnsmasq native process stat")
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return "", false, nil
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", false, err
	}
	return fields[19], true, nil
}

func nativeDnsmasqProcessExited(ctx context.Context, expected dnsmasqNativeProcess) (bool, error) {
	scope, err := dnsmasqProcessScope(ctx)
	if err != nil {
		return false, err
	}
	if scope != expected.Scope {
		return false, errors.New("dnsmasq process scope changed while stopping")
	}
	fd, err := unix.PidfdOpen(expected.PID, 0)
	if errors.Is(err, unix.ESRCH) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)
	if dnsmasqLinuxProcessExited(fd) {
		return true, nil
	}
	base, err := dnsmasqLinuxProcPath(fd)
	if err != nil {
		return false, err
	}
	if base == "" {
		return dnsmasqLinuxProcessExited(fd), nil
	}
	data, err := readDnsmasqFile(filepath.Join(base, "stat"), 16<<10)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return dnsmasqLinuxProcessExited(fd), nil
	}
	if err != nil {
		return false, err
	}
	birth, alive, err := dnsmasqLinuxBirth(data)
	return err == nil && (!alive || birth != expected.Birth), err
}

func readNativeDnsmasqProcess(ctx context.Context, pid int) (dnsmasqNativeProcess, error) {
	var absent dnsmasqNativeProcess
	if err := ctx.Err(); err != nil {
		return absent, err
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return absent, nil
	}
	if err != nil {
		return absent, err
	}
	defer unix.Close(fd)
	base, err := dnsmasqLinuxProcPath(fd)
	if err != nil {
		return absent, err
	}
	if base == "" {
		if dnsmasqLinuxProcessExited(fd) {
			return absent, nil
		}
		return absent, errors.New("live dnsmasq PID is not observable in procfs")
	}
	stat, err := readDnsmasqFile(filepath.Join(base, "stat"), 16<<10)
	if (errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)) && dnsmasqLinuxProcessExited(fd) {
		return absent, nil
	}
	if err != nil {
		return absent, err
	}
	birth, alive, err := dnsmasqLinuxBirth(stat)
	if err != nil || !alive {
		return absent, err
	}
	args, err := readDnsmasqFile(filepath.Join(base, "cmdline"), 64<<10)
	if errors.Is(err, os.ErrNotExist) && dnsmasqLinuxProcessExited(fd) {
		return absent, nil
	}
	if err != nil {
		return absent, err
	}
	executable, err := os.Readlink(filepath.Join(base, "exe"))
	if errors.Is(err, os.ErrNotExist) && dnsmasqLinuxProcessExited(fd) {
		return absent, nil
	}
	if err != nil {
		return absent, err
	}
	scope, err := dnsmasqProcessScope(ctx)
	if err != nil {
		return absent, err
	}
	ours, err := os.Stat("/proc/self/ns/net")
	if err != nil {
		return absent, err
	}
	theirs, err := os.Stat(filepath.Join(base, "ns/net"))
	if errors.Is(err, os.ErrNotExist) && dnsmasqLinuxProcessExited(fd) {
		return absent, nil
	}
	if err != nil {
		return absent, err
	}
	if !os.SameFile(ours, theirs) {
		scope = ""
	}
	ourPIDNS, err := os.Stat("/proc/self/ns/pid")
	if err != nil {
		return absent, err
	}
	theirPIDNS, err := os.Stat(filepath.Join(base, "ns/pid"))
	if errors.Is(err, os.ErrNotExist) && dnsmasqLinuxProcessExited(fd) {
		return absent, nil
	}
	if err != nil {
		return absent, err
	}
	if !os.SameFile(ourPIDNS, theirPIDNS) {
		scope = ""
	}

	after, err := readDnsmasqFile(filepath.Join(base, "stat"), 16<<10)
	if errors.Is(err, os.ErrNotExist) && dnsmasqLinuxProcessExited(fd) {
		return absent, nil
	}
	if err != nil {
		return absent, err
	}
	second, alive, err := dnsmasqLinuxBirth(after)
	if err != nil || !alive {
		return absent, err
	}
	if second != birth {
		return absent, errors.New("dnsmasq PID changed during observation")
	}
	if len(args) == 0 || args[len(args)-1] != 0 {
		return absent, errors.New("invalid dnsmasq native argument vector")
	}
	if dnsmasqLinuxProcessExited(fd) {
		return absent, nil
	}
	return dnsmasqNativeProcess{PID: pid, Birth: birth, Scope: scope, Executable: executable, Args: strings.Split(string(args[:len(args)-1]), "\x00")}, nil
}

func listNativeDnsmasqProcesses(ctx context.Context, spec dnsmasqProcessSpec) ([]dnsmasqNativeProcess, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var processes []dnsmasqNativeProcess
	want := filepath.Base(spec.Command)
	if len(want) > 15 {
		want = want[:15]
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}

		comm, err := readDnsmasqFile(filepath.Join("/proc", entry.Name(), "comm"), 256)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(comm)) != want {
			continue
		}
		executable, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if strings.TrimSuffix(executable, " (deleted)") != spec.Command {
			continue
		}
		pid, err := dnsmasqLinuxProcLocalPID(filepath.Join("/proc", entry.Name()))
		if err != nil {
			return nil, err
		}
		if pid <= 1 || pid == os.Getpid() {
			continue
		}
		process, err := readNativeDnsmasqProcess(ctx, pid)
		if err != nil {
			return nil, err
		}
		if process.PID != 0 && dnsmasqProcessMatches(process, spec) {
			processes = append(processes, process)
		}
	}
	return processes, nil
}

func signalNativeDnsmasqProcess(ctx context.Context, expected dnsmasqNativeProcess, signal syscall.Signal) error {
	fd, err := unix.PidfdOpen(expected.PID, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open dnsmasq native process handle: %w", err)
	}
	defer unix.Close(fd)
	current, err := readNativeDnsmasqProcess(ctx, expected.PID)
	if err != nil {
		return err
	}
	if current.PID == 0 {
		return nil
	}
	if !sameDnsmasqNativeProcess(current, expected) {
		return errors.New("dnsmasq identity changed before signal")
	}
	if err := unix.PidfdSendSignal(fd, unix.Signal(signal), nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		return err
	}
	return nil
}
