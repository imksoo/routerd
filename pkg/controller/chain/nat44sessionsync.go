// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/netip"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

const defaultNAT44SessionSyncCommandTimeout = 2 * time.Minute
const defaultNAT44SessionSyncEventBatchInterval = 5 * time.Second
const defaultNAT44SessionSyncEventBatchMax = 512

type sessionSyncCommandFunc func(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error)
type sessionSyncStreamFunc func(ctx context.Context, name string, args []string) (io.ReadCloser, func() error, error)

type NAT44SessionSyncController struct {
	Router       *api.Router
	Store        Store
	DryRun       bool
	Command      sessionSyncCommandFunc
	EventCommand sessionSyncStreamFunc
	Workers      *nat44SessionSyncWorkerManager
	Now          func() time.Time
}

type nat44SessionSyncJob struct {
	APIVersion       string
	Kind             string
	Name             string
	Mode             string
	Interval         time.Duration
	ConntrackCommand string
	SNATAddresses    []string
	Targets          []nat44SessionSyncTarget
}

type nat44SessionSyncTarget struct {
	Name           string
	Host           string
	User           string
	SSHOptions     []string
	RestoreCommand []string
}

type nat44SessionSyncRestoreResult struct {
	OKDel        int
	MissingDel   int
	NGDel        int
	OKIns        int
	DuplicateIns int
	NGIns        int
}

type conntrackRestoreEntry struct {
	Insert []string
	Delete []string
}

type conntrackRestoreOperation struct {
	Entry      conntrackRestoreEntry
	DeleteOnly bool
}

var defaultNAT44SessionSyncWorkers = newNAT44SessionSyncWorkerManager()

func (c NAT44SessionSyncController) Reconcile(ctx context.Context) error {
	active := map[string]bool{}
	for _, resource := range c.resources() {
		job, pending, err := c.jobFromResource(resource)
		key := nat44SessionSyncWorkerKey(firstNonEmpty(resource.APIVersion, api.NetAPIVersion), resource.Kind, resource.Metadata.Name)
		active[key] = true
		if err != nil {
			c.workerManager().stop(key)
			if saveErr := c.save(resource.APIVersion, resource.Kind, resource.Metadata.Name, map[string]any{"phase": "Error", "reason": "InvalidSpec", "error": err.Error(), "dryRun": c.DryRun}); saveErr != nil {
				return saveErr
			}
			continue
		}
		if pending != "" {
			c.workerManager().stop(key)
			if err := c.save(job.APIVersion, job.Kind, job.Name, map[string]any{"phase": "Pending", "reason": "SNATAddressPending", "pending": pending, "dryRun": c.DryRun}); err != nil {
				return err
			}
			continue
		}
		if err := c.reconcileJob(ctx, job); err != nil {
			return err
		}
	}
	c.workerManager().stopMissing(active)
	return nil
}

func (c NAT44SessionSyncController) resources() []api.Resource {
	if c.Router == nil {
		return nil
	}
	var out []api.Resource
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind == "NAT44SessionSync" {
			out = append(out, resource)
		}
	}
	return out
}

func (c NAT44SessionSyncController) jobFromResource(resource api.Resource) (nat44SessionSyncJob, string, error) {
	spec, err := resource.NAT44SessionSyncSpec()
	if err != nil {
		return nat44SessionSyncJob{}, "", err
	}
	interval := 30 * time.Second
	if strings.TrimSpace(spec.Interval) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(spec.Interval))
		if err != nil {
			return nat44SessionSyncJob{}, "", err
		}
		interval = parsed
	}
	addresses, pending := c.resolveSNATAddresses(spec)
	job := nat44SessionSyncJob{
		APIVersion:       firstNonEmpty(resource.APIVersion, api.NetAPIVersion),
		Kind:             resource.Kind,
		Name:             resource.Metadata.Name,
		Mode:             firstNonEmpty(strings.TrimSpace(spec.Mode), "snapshot"),
		Interval:         interval,
		ConntrackCommand: firstNonEmpty(strings.TrimSpace(spec.ConntrackCommand), "conntrack"),
		SNATAddresses:    addresses,
	}
	for _, target := range spec.Targets {
		restoreCommand := append([]string(nil), target.RestoreCommand...)
		if len(restoreCommand) == 0 {
			restoreCommand = []string{"conntrack"}
		}
		job.Targets = append(job.Targets, nat44SessionSyncTarget{
			Name:           strings.TrimSpace(target.Name),
			Host:           strings.TrimSpace(target.Host),
			User:           strings.TrimSpace(target.User),
			SSHOptions:     append([]string(nil), target.SSHOptions...),
			RestoreCommand: restoreCommand,
		})
	}
	return job, pending, nil
}

