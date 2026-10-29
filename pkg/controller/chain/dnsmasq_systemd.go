// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/subprocessio"
)

const dnsmasqUnitName = "routerd-dnsmasq.service"

// Only the current generated unit grants process authority. In particular, a
// similarly named unit or another manager fragment must never grant stop rights.
func dnsmasqUnitSpec(data []byte) (dnsmasqProcessSpec, error) {
	var spec dnsmasqProcessSpec
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		args := strings.Fields(strings.TrimPrefix(line, "ExecStart="))
		if len(args) != 4 || args[1] != "--keep-in-foreground" || !strings.HasPrefix(args[2], "--conf-file=") || !strings.HasPrefix(args[3], "--pid-file=") {
			return spec, errors.New("invalid managed dnsmasq unit command")
		}
		spec = dnsmasqProcessSpec{args[0], strings.TrimPrefix(args[2], "--conf-file="), strings.TrimPrefix(args[3], "--pid-file=")}
	}
	if !filepath.IsAbs(spec.Command) || !filepath.IsAbs(spec.Config) || !filepath.IsAbs(spec.PIDFile) || !bytes.Equal(data, render.DnsmasqServiceUnitWithPID(spec.Config, spec.PIDFile, spec.Command)) {
		return spec, errors.New("dnsmasq systemd unit is foreign or malformed")
	}
	return spec, nil
}

func dnsmasqUnitProperty(ctx context.Context, property string) (string, error) {
	out, err := dnsmasqSystemctl(ctx, "show", "--property="+property, "--value", dnsmasqUnitName)
	return strings.TrimSpace(string(out)), err
}

func verifyDnsmasqUnitFragment(ctx context.Context, unitPath string) error {
	fragment, err := dnsmasqUnitProperty(ctx, "FragmentPath")
	if err != nil {
		return err
	}
	if fragment != unitPath {
		return fmt.Errorf("dnsmasq service uses a foreign or unknown manager fragment %q", fragment)
	}
	dropIns, err := dnsmasqUnitProperty(ctx, "DropInPaths")
	if err != nil {
		return err
	}
	if dropIns != "" {
		return errors.New("dnsmasq service has foreign manager overrides")
	}
	return nil
}

func systemdDnsmasqProcess(ctx context.Context, spec dnsmasqProcessSpec) (dnsmasqNativeProcess, error) {
	var missing dnsmasqNativeProcess
	pidValue, err := dnsmasqUnitProperty(ctx, "MainPID")
	if err != nil {
		return missing, err
	}
	pid, err := strconv.Atoi(pidValue)
	if err != nil || pid <= 1 || pid == os.Getpid() {
		return missing, errors.New("dnsmasq service has no valid native main process")
	}
	process, err := readNativeDnsmasqProcess(ctx, pid)
	if err != nil {
		return missing, err
	}
	if !dnsmasqProcessMatches(process, spec) {
		return missing, errors.New("dnsmasq service main process is absent, foreign, or unknown")
	}
	return process, nil
}

