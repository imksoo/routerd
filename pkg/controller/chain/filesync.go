// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

const (
	defaultFileSyncCommandTimeout      = 2 * time.Minute
	defaultFileSyncRsyncTimeoutSeconds = "60"
)

type fileSyncCommandFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

type fileSyncJob struct {
	APIVersion string
	Kind       string
	Name       string
	Command    string
	Interval   time.Duration
	Sources    []fileSyncSource
	Targets    []fileSyncTarget
}

type fileSyncSource struct {
	Name     string
	Path     string
	Required bool
}

type fileSyncTarget struct {
	Name       string
	Host       string
	User       string
	Path       string
	SSHOptions []string
	Options    []string
}

type FileSyncController struct {
	Router  *api.Router
	Store   Store
	DryRun  bool
	Command fileSyncCommandFunc
	Now     func() time.Time
}

func (c FileSyncController) Reconcile(ctx context.Context) error {
	for _, resource := range c.fileSyncResources() {
		job, err := c.fileSyncJobFromResource(resource)
		if err != nil {
			if saveErr := c.save(resource.APIVersion, resource.Kind, resource.Metadata.Name, map[string]any{"phase": "Error", "reason": "InvalidSpec", "error": err.Error(), "dryRun": c.DryRun}); saveErr != nil {
				return saveErr
			}
			continue
		}
		if err := c.reconcileJob(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

func (c FileSyncController) fileSyncResources() []api.Resource {
	if c.Router == nil {
		return nil
	}
	var out []api.Resource
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind == "DHCPv4ServerLeaseSync" || resource.Kind == "DHCPv6ServerLeaseSync" || resource.Kind == "DHCPv6PrefixDelegationLeaseSync" {
			out = append(out, resource)
		}
	}
	return out
}

func (c FileSyncController) reconcileJob(ctx context.Context, job fileSyncJob) error {
	now := c.now()
	if c.shouldSkip(job, now) {
		return nil
	}
	sources, pending, err := fileSyncSourceStatuses(job.Sources)
	if err != nil {
		return c.save(job.APIVersion, job.Kind, job.Name, map[string]any{"phase": "Error", "reason": "SourceStatFailed", "error": err.Error(), "dryRun": c.DryRun})
	}
	if pending != "" {
		return c.save(job.APIVersion, job.Kind, job.Name, map[string]any{"phase": "Pending", "reason": "SourceMissing", "source": pending, "sources": sources, "dryRun": c.DryRun})
	}
	if len(sources) == 0 {
		return c.save(job.APIVersion, job.Kind, job.Name, map[string]any{"phase": "Pending", "reason": "NoSources", "dryRun": c.DryRun})
	}
	targetStatuses := fileSyncTargetStatuses(job.Targets)
	if c.DryRun {
		return c.save(job.APIVersion, job.Kind, job.Name, map[string]any{
			"phase":       "Rendered",
			"reason":      "DryRun",
			"command":     firstNonEmpty(job.Command, "rsync"),
			"sources":     sources,
			"targets":     targetStatuses,
			"syncedAt":    now.Format(time.RFC3339Nano),
			"sourceCount": len(sources),
			"targetCount": len(job.Targets),
			"dryRun":      true,
		})
	}
	run := c.Command
	if run == nil {
		run = runOutputCommandContext
	}
	command := firstNonEmpty(job.Command, "rsync")
	for _, source := range job.Sources {
		if !fileSyncSourcePresent(sources, source.Path) {
			continue
		}
		for _, target := range job.Targets {
			args := fileSyncRsyncArgs(source, target, len(job.Sources))
			runCtx, cancel := fileSyncCommandContext(ctx)
			out, err := run(runCtx, command, args...)
			cancel()
			if err != nil {
				return c.save(job.APIVersion, job.Kind, job.Name, map[string]any{
					"phase":   "Error",
					"reason":  "SyncFailed",
					"command": command,
					"source":  source.Path,
					"target":  target.Host,
					"output":  strings.TrimSpace(string(out)),
					"error":   err.Error(),
					"dryRun":  false,
				})
			}
		}
	}
	return c.save(job.APIVersion, job.Kind, job.Name, map[string]any{
		"phase":       "Synced",
		"command":     command,
		"sources":     sources,
		"targets":     targetStatuses,
		"syncedAt":    now.Format(time.RFC3339Nano),
		"sourceCount": len(sources),
		"targetCount": len(job.Targets),
		"dryRun":      false,
	})
}

func (c FileSyncController) shouldSkip(job fileSyncJob, now time.Time) bool {
	if job.Interval <= 0 || c.Store == nil {
		return false
	}
	status := c.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name)
	last, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(status["syncedAt"]))
	return !last.IsZero() && now.Sub(last) < job.Interval
}