func (c NAT44SessionSyncController) resolveSNATAddresses(spec api.NAT44SessionSyncSpec) ([]string, string) {
	addresses := map[string]bool{}
	for _, address := range spec.SNATAddresses {
		address = strings.TrimSpace(address)
		if address != "" {
			addresses[address] = true
		}
	}
	excluded := map[string]bool{}
	for _, ref := range spec.ExcludeNATRules {
		excluded[nat44SessionSyncNATRuleName(ref)] = true
	}
	var pending []string
	for _, ref := range spec.NATRules {
		name := nat44SessionSyncNATRuleName(ref)
		if name == "" || excluded[name] {
			continue
		}
		address := c.snatAddressForNATRule(name)
		if address == "" {
			pending = append(pending, name)
			continue
		}
		addresses[address] = true
	}
	out := make([]string, 0, len(addresses))
	for address := range addresses {
		if addr, err := netip.ParseAddr(address); err == nil && addr.Is4() {
			out = append(out, address)
		}
	}
	sort.Strings(out)
	sort.Strings(pending)
	return out, strings.Join(pending, ",")
}

func (c NAT44SessionSyncController) snatAddressForNATRule(name string) string {
	if c.Router != nil {
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind != "NAT44Rule" || resource.Metadata.Name != name {
				continue
			}
			spec, err := resource.NAT44RuleSpec()
			if err == nil && strings.TrimSpace(spec.SNATAddress) != "" {
				return strings.TrimSpace(spec.SNATAddress)
			}
			break
		}
	}
	if c.Store == nil {
		return ""
	}
	status := c.Store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", name)
	raw, ok := status["snatAddress"]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func nat44SessionSyncNATRuleName(ref string) string {
	ref = strings.TrimSpace(ref)
	if kind, name, ok := strings.Cut(ref, "/"); ok {
		if kind == "NAT44Rule" {
			return strings.TrimSpace(name)
		}
		return ""
	}
	return ref
}

func (c NAT44SessionSyncController) reconcileJob(ctx context.Context, job nat44SessionSyncJob) error {
	if job.Mode == "event-stream" {
		return c.reconcileEventStreamJob(ctx, job)
	}
	c.workerManager().stop(nat44SessionSyncWorkerKey(job.APIVersion, job.Kind, job.Name))
	return c.reconcileSnapshotJob(ctx, job)
}

func (c NAT44SessionSyncController) reconcileSnapshotJob(ctx context.Context, job nat44SessionSyncJob) error {
	now := c.now()
	if c.shouldSkip(job, now) {
		return nil
	}
	if len(job.SNATAddresses) == 0 {
		return c.save(job.APIVersion, job.Kind, job.Name, map[string]any{"phase": "Pending", "reason": "NoSNATAddresses", "dryRun": c.DryRun})
	}
	entries, err := c.dumpEntries(ctx, job)
	if err != nil {
		return c.save(job.APIVersion, job.Kind, job.Name, map[string]any{"phase": "Error", "reason": "DumpFailed", "error": err.Error(), "dryRun": c.DryRun})
	}
	status := map[string]any{
		"mode":             job.Mode,
		"snatAddresses":    job.SNATAddresses,
		"snatAddressCount": len(job.SNATAddresses),
		"sessionCount":     len(entries),
		"targetCount":      len(job.Targets),
		"syncedAt":         now.Format(time.RFC3339Nano),
		"dryRun":           c.DryRun,
	}
	if c.DryRun {
		status["targets"] = nat44SessionSyncTargetStatuses(job.Targets)
		status["phase"] = "Rendered"
		status["reason"] = "DryRun"
		return c.save(job.APIVersion, job.Kind, job.Name, status)
	}
	targetStatuses, total, overallPhase, overallReason := c.restoreEntriesToTargets(ctx, job, entries)
	status["targets"] = targetStatuses
	addNAT44SessionSyncRestoreStatus(status, total)
	status["phase"] = overallPhase
	if overallReason != "" {
		status["reason"] = overallReason
	}
	status["scriptBytes"] = len(nat44SessionSyncRestoreScript(entries, nil))
	return c.save(job.APIVersion, job.Kind, job.Name, status)
}

func (c NAT44SessionSyncController) reconcileEventStreamJob(ctx context.Context, job nat44SessionSyncJob) error {
	if c.DryRun {
		return c.save(job.APIVersion, job.Kind, job.Name, map[string]any{
			"phase":            "Rendered",
			"reason":           "DryRun",
			"mode":             job.Mode,
			"snatAddresses":    job.SNATAddresses,
			"snatAddressCount": len(job.SNATAddresses),
			"targetCount":      len(job.Targets),
			"targets":          nat44SessionSyncTargetStatuses(job.Targets),
			"dryRun":           true,
		})
	}
	if len(job.SNATAddresses) == 0 {
		c.workerManager().stop(nat44SessionSyncWorkerKey(job.APIVersion, job.Kind, job.Name))
		return c.save(job.APIVersion, job.Kind, job.Name, map[string]any{"phase": "Pending", "reason": "NoSNATAddresses", "mode": job.Mode, "dryRun": c.DryRun})
	}
	status := c.workerManager().ensure(ctx, c, job)
	return c.save(job.APIVersion, job.Kind, job.Name, status)
}

