// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/subprocessio"
)

type dnsmasqProcessSpec struct{ Command, Config, PIDFile string }
type dnsmasqNativeProcess struct {
	PID                      int
	Birth, Scope, Executable string
	Args                     []string
}
type dnsmasqProcessOwner struct {
	Scope   string               `json:"scope"`
	Spec    dnsmasqProcessSpec   `json:"spec"`
	State   string               `json:"state"`
	Process dnsmasqNativeProcess `json:"process"`
}
type dnsmasqChild struct{ done <-chan error }

var dnsmasqChildren = map[int]dnsmasqChild{} // guarded by dnsmasqMu

func dnsmasqOwnerPath(pidFile string) string { return pidFile + ".routerd-owner.json" }

func dnsmasqSpec(command, configPath, pidFile string) (dnsmasqProcessSpec, error) {
	path, err := exec.LookPath(firstNonEmpty(command, "dnsmasq"))
	if err != nil {
		return dnsmasqProcessSpec{}, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return dnsmasqProcessSpec{}, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return dnsmasqProcessSpec{}, err
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return dnsmasqProcessSpec{}, err
	}
	pidFile, err = filepath.Abs(pidFile)
	if err != nil {
		return dnsmasqProcessSpec{}, err
	}
	return dnsmasqProcessSpec{path, configPath, pidFile}, nil
}

func readDnsmasqOwner(pidFile string) (*dnsmasqProcessOwner, error) {
	data, err := readDnsmasqFile(dnsmasqOwnerPath(pidFile), 64<<10)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var owner dnsmasqProcessOwner
	if err := json.Unmarshal(data, &owner); err != nil {
		return nil, err
	}
	if owner.Scope == "" || owner.Spec.Command == "" || owner.Spec.Config == "" || owner.Spec.PIDFile != pidFile || !filepath.IsAbs(owner.Spec.Command) || !filepath.IsAbs(owner.Spec.Config) || owner.State != "starting" && owner.State != "running" && owner.State != "stopping" || owner.Process.PID < 0 || owner.State == "running" && owner.Process.PID == 0 || owner.Process.PID > 0 && (owner.Process.Birth == "" || owner.Process.Scope != owner.Scope || owner.Process.Executable == "" || !dnsmasqProcessMatches(owner.Process, owner.Spec)) {
		return nil, errors.New("invalid dnsmasq process ownership receipt")
	}
	return &owner, nil
}

func writeDnsmasqOwner(ctx context.Context, owner dnsmasqProcessOwner) error {
	if owner.Scope == "" {
		scope, err := dnsmasqProcessScope(ctx)
		if err != nil {
			return err
		}
		owner.Scope = scope
	}
	if owner.Process.PID > 0 && owner.Process.Scope != owner.Scope {
		return errors.New("dnsmasq process belongs to another native scope")
	}

	data, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	return writeAtomicArtifact(dnsmasqOwnerPath(owner.Spec.PIDFile), append(data, '\n'), 0600)
}

func readDnsmasqFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("dnsmasq observation exceeds %d bytes", limit)
	}
	return data, nil
}

func dnsmasqProcessMatches(process dnsmasqNativeProcess, spec dnsmasqProcessSpec) bool {
	if process.Scope == "" || strings.TrimSuffix(process.Executable, " (deleted)") != spec.Command || len(process.Args) < 3 {
		return false
	}
	config, pid := 0, 0
	for i := 1; i < len(process.Args); i++ {
		arg := process.Args[i]
		switch {
		case arg == "--keep-in-foreground":
		case arg == "--conf-file":
			i++
			if i >= len(process.Args) || process.Args[i] != spec.Config {
				return false
			}
			config++
		case arg == "--pid-file":
			i++
			if i >= len(process.Args) || process.Args[i] != spec.PIDFile {
				return false
			}
			pid++
		case strings.HasPrefix(arg, "--conf-file="):
			if arg != "--conf-file="+spec.Config {
				return false
			}
			config++
		case strings.HasPrefix(arg, "--pid-file="):
			if arg != "--pid-file="+spec.PIDFile {
				return false
			}
			pid++
		default:
			return false
		}
	}
	return config == 1 && pid == 1
}

func sameDnsmasqNativeProcess(a, b dnsmasqNativeProcess) bool {
	return a.PID == b.PID && a.Birth == b.Birth && a.Scope == b.Scope && a.Executable == b.Executable && slices.Equal(a.Args, b.Args)
}