func (c FileSyncController) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

func (c FileSyncController) save(apiVersion, kind, name string, status map[string]any) error {
	if c.Store == nil {
		return nil
	}
	return c.Store.SaveObjectStatus(apiVersion, kind, name, status)
}

func (c FileSyncController) fileSyncJobFromResource(resource api.Resource) (fileSyncJob, error) {
	switch resource.Kind {
	case "DHCPv4ServerLeaseSync":
		return c.fileSyncJobFromDHCPv4ServerLeaseSync(resource)
	case "DHCPv6ServerLeaseSync":
		return c.fileSyncJobFromDHCPv6ServerLeaseSync(resource)
	case "DHCPv6PrefixDelegationLeaseSync":
		return fileSyncJobFromDHCPv6PrefixDelegationLeaseSync(resource)
	default:
		return fileSyncJob{}, fmt.Errorf("unsupported file sync resource kind %s", resource.Kind)
	}
}

func (c FileSyncController) fileSyncJobFromDHCPv4ServerLeaseSync(resource api.Resource) (fileSyncJob, error) {
	spec, err := resource.DHCPv4ServerLeaseSyncSpec()
	if err != nil {
		return fileSyncJob{}, err
	}
	interval := 30 * time.Second
	if strings.TrimSpace(spec.Interval) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(spec.Interval))
		if err != nil {
			return fileSyncJob{}, err
		}
		interval = parsed
	}
	job := fileSyncJob{
		APIVersion: firstNonEmpty(resource.APIVersion, api.NetAPIVersion),
		Kind:       resource.Kind,
		Name:       resource.Metadata.Name,
		Command:    strings.TrimSpace(spec.Command),
		Interval:   interval,
	}
	sourceName := dhcpv4ServerLeaseSyncResourceName(spec.Source.Resource)
	if sourceName != "" {
		job.Sources = append(job.Sources, fileSyncSource{Name: "leaseFile", Path: c.dhcpv4LeaseFile(sourceName), Required: true})
	}
	for _, target := range spec.Targets {
		job.Targets = append(job.Targets, fileSyncTarget{
			Name:       strings.TrimSpace(target.Name),
			Host:       strings.TrimSpace(target.Host),
			User:       strings.TrimSpace(target.User),
			SSHOptions: append([]string(nil), target.SSHOptions...),
			Options:    append([]string(nil), target.Options...),
		})
	}
	return job, nil
}

func (c FileSyncController) dhcpv4LeaseFile(resourceName string) string {
	if c.Router != nil {
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind != "DHCPv4Server" || resource.Metadata.Name != resourceName {
				continue
			}
			spec, err := resource.DHCPv4ServerSpec()
			if err == nil && strings.TrimSpace(spec.LeaseFile) != "" {
				return strings.TrimSpace(spec.LeaseFile)
			}
			break
		}
	}
	return defaultDHCPv4LeaseFile()
}

func (c FileSyncController) fileSyncJobFromDHCPv6ServerLeaseSync(resource api.Resource) (fileSyncJob, error) {
	spec, err := resource.DHCPv6ServerLeaseSyncSpec()
	if err != nil {
		return fileSyncJob{}, err
	}
	interval := 30 * time.Second
	if strings.TrimSpace(spec.Interval) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(spec.Interval))
		if err != nil {
			return fileSyncJob{}, err
		}
		interval = parsed
	}
	job := fileSyncJob{
		APIVersion: firstNonEmpty(resource.APIVersion, api.NetAPIVersion),
		Kind:       resource.Kind,
		Name:       resource.Metadata.Name,
		Command:    strings.TrimSpace(spec.Command),
		Interval:   interval,
	}
	sourceName := dhcpv6ServerLeaseSyncResourceName(spec.Source.Resource)
	if sourceName != "" {
		job.Sources = append(job.Sources, fileSyncSource{Name: "leaseFile", Path: c.dhcpv6LeaseFile(sourceName), Required: true})
	}
	for _, target := range spec.Targets {
		job.Targets = append(job.Targets, fileSyncTarget{
			Name:       strings.TrimSpace(target.Name),
			Host:       strings.TrimSpace(target.Host),
			User:       strings.TrimSpace(target.User),
			SSHOptions: append([]string(nil), target.SSHOptions...),
			Options:    append([]string(nil), target.Options...),
		})
	}
	return job, nil
}