func (c NAT44SessionSyncController) dumpEntries(ctx context.Context, job nat44SessionSyncJob) ([]conntrackRestoreEntry, error) {
	run := c.Command
	if run == nil {
		run = runOutputCommandWithInput
	}
	seen := map[string]bool{}
	var out []conntrackRestoreEntry
	for _, address := range job.SNATAddresses {
		runCtx, cancel := nat44SessionSyncCommandContext(ctx)
		data, err := c.runConntrackDump(runCtx, run, job.ConntrackCommand, address)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("%s --dump -n %s: %w: %s", job.ConntrackCommand, address, err, strings.TrimSpace(string(data)))
		}
		for _, line := range strings.Split(string(data), "\n") {
			entry, ok, err := parseConntrackExtendedLine(line)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			key := strings.Join(entry.Insert, "\x00")
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, entry)
		}
	}
	return out, nil
}

func (c NAT44SessionSyncController) restoreEntriesToTargets(ctx context.Context, job nat44SessionSyncJob, entries []conntrackRestoreEntry) ([]map[string]any, nat44SessionSyncRestoreResult, string, string) {
	targetStatuses := make([]map[string]any, 0, len(job.Targets))
	total := nat44SessionSyncRestoreResult{}
	overallPhase := "Synced"
	overallReason := ""
	for _, target := range job.Targets {
		targetStatus := nat44SessionSyncTargetStatus(target)
		targetScript := nat44SessionSyncRestoreScript(entries, target.RestoreCommand)
		out, err := c.runSSH(ctx, target, targetScript)
		if err != nil {
			targetStatus["phase"] = "Error"
			targetStatus["reason"] = "SyncFailed"
			targetStatus["output"] = strings.TrimSpace(string(out))
			targetStatus["error"] = err.Error()
			targetStatuses = append(targetStatuses, targetStatus)
			overallPhase = "Error"
			overallReason = "SyncFailed"
			continue
		}
		result, err := parseNAT44SessionSyncRestoreOutput(out)
		if err != nil {
			targetStatus["phase"] = "Error"
			targetStatus["reason"] = "RestoreOutputInvalid"
			targetStatus["output"] = strings.TrimSpace(string(out))
			targetStatus["error"] = err.Error()
			targetStatuses = append(targetStatuses, targetStatus)
			overallPhase = "Error"
			overallReason = "RestoreOutputInvalid"
			continue
		}
		addNAT44SessionSyncRestoreStatus(targetStatus, result)
		total.OKDel += result.OKDel
		total.MissingDel += result.MissingDel
		total.NGDel += result.NGDel
		total.OKIns += result.OKIns
		total.DuplicateIns += result.DuplicateIns
		total.NGIns += result.NGIns
		phase, reason := nat44SessionSyncRestorePhase(len(entries), result)
		targetStatus["phase"] = phase
		if reason != "" {
			targetStatus["reason"] = reason
		}
		if phase != "Synced" {
			targetStatus["output"] = strings.TrimSpace(string(out))
		}
		targetStatuses = append(targetStatuses, targetStatus)
		switch {
		case phase == "Error":
			overallPhase = "Error"
			overallReason = reason
		case phase == "Degraded" && overallPhase == "Synced":
			overallPhase = "Degraded"
			overallReason = reason
		}
	}
	return targetStatuses, total, overallPhase, overallReason
}

func (c NAT44SessionSyncController) restoreOperationsToTargets(ctx context.Context, job nat44SessionSyncJob, operations []conntrackRestoreOperation) ([]map[string]any, nat44SessionSyncRestoreResult, string, string) {
	targetStatuses := make([]map[string]any, 0, len(job.Targets))
	total := nat44SessionSyncRestoreResult{}
	overallPhase := "Synced"
	overallReason := ""
	for _, target := range job.Targets {
		targetStatus := nat44SessionSyncTargetStatus(target)
		targetScript := nat44SessionSyncRestoreOperationsScript(operations, target.RestoreCommand)
		out, err := c.runSSH(ctx, target, targetScript)
		if err != nil {
			targetStatus["phase"] = "Error"
			targetStatus["reason"] = "SyncFailed"
			targetStatus["output"] = strings.TrimSpace(string(out))
			targetStatus["error"] = err.Error()
			targetStatuses = append(targetStatuses, targetStatus)
			overallPhase = "Error"
			overallReason = "SyncFailed"
			continue
		}
		result, err := parseNAT44SessionSyncRestoreOutput(out)
		if err != nil {
			targetStatus["phase"] = "Error"
			targetStatus["reason"] = "RestoreOutputInvalid"
			targetStatus["output"] = strings.TrimSpace(string(out))
			targetStatus["error"] = err.Error()
			targetStatuses = append(targetStatuses, targetStatus)
			overallPhase = "Error"
			overallReason = "RestoreOutputInvalid"
			continue
		}
		addNAT44SessionSyncRestoreStatus(targetStatus, result)
		total.OKDel += result.OKDel
		total.MissingDel += result.MissingDel
		total.NGDel += result.NGDel
		total.OKIns += result.OKIns
		total.DuplicateIns += result.DuplicateIns
		total.NGIns += result.NGIns
		phase, reason := nat44SessionSyncRestoreOperationsPhase(operations, result)
		targetStatus["phase"] = phase
		if reason != "" {
			targetStatus["reason"] = reason
		}
		if phase != "Synced" {
			targetStatus["output"] = strings.TrimSpace(string(out))
		}
		targetStatuses = append(targetStatuses, targetStatus)
		switch {
		case phase == "Error":
			overallPhase = "Error"
			overallReason = reason
		case phase == "Degraded" && overallPhase == "Synced":
			overallPhase = "Degraded"
			overallReason = reason
		}
	}
	return targetStatuses, total, overallPhase, overallReason
}