func observedDnsmasqProcess(ctx context.Context, spec dnsmasqProcessSpec, owner *dnsmasqProcessOwner) (dnsmasqNativeProcess, error) {
	var missing dnsmasqNativeProcess
	if owner != nil {
		scope, err := dnsmasqProcessScope(ctx)
		if err != nil {
			return missing, err
		}
		if scope != owner.Scope {
			return missing, errors.New("dnsmasq owner belongs to another kernel or process namespace")
		}
	}
	data, err := readDnsmasqFile(spec.PIDFile, 128)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return missing, err
	}
	if err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 1 || pid == os.Getpid() {
			return missing, errors.New("invalid dnsmasq PID file")
		}
		process, err := readNativeDnsmasqProcess(ctx, pid)
		if err != nil {
			return missing, err
		}
		if process.PID != 0 {
			if !dnsmasqProcessMatches(process, spec) {
				return missing, errors.New("dnsmasq PID file refers to a foreign process")
			}
			if owner != nil && owner.Process.PID == pid && owner.Process.Birth != "" && !sameDnsmasqNativeProcess(owner.Process, process) {
				return missing, errors.New("dnsmasq native process identity changed")
			}
			return process, nil
		}
	}
	// A starting operation can outlive its caller before publishing a PID.
	// Look only for the exact executable and private config/PID argument tuple.
	processes, err := listNativeDnsmasqProcesses(ctx, spec)
	if err != nil {
		return missing, err
	}
	if len(processes) > 1 {
		return missing, errors.New("multiple processes claim the managed dnsmasq configuration")
	}
	if len(processes) == 1 {
		return processes[0], nil
	}
	return missing, nil
}

func ensureNativeDnsmasq(ctx context.Context, spec dnsmasqProcessSpec, changed bool) error {
	data, err := readDnsmasqFile(spec.Config, subprocessio.StdoutLimit)
	if err != nil {
		return err
	}
	if !dnsmasqConfigOwned(data) {
		return errors.New("dnsmasq configuration is foreign")
	}
	input := []byte(spec.Command + "\x00" + spec.PIDFile)
	_, pending, digest, err := prepareArtifactActivation(spec.Config, data, input, false)
	if err != nil {
		return err
	}
	if changed && !pending {
		receipt, err := artifactActivationReceipt(spec.Config)
		if err != nil {
			return err
		}
		receipt.PendingDigest = digest
		if err := writeArtifactActivation(spec.Config, receipt); err != nil {
			return err
		}
		pending = true
	}
	owner, err := readDnsmasqOwner(spec.PIDFile)
	if err != nil {
		return err
	}
	if owner != nil && owner.Spec != spec {
		if err := stopNativeDnsmasq(ctx, owner.Spec); err != nil {
			return err
		}
		owner = nil
	}
	process, err := observedDnsmasqProcess(ctx, spec, owner)
	if err != nil {
		return err
	}
	if process.PID != 0 && (pending || owner == nil || owner.State != "running") {
		if err := stopNativeDnsmasq(ctx, spec); err != nil {
			return err
		}
		process = dnsmasqNativeProcess{}
	}
	if process.PID == 0 {
		receipt, err := artifactActivationReceipt(spec.Config)
		if err != nil {
			return err
		}
		receipt.PendingDigest = digest
		receipt.Removing = false
		if err := writeArtifactActivation(spec.Config, receipt); err != nil {
			return err
		}
		if err := startOwnedDnsmasq(ctx, spec); err != nil {
			return err
		}
		process, err = observedDnsmasqProcess(ctx, spec, nil)
		if err != nil {
			return err
		}
		if process.PID == 0 {
			return errors.New("dnsmasq absent after startup")
		}
	}
	current, err := readDnsmasqFile(spec.Config, subprocessio.StdoutLimit)
	if err != nil {
		return err
	}
	if artifactActivationDigest(current, input) != digest {
		return errors.New("dnsmasq configuration changed during activation")
	}
	if err := writeDnsmasqOwner(ctx, dnsmasqProcessOwner{Spec: spec, State: "running", Process: process}); err != nil {
		return err
	}
	return acknowledgeArtifactActivation(spec.Config, digest)
}