func (c FileSyncController) dhcpv6LeaseFile(resourceName string) string {
	if c.Router != nil {
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind != "DHCPv6Server" || resource.Metadata.Name != resourceName {
				continue
			}
			spec, err := resource.DHCPv6ServerSpec()
			if err == nil && strings.TrimSpace(spec.LeaseFile) != "" {
				return strings.TrimSpace(spec.LeaseFile)
			}
			break
		}
	}
	return defaultDNSMasqLeaseFile()
}

func fileSyncJobFromDHCPv6PrefixDelegationLeaseSync(resource api.Resource) (fileSyncJob, error) {
	spec, err := resource.DHCPv6PrefixDelegationLeaseSyncSpec()
	if err != nil {
		return fileSyncJob{}, err
	}
	interval := 30 * time.Second
	if strings.TrimSpace(spec.Interval) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(spec.Interval))
		if err != nil {
			return fileSyncJob{}, err
		}
		interval = parsed
	}
	pdName := dhcpv6PrefixDelegationLeaseSyncResourceName(spec.Source.Resource)
	leaseFile := ""
	if pdName != "" {
		leaseFile = defaultDHCPv6PDLeaseFile(pdName)
	}
	job := fileSyncJob{
		APIVersion: firstNonEmpty(resource.APIVersion, api.NetAPIVersion),
		Kind:       resource.Kind,
		Name:       resource.Metadata.Name,
		Command:    strings.TrimSpace(spec.Command),
		Interval:   interval,
	}
	if leaseFile != "" {
		job.Sources = append(job.Sources, fileSyncSource{Name: "leaseFile", Path: leaseFile, Required: true})
	}
	for _, target := range spec.Targets {
		job.Targets = append(job.Targets, fileSyncTarget{
			Name:       strings.TrimSpace(target.Name),
			Host:       strings.TrimSpace(target.Host),
			User:       strings.TrimSpace(target.User),
			SSHOptions: append([]string(nil), target.SSHOptions...),
			Options:    append([]string(nil), target.Options...),
		})
	}
	return job, nil
}

func dhcpv6PrefixDelegationLeaseSyncResourceName(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	kind, name, ok := strings.Cut(ref, "/")
	if !ok {
		return ref
	}
	if kind != "DHCPv6PrefixDelegation" {
		return ""
	}
	return strings.TrimSpace(name)
}

func dhcpv4ServerLeaseSyncResourceName(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	kind, name, ok := strings.Cut(ref, "/")
	if !ok {
		return ref
	}
	if kind != "DHCPv4Server" {
		return ""
	}
	return strings.TrimSpace(name)
}

func dhcpv6ServerLeaseSyncResourceName(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	kind, name, ok := strings.Cut(ref, "/")
	if !ok {
		return ref
	}
	if kind != "DHCPv6Server" {
		return ""
	}
	return strings.TrimSpace(name)
}

func defaultDHCPv4LeaseFile() string {
	return defaultDNSMasqLeaseFile()
}

func defaultDNSMasqLeaseFile() string {
	defaults, _ := platform.Current()
	return filepath.Join(defaults.StateDir, "dnsmasq", "dnsmasq.leases")
}

func defaultDHCPv6PDLeaseFile(resource string) string {
	defaults, _ := platform.Current()
	return filepath.Join(defaults.StateDir, "dhcpv6-client", resource, "lease.json")
}

func fileSyncSourceStatuses(sources []fileSyncSource) ([]map[string]any, string, error) {
	var statuses []map[string]any
	for _, source := range sources {
		info, err := os.Stat(source.Path)
		if err != nil {
			if os.IsNotExist(err) {
				if source.Required {
					return statuses, source.Path, nil
				}
				continue
			}
			return statuses, "", err
		}
		if info.IsDir() {
			return statuses, "", fmt.Errorf("%s is a directory", source.Path)
		}
		statuses = append(statuses, map[string]any{
			"name":    source.Name,
			"path":    source.Path,
			"size":    info.Size(),
			"modTime": info.ModTime().UTC().Format(time.RFC3339Nano),
		})
	}
	return statuses, "", nil
}