func (c NAT44SessionSyncController) runConntrackDump(ctx context.Context, run sessionSyncCommandFunc, command, address string) ([]byte, error) {
	args := []string{"--dump", "-o", "extended", "-n", address}
	if c.Command != nil {
		return run(ctx, command, args, nil)
	}
	cmd := exec.CommandContext(ctx, command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stderr.Bytes(), err
	}
	return stdout.Bytes(), nil
}

func (c NAT44SessionSyncController) runConntrackEventStream(ctx context.Context, command string) (io.ReadCloser, func() error, error) {
	args := []string{"-E", "-o", "extended"}
	if c.EventCommand != nil {
		return c.EventCommand(ctx, command, args)
	}
	cmd := exec.CommandContext(ctx, command, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	wait := func() error {
		if err := cmd.Wait(); err != nil {
			if stderr.Len() > 0 {
				return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
			}
			return err
		}
		return nil
	}
	return stdout, wait, nil
}

func (c NAT44SessionSyncController) runSSH(ctx context.Context, target nat44SessionSyncTarget, script []byte) ([]byte, error) {
	run := c.Command
	if run == nil {
		run = runOutputCommandWithInput
	}
	args := append([]string{}, fileSyncEffectiveSSHOptions(target.SSHOptions)...)
	args = append(args, nat44SessionSyncDestination(target), "sh", "-s")
	runCtx, cancel := nat44SessionSyncCommandContext(ctx)
	defer cancel()
	return run(runCtx, "ssh", args, script)
}

func (c NAT44SessionSyncController) shouldSkip(job nat44SessionSyncJob, now time.Time) bool {
	if job.Interval <= 0 || c.Store == nil {
		return false
	}
	status := c.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name)
	last, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(status["syncedAt"]))
	return !last.IsZero() && now.Sub(last) < job.Interval
}

func (c NAT44SessionSyncController) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

func (c NAT44SessionSyncController) save(apiVersion, kind, name string, status map[string]any) error {
	if c.Store == nil {
		return nil
	}
	return c.Store.SaveObjectStatus(apiVersion, kind, name, status)
}

func (c NAT44SessionSyncController) workerManager() *nat44SessionSyncWorkerManager {
	if c.Workers != nil {
		return c.Workers
	}
	return defaultNAT44SessionSyncWorkers
}

type nat44SessionSyncWorkerManager struct {
	mu      sync.Mutex
	workers map[string]*nat44SessionSyncWorker
}

func newNAT44SessionSyncWorkerManager() *nat44SessionSyncWorkerManager {
	return &nat44SessionSyncWorkerManager{workers: map[string]*nat44SessionSyncWorker{}}
}

func (m *nat44SessionSyncWorkerManager) ensure(ctx context.Context, controller NAT44SessionSyncController, job nat44SessionSyncJob) map[string]any {
	key := nat44SessionSyncWorkerKey(job.APIVersion, job.Kind, job.Name)
	signature := nat44SessionSyncWorkerSignature(job)
	m.mu.Lock()
	worker := m.workers[key]
	if worker == nil || worker.signature != signature {
		if worker != nil {
			worker.stop()
		}
		worker = newNAT44SessionSyncWorker(ctx, controller, job, signature)
		m.workers[key] = worker
		worker.start()
	}
	status := worker.status()
	m.mu.Unlock()
	return status
}

func (m *nat44SessionSyncWorkerManager) stop(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if worker := m.workers[key]; worker != nil {
		worker.stop()
		delete(m.workers, key)
	}
}

func (m *nat44SessionSyncWorkerManager) stopMissing(active map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, worker := range m.workers {
		if !active[key] {
			worker.stop()
			delete(m.workers, key)
		}
	}
}

func nat44SessionSyncWorkerKey(apiVersion, kind, name string) string {
	return apiVersion + "/" + kind + "/" + name
}