func startOwnedDnsmasq(ctx context.Context, spec dnsmasqProcessSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeDnsmasqOwner(ctx, dnsmasqProcessOwner{Spec: spec, State: "starting"}); err != nil {
		return err
	}
	// The service lifetime belongs to its durable owner, not a reconcile request.
	// An interrupted startup remains pending and is observed on the next retry.
	cmd := exec.Command(spec.Command, "--keep-in-foreground", "--conf-file="+spec.Config, "--pid-file="+spec.PIDFile)
	output := subprocessio.NewCapture(subprocessio.StderrLimit)
	cmd.Stdout = io.Discard
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
		dnsmasqMu.Lock()
		if current, exists := dnsmasqChildren[cmd.Process.Pid]; exists && current.done == done {
			delete(dnsmasqChildren, cmd.Process.Pid)
		}
		dnsmasqMu.Unlock()
	}()
	dnsmasqChildren[cmd.Process.Pid] = dnsmasqChild{done: done}
	process, err := waitStartedDnsmasq(ctx, spec, cmd.Process.Pid, done)
	if err != nil {
		return err
	}
	if err := writeDnsmasqOwner(ctx, dnsmasqProcessOwner{Spec: spec, State: "starting", Process: process}); err != nil {
		return err
	}
	if err := writeAtomicArtifact(spec.PIDFile, []byte(strconv.Itoa(process.PID)+"\n"), 0644); err != nil {
		return err
	}
	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()
	select {
	case err := <-done:
		delete(dnsmasqChildren, process.PID)
		return errors.Join(err, fmt.Errorf("dnsmasq exited during startup: %s", strings.TrimSpace(output.String())))
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	current, err := readNativeDnsmasqProcess(ctx, process.PID)
	if err != nil {
		return err
	}
	if !sameDnsmasqNativeProcess(process, current) {
		return errors.New("dnsmasq changed or exited during startup")
	}
	return nil
}

func waitStartedDnsmasq(ctx context.Context, spec dnsmasqProcessSpec, pid int, done <-chan error) (dnsmasqNativeProcess, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		process, err := readNativeDnsmasqProcess(ctx, pid)
		if err == nil && process.PID != 0 {
			if !dnsmasqProcessMatches(process, spec) {
				return process, errors.New("dnsmasq startup native identity does not match")
			}
			return process, nil
		}
		last = err
		select {
		case exited := <-done:
			return dnsmasqNativeProcess{}, errors.Join(exited, errors.New("dnsmasq exited before native identity readback"))
		case <-ctx.Done():
			return dnsmasqNativeProcess{}, errors.Join(ctx.Err(), last)
		case <-ticker.C:
		}
	}
}

func stopNativeDnsmasq(ctx context.Context, spec dnsmasqProcessSpec) error {
	owner, err := readDnsmasqOwner(spec.PIDFile)
	if err != nil {
		return err
	}
	process, err := observedDnsmasqProcess(ctx, spec, owner)
	if err != nil {
		return err
	}
	if err := beginArtifactRemoval(spec.Config); err != nil {
		return err
	}
	if err := writeDnsmasqOwner(ctx, dnsmasqProcessOwner{Spec: spec, State: "stopping", Process: process}); err != nil {
		return err
	}
	if process.PID != 0 {
		if err := signalNativeDnsmasqProcess(ctx, process, syscall.SIGTERM); err != nil {
			return err
		}
		if err := waitDnsmasqProcessExit(ctx, process, 2*time.Second); err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			if err := signalNativeDnsmasqProcess(ctx, process, syscall.SIGKILL); err != nil {
				return err
			}
			if err := waitDnsmasqProcessExit(ctx, process, 2*time.Second); err != nil {
				return err
			}
		}
		if child, ok := dnsmasqChildren[process.PID]; ok {
			select {
			case <-child.done:
				delete(dnsmasqChildren, process.PID)
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
				return errors.New("dnsmasq child has not joined after native exit")
			}
		}
	}
	if data, err := readDnsmasqFile(spec.PIDFile, 128); err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return err
		}
		if process.PID != 0 && pid != process.PID {
			return errors.New("dnsmasq PID file changed during stop")
		}
		if _, err := removeFileIfExists(spec.PIDFile, false); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := clearArtifactActivation(spec.Config); err != nil {
		return err
	}
	_, err = removeFileIfExists(dnsmasqOwnerPath(spec.PIDFile), false)
	return err
}