func fileSyncSourcePresent(statuses []map[string]any, sourcePath string) bool {
	for _, status := range statuses {
		if fmt.Sprint(status["path"]) == sourcePath {
			return true
		}
	}
	return false
}

func fileSyncTargetStatuses(targets []fileSyncTarget) []map[string]any {
	out := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		out = append(out, map[string]any{
			"name": target.Name,
			"host": target.Host,
			"user": target.User,
			"path": target.Path,
		})
	}
	return out
}

func fileSyncRsyncArgs(source fileSyncSource, target fileSyncTarget, sourceCount int) []string {
	args := []string{"-a", "--delay-updates"}
	if !fileSyncHasRsyncOption(target.Options, "--timeout") {
		args = append(args, "--timeout="+defaultFileSyncRsyncTimeoutSeconds)
	}
	args = append(args, target.Options...)
	sshOptions := fileSyncEffectiveSSHOptions(target.SSHOptions)
	if len(sshOptions) > 0 {
		args = append(args, "-e", "ssh "+strings.Join(sshOptions, " "))
	}
	remotePath := fileSyncTargetPath(source, target, sourceCount)
	if dir := path.Dir(remotePath); dir != "." && dir != "/" {
		args = append(args, "--rsync-path=mkdir -p "+fileSyncShellQuote(dir)+" && rsync")
	}
	args = append(args, source.Path, fileSyncDestination(target, remotePath))
	return args
}

func fileSyncCommandContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if defaultFileSyncCommandTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultFileSyncCommandTimeout)
}

func fileSyncHasRsyncOption(options []string, name string) bool {
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == name || strings.HasPrefix(option, name+"=") {
			return true
		}
	}
	return false
}

func fileSyncEffectiveSSHOptions(options []string) []string {
	userKeys := fileSyncSSHOptionKeys(options)
	defaults := []struct {
		key  string
		args []string
	}{
		{key: "batchmode", args: []string{"-o", "BatchMode=yes"}},
		{key: "connecttimeout", args: []string{"-o", "ConnectTimeout=10"}},
	}
	var out []string
	for _, def := range defaults {
		if !userKeys[def.key] {
			out = append(out, def.args...)
		}
	}
	for _, option := range options {
		if strings.TrimSpace(option) != "" {
			out = append(out, option)
		}
	}
	return out
}

func fileSyncSSHOptionKeys(options []string) map[string]bool {
	keys := map[string]bool{}
	for i := 0; i < len(options); i++ {
		option := strings.TrimSpace(options[i])
		var spec string
		switch {
		case option == "-o" && i+1 < len(options):
			spec = options[i+1]
			i++
		case strings.HasPrefix(option, "-o "):
			spec = strings.TrimSpace(strings.TrimPrefix(option, "-o "))
		case strings.HasPrefix(option, "-o") && len(option) > len("-o"):
			spec = strings.TrimSpace(strings.TrimPrefix(option, "-o"))
		default:
			continue
		}
		if key := fileSyncSSHOptionKey(spec); key != "" {
			keys[key] = true
		}
	}
	return keys
}

func fileSyncSSHOptionKey(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ""
	}
	cut := len(spec)
	for _, sep := range []string{"=", " ", "\t"} {
		if idx := strings.Index(spec, sep); idx >= 0 && idx < cut {
			cut = idx
		}
	}
	return strings.ToLower(strings.TrimSpace(spec[:cut]))
}

func fileSyncTargetPath(source fileSyncSource, target fileSyncTarget, sourceCount int) string {
	if strings.TrimSpace(target.Path) == "" {
		return source.Path
	}
	if sourceCount == 1 {
		return target.Path
	}
	return path.Join(target.Path, filepath.Base(source.Path))
}

func fileSyncDestination(target fileSyncTarget, remotePath string) string {
	host := target.Host
	if target.User != "" {
		host = target.User + "@" + host
	}
	return host + ":" + remotePath
}

func fileSyncShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