func nat44SessionSyncWorkerSignature(job nat44SessionSyncJob) string {
	var b strings.Builder
	b.WriteString(job.Mode)
	b.WriteString("|")
	b.WriteString(job.ConntrackCommand)
	b.WriteString("|")
	b.WriteString(strings.Join(job.SNATAddresses, ","))
	for _, target := range job.Targets {
		b.WriteString("|")
		b.WriteString(target.Name)
		b.WriteString("@")
		b.WriteString(nat44SessionSyncDestination(target))
		b.WriteString("/")
		b.WriteString(strings.Join(target.SSHOptions, "\x00"))
		b.WriteString("/")
		b.WriteString(strings.Join(target.RestoreCommand, "\x00"))
	}
	return b.String()
}

type nat44SessionSyncWorker struct {
	controller NAT44SessionSyncController
	job        nat44SessionSyncJob
	signature  string
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	state      map[string]any
}

func newNAT44SessionSyncWorker(ctx context.Context, controller NAT44SessionSyncController, job nat44SessionSyncJob, signature string) *nat44SessionSyncWorker {
	workerCtx, cancel := context.WithCancel(ctx)
	controller.Workers = nil
	return &nat44SessionSyncWorker{
		controller: controller,
		job:        job,
		signature:  signature,
		ctx:        workerCtx,
		cancel:     cancel,
		state: map[string]any{
			"phase":            "Pending",
			"reason":           "Starting",
			"mode":             "event-stream",
			"streamState":      "starting",
			"snatAddresses":    job.SNATAddresses,
			"snatAddressCount": len(job.SNATAddresses),
			"targetCount":      len(job.Targets),
			"targets":          nat44SessionSyncTargetStatuses(job.Targets),
			"dryRun":           controller.DryRun,
		},
	}
}

func (w *nat44SessionSyncWorker) start() {
	go w.run()
}

func (w *nat44SessionSyncWorker) stop() {
	w.cancel()
}

func (w *nat44SessionSyncWorker) status() map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	return cloneStatusMap(w.state)
}

func (w *nat44SessionSyncWorker) set(fields map[string]any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	next := cloneStatusMap(w.state)
	for key, value := range fields {
		if value == nil {
			delete(next, key)
			continue
		}
		next[key] = value
	}
	w.state = next
}

func (w *nat44SessionSyncWorker) run() {
	for {
		if err := w.runOnce(); err != nil {
			if w.ctx.Err() != nil {
				return
			}
			w.set(map[string]any{"phase": "Degraded", "reason": "StreamFailed", "streamState": "restarting", "lastError": err.Error()})
			select {
			case <-time.After(5 * time.Second):
			case <-w.ctx.Done():
				return
			}
			continue
		}
		return
	}
}

func (w *nat44SessionSyncWorker) runOnce() error {
	if err := w.resync(); err != nil {
		return err
	}
	reader, wait, err := w.controller.runConntrackEventStream(w.ctx, w.job.ConntrackCommand)
	if err != nil {
		return err
	}
	defer reader.Close()
	w.set(map[string]any{"phase": "Synced", "streamState": "running", "reason": nil, "lastError": nil})
	lines := make(chan string, 128)
	readErr := make(chan error, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-w.ctx.Done():
				readErr <- w.ctx.Err()
				return
			}
		}
		if err := scanner.Err(); err != nil {
			readErr <- err
			return
		}
		if wait != nil {
			readErr <- wait()
			return
		}
		readErr <- nil
	}()
	var batch []conntrackRestoreOperation
	ticker := time.NewTicker(defaultNAT44SessionSyncEventBatchInterval)
	defer ticker.Stop()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				lines = nil
				continue
			}
			op, ok, err := parseConntrackEventLine(line, w.job.SNATAddresses)
			if err != nil {
				w.set(map[string]any{"phase": "Degraded", "reason": "EventParseFailed", "lastError": err.Error(), "streamState": "running"})
				continue
			}
			if !ok {
				continue
			}
			batch = append(batch, op)
			w.set(map[string]any{"lastEventAt": time.Now().UTC().Format(time.RFC3339Nano), "queuedEventCount": len(batch)})
			if len(batch) >= defaultNAT44SessionSyncEventBatchMax {
				w.flush(batch)
				batch = nil
			}
		case err := <-readErr:
			if len(batch) > 0 {
				w.flush(batch)
			}
			if err != nil && w.ctx.Err() == nil {
				return err
			}
			if w.ctx.Err() != nil {
				return w.ctx.Err()
			}
			return fmt.Errorf("%s event stream exited", w.job.ConntrackCommand)
		case <-ticker.C:
			if len(batch) > 0 {
				w.flush(batch)
				batch = nil
			}
		case <-w.ctx.Done():
			return w.ctx.Err()
		}
	}
}