func ensureSystemdDnsmasqService(ctx context.Context, systemdDir, command, configPath, pidFile string, changed bool) error {
	spec, err := dnsmasqSpec(command, configPath, pidFile)
	if err != nil {
		return err
	}
	config, err := readDnsmasqFile(spec.Config, subprocessio.StdoutLimit)
	if err != nil {
		return err
	}
	if !dnsmasqConfigOwned(config) {
		return errors.New("dnsmasq configuration is foreign")
	}
	unitPath := filepath.Join(firstNonEmpty(systemdDir, "/etc/systemd/system"), dnsmasqUnitName)
	if old, err := readDnsmasqFile(unitPath, subprocessio.StdoutLimit); err == nil {
		if _, err := dnsmasqUnitSpec(old); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	unit := render.DnsmasqServiceUnitWithPID(spec.Config, spec.PIDFile, spec.Command)
	_, pending, digest, err := prepareArtifactActivation(unitPath, unit, config, false)
	if err != nil {
		return err
	}
	owner, err := readDnsmasqOwner(spec.PIDFile)
	if err != nil {
		return err
	}
	if owner != nil {
		scope, err := dnsmasqProcessScope(ctx)
		if err != nil {
			return err
		}
		if owner.Scope != scope || owner.Spec != spec {
			return errors.New("dnsmasq process receipt belongs to another native scope or configuration")
		}
	}
	if changed && !pending {
		receipt, err := artifactActivationReceipt(unitPath)
		if err != nil {
			return err
		}
		receipt.PendingDigest = digest
		if err := writeArtifactActivation(unitPath, receipt); err != nil {
			return err
		}
		pending = true
	}
	if pending {
		if _, err := dnsmasqSystemctl(ctx, "daemon-reload"); err != nil {
			return err
		}
	}
	if err := verifyDnsmasqUnitFragment(ctx, unitPath); err != nil {
		return err
	}
	enabled, err := dnsmasqUnitProperty(ctx, "UnitFileState")
	if err != nil {
		return err
	}
	switch enabled {
	case "enabled":
	case "disabled":
		if err := markDnsmasqActivationPending(unitPath, digest); err != nil {
			return err
		}
		pending = true
		if _, err := dnsmasqSystemctl(ctx, "enable", dnsmasqUnitName); err != nil {
			return err
		}
	default:
		return fmt.Errorf("dnsmasq service enablement is unknown or unsupported: %q", enabled)
	}
	active, err := dnsmasqUnitProperty(ctx, "ActiveState")
	if err != nil {
		return err
	}
	var previous dnsmasqNativeProcess
	switch active {
	case "active":
		previous, err = systemdDnsmasqProcess(ctx, spec)
		if err != nil {
			return err
		}
	case "inactive", "failed":
	default:
		return fmt.Errorf("dnsmasq service state is unknown or transitional: %q", active)
	}
	if pending || active != "active" {
		if err := markDnsmasqActivationPending(unitPath, digest); err != nil {
			return err
		}
		if err := writeDnsmasqOwner(ctx, dnsmasqProcessOwner{Spec: spec, State: "starting", Process: previous}); err != nil {
			return err
		}
		if active == "failed" {
			if _, err := dnsmasqSystemctl(ctx, "reset-failed", dnsmasqUnitName); err != nil {
				return err
			}
		}
		if _, err := dnsmasqSystemctl(ctx, "restart", dnsmasqUnitName); err != nil {
			return err
		}
		if previous.PID != 0 {
			if err := waitDnsmasqProcessExit(ctx, previous, 2*time.Second); err != nil {
				return err
			}
		}
		active, err = dnsmasqUnitProperty(ctx, "ActiveState")
		if err != nil {
			return err
		}
		if active != "active" {
			return fmt.Errorf("dnsmasq service restart not acknowledged: %q", active)
		}
		previous, err = systemdDnsmasqProcess(ctx, spec)
		if err != nil {
			return err
		}
	}
	current, err := readDnsmasqFile(spec.Config, subprocessio.StdoutLimit)
	if err != nil {
		return err
	}
	actualUnit, err := readDnsmasqFile(unitPath, subprocessio.StdoutLimit)
	if err != nil {
		return err
	}
	if artifactActivationDigest(actualUnit, current) != digest {
		return errors.New("dnsmasq configuration or unit changed during activation")
	}
	if err := writeDnsmasqOwner(ctx, dnsmasqProcessOwner{Spec: spec, State: "running", Process: previous}); err != nil {
		return err
	}
	return acknowledgeArtifactActivation(unitPath, digest)
}

func markDnsmasqActivationPending(path, digest string) error {
	receipt, err := artifactActivationReceipt(path)
	if err != nil {
		return err
	}
	receipt.PendingDigest, receipt.Removing = digest, false
	return writeArtifactActivation(path, receipt)
}

func reloadSystemdDnsmasq(ctx context.Context, unitPath, configPath, pidFile string) error {
	dnsmasqMu.Lock()
	defer dnsmasqMu.Unlock()
	var err error
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return err
	}
	pidFile, err = filepath.Abs(pidFile)
	if err != nil {
		return err
	}
	unit, err := readDnsmasqFile(unitPath, subprocessio.StdoutLimit)
	if err != nil {
		return err
	}
	spec, err := dnsmasqUnitSpec(unit)
	if err != nil {
		return err
	}
	if spec.Config != configPath || spec.PIDFile != pidFile {
		return errors.New("dnsmasq hosts reload targets a different service configuration")
	}
	config, err := readDnsmasqFile(configPath, subprocessio.StdoutLimit)
	if err != nil {
		return err
	}
	receipt, err := artifactActivationReceipt(unitPath)
	if err != nil {
		return err
	}
	if receipt.PendingDigest != "" || receipt.Removing || receipt.AppliedDigest != artifactActivationDigest(unit, config) {
		return errors.New("dnsmasq main configuration awaits activation")
	}
	if err := verifyDnsmasqUnitFragment(ctx, unitPath); err != nil {
		return err
	}
	if _, err := systemdDnsmasqProcess(ctx, spec); err != nil {
		return err
	}
	_, err = dnsmasqSystemctl(ctx, "reload", dnsmasqUnitName)
	return err
}