func waitDnsmasqProcessExit(ctx context.Context, expected dnsmasqNativeProcess, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		exited, err := nativeDnsmasqProcessExited(ctx, expected)
		if err != nil {
			return err
		}
		if exited {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func reloadOwnedDnsmasqHosts(ctx context.Context, pidFile string) error {
	dnsmasqMu.Lock()
	defer dnsmasqMu.Unlock()
	pidFile, err := filepath.Abs(pidFile)
	if err != nil {
		return err
	}
	owner, err := readDnsmasqOwner(pidFile)
	if err != nil {
		return err
	}
	if owner == nil {
		return errors.New("dnsmasq ownership is unavailable for hosts reload")
	}
	if owner.State != "running" {
		return errors.New("dnsmasq activation or stop awaits recovery")
	}
	activation, err := artifactActivationReceipt(owner.Spec.Config)
	if err != nil {
		return err
	}
	data, err := readDnsmasqFile(owner.Spec.Config, subprocessio.StdoutLimit)
	if err != nil {
		return err
	}
	if activation.PendingDigest != "" || activation.Removing || activation.AppliedDigest != artifactActivationDigest(data, []byte(owner.Spec.Command+"\x00"+owner.Spec.PIDFile)) {
		return errors.New("dnsmasq main configuration awaits activation")
	}

	process, err := observedDnsmasqProcess(ctx, owner.Spec, owner)
	if err != nil {
		return err
	}
	if process.PID == 0 {
		return errors.New("dnsmasq absent during hosts reload")
	}
	return signalNativeDnsmasqProcess(ctx, process, syscall.SIGHUP)
}

func stopOwnedDnsmasq(ctx context.Context, command, configPath, pidFile string) error {
	dnsmasqMu.Lock()
	defer dnsmasqMu.Unlock()
	var err error
	configPath, err = filepath.Abs(firstNonEmpty(configPath, "/run/routerd/dnsmasq-phase1.conf"))
	if err != nil {
		return err
	}
	pidFile, err = filepath.Abs(firstNonEmpty(pidFile, "/run/routerd/dnsmasq-phase1.pid"))
	if err != nil {
		return err
	}
	owner, err := readDnsmasqOwner(pidFile)
	if err != nil {
		return err
	}
	config, configErr := readDnsmasqFile(configPath, subprocessio.StdoutLimit)
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		return configErr
	}
	if configErr == nil && !dnsmasqConfigOwned(config) {
		return errors.New("refuse to stop dnsmasq for a foreign configuration")
	}
	defaults, features := platform.Current()
	if defaults.OS == platform.OSFreeBSD && filepath.Clean(configPath) == filepath.Clean(defaults.DnsmasqConfigFile) && defaults.DnsmasqServiceFile != "" {
		if _, err := os.Lstat(defaults.DnsmasqServiceFile); err == nil {
			return errors.New("dnsmasq rc boot creator conflicts with the native process owner")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if features.HasSystemd {
		if err := stopSystemdDnsmasq(ctx, filepath.Join(defaults.SystemdSystemDir, "routerd-dnsmasq.service"), configPath, configErr == nil || owner != nil); err != nil {
			return err
		}
		owner, err = readDnsmasqOwner(pidFile)
		if err != nil {
			return err
		}
	}
	if owner != nil {
		return stopNativeDnsmasq(ctx, owner.Spec)
	}
	if errors.Is(configErr, os.ErrNotExist) {
		if _, err := os.Lstat(pidFile); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		return errors.New("dnsmasq PID exists without its configuration or owner receipt")
	}
	if features.HasSystemd {
		return nil
	}
	spec, err := dnsmasqSpec(command, configPath, pidFile)
	if err != nil {
		return err
	}
	return stopNativeDnsmasq(ctx, spec)
}

func dnsmasqSystemctl(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	out, stderr := subprocessio.NewCapture(subprocessio.StdoutLimit), subprocessio.NewCapture(subprocessio.StderrLimit)
	cmd.Stdout, cmd.Stderr = out, stderr
	err := subprocessio.Run(cmd)
	if limitErr := subprocessio.LimitError(out, stderr); limitErr != nil {
		return nil, errors.Join(err, limitErr)
	}
	if err != nil {
		return out.Bytes(), fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}