func (w *nat44SessionSyncWorker) resync() error {
	now := time.Now().UTC()
	w.set(map[string]any{"phase": "Pending", "reason": "Resyncing", "streamState": "resyncing", "lastResyncStartedAt": now.Format(time.RFC3339Nano)})
	entries, err := w.controller.dumpEntries(w.ctx, w.job)
	if err != nil {
		return err
	}
	targetStatuses, total, phase, reason := w.controller.restoreEntriesToTargets(w.ctx, w.job, entries)
	status := map[string]any{
		"mode":             "event-stream",
		"streamState":      "running",
		"snatAddresses":    w.job.SNATAddresses,
		"snatAddressCount": len(w.job.SNATAddresses),
		"sessionCount":     len(entries),
		"targetCount":      len(w.job.Targets),
		"targets":          targetStatuses,
		"syncedAt":         now.Format(time.RFC3339Nano),
		"lastResyncAt":     now.Format(time.RFC3339Nano),
		"queuedEventCount": 0,
		"dryRun":           w.controller.DryRun,
		"phase":            phase,
		"scriptBytes":      len(nat44SessionSyncRestoreScript(entries, nil)),
	}
	if reason != "" {
		status["reason"] = reason
	} else {
		status["reason"] = nil
	}
	status["lastError"] = nil
	addNAT44SessionSyncRestoreStatus(status, total)
	current := w.status()
	resyncCount, _ := statusInt(current["resyncCount"])
	status["resyncCount"] = resyncCount + 1
	w.set(status)
	return nil
}

func (w *nat44SessionSyncWorker) flush(operations []conntrackRestoreOperation) {
	if len(operations) == 0 {
		return
	}
	targetStatuses, total, phase, reason := w.controller.restoreOperationsToTargets(w.ctx, w.job, operations)
	status := map[string]any{
		"phase":            phase,
		"streamState":      "running",
		"targets":          targetStatuses,
		"lastBatchAt":      time.Now().UTC().Format(time.RFC3339Nano),
		"lastBatchEvents":  len(operations),
		"queuedEventCount": 0,
	}
	if reason != "" {
		status["reason"] = reason
	} else {
		status["reason"] = nil
	}
	status["lastError"] = nil
	addNAT44SessionSyncRestoreStatus(status, total)
	w.set(status)
}

func cloneStatusMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func parseConntrackExtendedLine(line string) (conntrackRestoreEntry, bool, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) > 0 && fields[0] == "conntrack" && strings.Contains(line, "flow entries have been shown") {
		return conntrackRestoreEntry{}, false, nil
	}
	for len(fields) > 0 && strings.HasPrefix(fields[0], "[") && strings.HasSuffix(fields[0], "]") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return conntrackRestoreEntry{}, false, nil
	}
	if len(fields) < 4 {
		return conntrackRestoreEntry{}, false, fmt.Errorf("short conntrack extended line: %q", line)
	}
	family, proto, index, ok := conntrackExtendedHeader(fields)
	if !ok {
		return conntrackRestoreEntry{}, false, fmt.Errorf("unsupported conntrack extended line: %q", line)
	}
	timeout := ""
	if _, err := strconv.Atoi(fields[index]); err == nil {
		timeout = fields[index]
		index++
	}
	state := ""
	if index < len(fields) && !strings.Contains(fields[index], "=") && !strings.HasPrefix(fields[index], "[") {
		state = fields[index]
		index++
	}
	orig, reply, mark, flags := parseConntrackExtendedTuples(fields[index:])
	status := conntrackStatusFromFlags(flags)
	insert := []string{"-I"}
	if timeout != "" {
		insert = append(insert, "-t", timeout)
	}
	insert = append(insert, "-u", status, "-s", orig["src"], "-d", orig["dst"], "-r", reply["src"], "-q", reply["dst"], "-p", proto)
	switch proto {
	case "tcp", "udp":
		insert = append(insert, "--sport", orig["sport"], "--dport", orig["dport"], "--reply-port-src", reply["sport"], "--reply-port-dst", reply["dport"])
	case "icmp":
		insert = append(insert, "--icmp-type", orig["type"], "--icmp-code", orig["code"], "--icmp-id", orig["id"])
	default:
		return conntrackRestoreEntry{}, false, fmt.Errorf("unsupported conntrack protocol %q", proto)
	}
	if proto == "tcp" && state != "" {
		insert = append(insert, "--state", state)
	}
	if mark != "" {
		insert = append(insert, "-m", mark)
	}
	if family == "ipv6" {
		insert = append(insert, "-f", "ipv6")
	}
	deleteArgs := conntrackDeleteArgs(insert)
	return conntrackRestoreEntry{Insert: insert, Delete: deleteArgs}, true, nil
}