func stopSystemdDnsmasq(ctx context.Context, unitPath, configPath string, previouslyManaged bool) error {
	data, err := readDnsmasqFile(unitPath, subprocessio.StdoutLimit)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	receipt, receiptErr := artifactActivationReceipt(unitPath)
	if receiptErr != nil {
		return receiptErr
	}
	if errors.Is(err, os.ErrNotExist) {
		if !previouslyManaged && receipt.AppliedDigest == "" && receipt.PendingDigest == "" && !receipt.Removing {
			return nil
		}
		state, err := dnsmasqUnitProperty(ctx, "ActiveState")
		if err != nil {
			return err
		}
		if state != "inactive" && state != "failed" {
			return errors.New("dnsmasq service may still be active without an ownership artifact")
		}
		if _, err := dnsmasqSystemctl(ctx, "daemon-reload"); err != nil {
			return err
		}
		return clearArtifactActivation(unitPath)
	}
	spec, err := dnsmasqUnitSpec(data)
	if err != nil {
		return err
	}
	if spec.Config != configPath {
		return errors.New("dnsmasq service belongs to another configuration")
	}
	if err := verifyDnsmasqUnitFragment(ctx, unitPath); err != nil {
		return err
	}
	active, err := dnsmasqUnitProperty(ctx, "ActiveState")
	if err != nil {
		return err
	}
	switch active {
	case "active":
		if _, err := systemdDnsmasqProcess(ctx, spec); err != nil {
			return err
		}
	case "inactive", "failed":
	default:
		return fmt.Errorf("dnsmasq service state is unknown or transitional: %q", active)
	}
	owner, err := readDnsmasqOwner(spec.PIDFile)
	if err != nil {
		return err
	}
	if owner != nil && owner.Spec != spec {
		return errors.New("dnsmasq stop receipt belongs to another configuration")
	}
	process, err := observedDnsmasqProcess(ctx, spec, owner)
	if err != nil {
		return err
	}
	if err := beginArtifactRemoval(unitPath); err != nil {
		return err
	}
	if err := writeDnsmasqOwner(ctx, dnsmasqProcessOwner{Spec: spec, State: "stopping", Process: process}); err != nil {
		return err
	}
	if _, err := dnsmasqSystemctl(ctx, "stop", dnsmasqUnitName); err != nil {
		return err
	}
	state, err := dnsmasqUnitProperty(ctx, "ActiveState")
	if err != nil {
		return err
	}
	if state != "inactive" && state != "failed" {
		return fmt.Errorf("dnsmasq systemd stop not acknowledged: %q", state)
	}
	// After the manager has acknowledged inactive, retire any still-live exact
	// process using the same native receipt as the foreground owner. This also
	// keeps a failed stop recoverable when the manager later reports inactive.
	if err := stopNativeDnsmasq(ctx, spec); err != nil {
		return err
	}
	if _, err := dnsmasqSystemctl(ctx, "disable", dnsmasqUnitName); err != nil {
		return err
	}
	if _, err := removeFileIfExists(unitPath, false); err != nil {
		return err
	}
	if _, err := dnsmasqSystemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	return clearArtifactActivation(unitPath)
}

func testDnsmasqConfig(ctx context.Context, command, configPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, "--test", "--conf-file="+configPath)
	out, stderr := subprocessio.NewCapture(subprocessio.StdoutLimit), subprocessio.NewCapture(subprocessio.StderrLimit)
	cmd.Stdout, cmd.Stderr = out, stderr
	err := subprocessio.Run(cmd)
	if limitErr := subprocessio.LimitError(out, stderr); limitErr != nil {
		return errors.Join(err, limitErr)
	}
	if err != nil {
		return fmt.Errorf("%s --test --conf-file=%s: %w: %s", command, configPath, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
