package dohproxy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/dohproxy"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Controller struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	DryRun bool
	Binary string
}

func (c Controller) Start(ctx context.Context) {
	_ = c.Reconcile(ctx)
}

type runningProxy struct {
	process *exec.Cmd
	spec    api.DoHProxySpec
}

var (
	runningMu      sync.Mutex
	runningProxies = map[string]runningProxy{}
)

func (c Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DoHProxy" {
			continue
		}
		spec, err := resource.DoHProxySpec()
		if err != nil {
			return err
		}
		spec = dohproxy.NormalizeSpec(spec)
		phase := "Applied"
		if !c.DryRun {
			if err := c.ensureRunning(ctx, resource.Metadata.Name, spec); err != nil {
				phase = "Pending"
				if err := c.saveStatus(resource.Metadata.Name, spec, phase, err.Error()); err != nil {
					return err
				}
				return err
			}
		}
		if err := c.saveStatus(resource.Metadata.Name, spec, phase, ""); err != nil {
			return err
		}
		if c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "routerd.doh-proxy.configured", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DoHProxy", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"backend": spec.Backend, "listenAddress": spec.ListenAddress}
			_ = c.Bus.Publish(ctx, event)
		}
	}
	return nil
}

func (c Controller) ensureRunning(ctx context.Context, name string, spec api.DoHProxySpec) error {
	runningMu.Lock()
	defer runningMu.Unlock()
	if current, ok := runningProxies[name]; ok && processAlive(current.process.Process) && sameProxySpec(current.spec, spec) {
		return nil
	}
	if current, ok := runningProxies[name]; ok && current.process.Process != nil {
		_ = current.process.Process.Signal(syscall.SIGTERM)
		delete(runningProxies, name)
	}
	binary := c.Binary
	if binary == "" {
		binary = "/usr/local/sbin/routerd-doh-proxy"
	}
	args := []string{
		"daemon",
		"--resource", name,
		"--backend", spec.Backend,
		"--listen-address", spec.ListenAddress,
		"--listen-port", strconv.Itoa(spec.ListenPort),
		"--upstream", joinCSV(spec.Upstreams),
		"--socket", firstNonEmpty(spec.SocketPath, filepath.Join("/run/routerd/doh-proxy", name+".sock")),
		"--state-file", firstNonEmpty(spec.StateFile, filepath.Join("/var/lib/routerd/doh-proxy", name, "state.json")),
		"--event-file", firstNonEmpty(spec.EventFile, filepath.Join("/var/lib/routerd/doh-proxy", name, "events.jsonl")),
	}
	if spec.Command != "" {
		args = append(args, "--command", spec.Command)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	runningProxies[name] = runningProxy{process: cmd, spec: spec}
	go func() {
		err := cmd.Wait()
		runningMu.Lock()
		if current, ok := runningProxies[name]; ok && current.process == cmd {
			delete(runningProxies, name)
		}
		runningMu.Unlock()
		_ = err
	}()
	return nil
}

func (c Controller) saveStatus(name string, spec api.DoHProxySpec, phase, message string) error {
	status := map[string]any{
		"phase":         phase,
		"backend":       spec.Backend,
		"listenAddress": spec.ListenAddress,
		"listenPort":    spec.ListenPort,
		"updatedAt":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if message != "" {
		status["message"] = message
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "DoHProxy", name, status)
}

func processAlive(process *os.Process) bool {
	if process == nil {
		return false
	}
	err := process.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}

func sameProxySpec(a, b api.DoHProxySpec) bool {
	return a.Backend == b.Backend && a.ListenAddress == b.ListenAddress && a.ListenPort == b.ListenPort && joinCSV(a.Upstreams) == joinCSV(b.Upstreams) && a.Command == b.Command
}

func joinCSV(values []string) string {
	out := ""
	for _, value := range values {
		if value == "" {
			continue
		}
		if out != "" {
			out += ","
		}
		out += value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