func parseConntrackEventLine(line string, snatAddresses []string) (conntrackRestoreOperation, bool, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	deleteOnly := false
	var rest []string
	for _, field := range fields {
		trimmed := strings.Trim(field, "[]")
		switch trimmed {
		case "NEW", "UPDATE":
			continue
		case "DESTROY", "DELETE":
			deleteOnly = true
			continue
		default:
			rest = append(rest, field)
		}
	}
	if len(rest) == 0 {
		return conntrackRestoreOperation{}, false, nil
	}
	if !conntrackEventFamilyAndProtocolSupported(rest) {
		return conntrackRestoreOperation{}, false, nil
	}
	entry, ok, err := parseConntrackExtendedLine(strings.Join(rest, " "))
	if err != nil || !ok {
		return conntrackRestoreOperation{}, ok, err
	}
	if !conntrackEntryMatchesSNAT(entry, snatAddresses) {
		return conntrackRestoreOperation{}, false, nil
	}
	return conntrackRestoreOperation{Entry: entry, DeleteOnly: deleteOnly}, true, nil
}

func conntrackEventFamilyAndProtocolSupported(fields []string) bool {
	family, proto, _, ok := conntrackExtendedHeader(fields)
	if !ok || family != "ipv4" {
		return false
	}
	switch proto {
	case "tcp", "udp", "icmp":
		return true
	default:
		return false
	}
}

func conntrackEntryMatchesSNAT(entry conntrackRestoreEntry, snatAddresses []string) bool {
	if len(snatAddresses) == 0 {
		return false
	}
	want := map[string]bool{}
	for _, address := range snatAddresses {
		want[address] = true
	}
	for i := 0; i < len(entry.Insert)-1; i++ {
		if entry.Insert[i] == "-q" && want[entry.Insert[i+1]] {
			return true
		}
	}
	return false
}

func conntrackExtendedHeader(fields []string) (family, proto string, index int, ok bool) {
	if len(fields) >= 5 && (fields[0] == "ipv4" || fields[0] == "ipv6") {
		return fields[0], fields[2], 4, true
	}
	if len(fields) >= 4 {
		switch fields[0] {
		case "2":
			return "ipv4", fields[1], 3, true
		case "10":
			return "ipv6", fields[1], 3, true
		}
	}
	return "", "", 0, false
}

func parseConntrackExtendedTuples(fields []string) (map[string]string, map[string]string, string, map[string]bool) {
	orig := map[string]string{}
	reply := map[string]string{}
	flags := map[string]bool{}
	mark := ""
	inReply := false
	for _, field := range fields {
		if strings.HasPrefix(field, "[") && strings.HasSuffix(field, "]") {
			flags[strings.Trim(field, "[]")] = true
			continue
		}
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "packets", "bytes", "use":
			continue
		case "mark":
			mark = value
			continue
		case "src":
			if _, exists := orig["src"]; exists {
				inReply = true
			}
		}
		if inReply {
			reply[key] = value
		} else {
			orig[key] = value
		}
	}
	return orig, reply, mark, flags
}

func conntrackStatusFromFlags(flags map[string]bool) string {
	switch {
	case flags["ASSURED"]:
		return "SEEN_REPLY,ASSURED"
	case flags["UNREPLIED"]:
		return "UNSET"
	default:
		return "SEEN_REPLY"
	}
}

func conntrackDeleteArgs(insert []string) []string {
	deleteArgs := []string{"-D"}
	for i := 1; i < len(insert); i++ {
		switch insert[i] {
		case "-t", "-u", "--state", "-m":
			i++
			continue
		default:
			deleteArgs = append(deleteArgs, insert[i])
		}
	}
	return deleteArgs
}

func nat44SessionSyncRestoreScript(entries []conntrackRestoreEntry, command []string) []byte {
	operations := make([]conntrackRestoreOperation, 0, len(entries))
	for _, entry := range entries {
		operations = append(operations, conntrackRestoreOperation{Entry: entry})
	}
	return nat44SessionSyncRestoreOperationsScript(operations, command)
}

func nat44SessionSyncRestoreOperationsScript(operations []conntrackRestoreOperation, command []string) []byte {
	if len(command) == 0 {
		command = []string{"conntrack"}
	}
	var buf bytes.Buffer
	buf.WriteString("#!/bin/sh\nset -eu\nok_del=0; miss_del=0; ng_del=0; ok_ins=0; dup_ins=0; ng_ins=0; err_lines=0\n")
	buf.WriteString("record_restore_error() {\n")
	buf.WriteString("  if [ \"$err_lines\" -lt 3 ]; then\n")
	buf.WriteString("    printf '%s\\n' \"$1\"\n")
	buf.WriteString("    err_lines=$((err_lines+1))\n")
	buf.WriteString("  fi\n")
	buf.WriteString("}\n")
	for _, operation := range operations {
		entry := operation.Entry
		buf.WriteString("if out=$(")
		buf.WriteString(shellCommand(command, entry.Delete))
		buf.WriteString(" 2>&1); then\n")
		buf.WriteString("  case \"$out\" in *\"0 flow entries\"*|*\"not found\"*|*\"No such file\"*|*\"does not exist\"*) miss_del=$((miss_del+1));; *) ok_del=$((ok_del+1));; esac\n")
		buf.WriteString("else\n")
		buf.WriteString("  case \"$out\" in *\"0 flow entries\"*|*\"not found\"*|*\"No such file\"*|*\"does not exist\"*) miss_del=$((miss_del+1));; *) ng_del=$((ng_del+1)); record_restore_error \"delete failed: $out\";; esac\n")
		buf.WriteString("fi\n")
		if operation.DeleteOnly {
			continue
		}
		buf.WriteString("if out=$(")
		buf.WriteString(shellCommand(command, entry.Insert))
		buf.WriteString(" 2>&1); then ok_ins=$((ok_ins+1)); else\n")
		buf.WriteString("  case \"$out\" in *\"File exists\"*|*\"already exists\"*|*\"Such conntrack exists\"*|*\"exists, try -U\"*) dup_ins=$((dup_ins+1));; *) ng_ins=$((ng_ins+1)); record_restore_error \"insert failed: $out\";; esac\n")
		buf.WriteString("fi\n")
	}
	buf.WriteString("echo ok_del=$ok_del miss_del=$miss_del ng_del=$ng_del ok_ins=$ok_ins dup_ins=$dup_ins ng_ins=$ng_ins\n")
	return buf.Bytes()
}

func parseNAT44SessionSyncRestoreOutput(output []byte) (nat44SessionSyncRestoreResult, error) {
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		values := map[string]int{}
		for _, field := range fields {
			key, raw, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch key {
			case "ok_del", "miss_del", "ng_del", "ok_ins", "dup_ins", "ng_ins":
				value, err := strconv.Atoi(raw)
				if err != nil {
					return nat44SessionSyncRestoreResult{}, fmt.Errorf("%s must be an integer: %w", key, err)
				}
				values[key] = value
			}
		}
		if len(values) == 0 {
			continue
		}
		for _, key := range []string{"ok_del", "miss_del", "ng_del", "ok_ins", "dup_ins", "ng_ins"} {
			if _, ok := values[key]; !ok {
				return nat44SessionSyncRestoreResult{}, fmt.Errorf("restore output missing %s", key)
			}
		}
		return nat44SessionSyncRestoreResult{
			OKDel:        values["ok_del"],
			MissingDel:   values["miss_del"],
			NGDel:        values["ng_del"],
			OKIns:        values["ok_ins"],
			DuplicateIns: values["dup_ins"],
			NGIns:        values["ng_ins"],
		}, nil
	}
	return nat44SessionSyncRestoreResult{}, fmt.Errorf("restore output missing summary")
}

func nat44SessionSyncRestorePhase(entries int, result nat44SessionSyncRestoreResult) (string, string) {
	insertConverged := result.OKIns + result.DuplicateIns
	switch {
	case entries > 0 && insertConverged == 0:
		return "Error", "RestoreFailed"
	case result.NGDel > 0 || result.NGIns > 0:
		return "Degraded", "RestorePartialFailed"
	default:
		return "Synced", ""
	}
}

func nat44SessionSyncRestoreOperationsPhase(operations []conntrackRestoreOperation, result nat44SessionSyncRestoreResult) (string, string) {
	insertCount := 0
	for _, operation := range operations {
		if !operation.DeleteOnly {
			insertCount++
		}
	}
	if insertCount > 0 {
		return nat44SessionSyncRestorePhase(insertCount, result)
	}
	if result.NGDel > 0 {
		return "Degraded", "RestorePartialFailed"
	}
	return "Synced", ""
}

func addNAT44SessionSyncRestoreStatus(status map[string]any, result nat44SessionSyncRestoreResult) {
	status["deleteOK"] = result.OKDel
	status["deleteMissing"] = result.MissingDel
	status["deleteFailed"] = result.NGDel
	status["insertOK"] = result.OKIns
	status["insertExisting"] = result.DuplicateIns
	status["insertFailed"] = result.NGIns
}

func shellCommand(command, args []string) string {
	parts := make([]string, 0, len(command)+len(args))
	for _, part := range append(append([]string{}, command...), args...) {
		parts = append(parts, fileSyncShellQuote(part))
	}
	return strings.Join(parts, " ")
}

func nat44SessionSyncTargetStatuses(targets []nat44SessionSyncTarget) []map[string]any {
	out := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		out = append(out, nat44SessionSyncTargetStatus(target))
	}
	return out
}

func nat44SessionSyncTargetStatus(target nat44SessionSyncTarget) map[string]any {
	return map[string]any{"name": target.Name, "host": target.Host, "user": target.User}
}

func nat44SessionSyncDestination(target nat44SessionSyncTarget) string {
	if target.User != "" {
		return target.User + "@" + target.Host
	}
	return target.Host
}

func nat44SessionSyncCommandContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if defaultNAT44SessionSyncCommandTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultNAT44SessionSyncCommandTimeout)
}

func runOutputCommandWithInput(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}
