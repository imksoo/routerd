// SPDX-License-Identifier: BSD-3-Clause

package servicemgr

import (
	"fmt"
	"regexp"
	"strings"

	"routerd/pkg/platform"
	"routerd/pkg/resource"
)

type Operation string

const (
	OperationEnable  Operation = "enable"
	OperationReload  Operation = "reload"
	OperationRestart Operation = "restart"
	OperationStatus  Operation = "status"
)

type Command struct {
	Name string
	Args []string
}

type Hook struct {
	Operation      Operation
	Command        Command
	BeforeDefault  bool
	ReplaceDefault bool
}

type Plan struct {
	Operation Operation
	Commands  []Command
}

type Service struct {
	Name                string
	SystemdName         string
	SystemdArtifactKind string
	OpenRCName          string
	RCDName             string
	NixName             string
}

func BeforeDefaultHook(op Operation, command Command) Hook {
	return Hook{Operation: op, Command: command, BeforeDefault: true}
}

func ReplaceDefaultHook(op Operation, command Command) Hook {
	return Hook{Operation: op, Command: command, ReplaceDefault: true}
}

func FRRLiveReloadHooks(configPath, vtysh, reload string) []Hook {
	configPath = firstNonEmpty(configPath, "/run/routerd/frr/routerd.conf")
	vtysh = firstNonEmpty(vtysh, "vtysh")
	reload = firstNonEmpty(reload, "frr-reload.py")
	return []Hook{
		BeforeDefaultHook(OperationReload, Command{Name: vtysh, Args: []string{"-C", "-f", configPath}}),
		ReplaceDefaultHook(OperationReload, Command{Name: reload, Args: []string{"--reload", configPath}}),
	}
}

func PIDSignalHook(op Operation, signal, pidPath string) Hook {
	signal = strings.TrimPrefix(firstNonEmpty(signal, "HUP"), "-")
	return ReplaceDefaultHook(op, Command{Name: "sh", Args: []string{"-c", fmt.Sprintf("kill -%s \"$(cat %s)\"", signal, pidPath)}})
}

func DaemonAPICommandHook(op Operation, socketPath, command string) Hook {
	return ReplaceDefaultHook(op, Command{Name: "daemonapi", Args: []string{"POST", "unix://" + strings.TrimSpace(socketPath), "/v1/commands/" + strings.TrimSpace(command)}})
}

type Manager interface {
	Name() string
	ArtifactKind() string
	ApplyWith() string
	ServiceName(Service) string
	Command(Operation, Service) Command
	Plan(Operation, Service, ...Hook) Plan
	Intent(owner string, service Service, action string, attrs map[string]string) resource.Intent
}

func ForPlatform(features platform.Features) Manager {
	switch {
	case features.HasOpenRC:
		return OpenRC{}
	case features.HasRCD:
		return RCD{}
	case features.HasSystemd:
		return Systemd{}
	default:
		return NixOS{}
	}
}

func IntentForPlatform(owner string, features platform.Features, service Service, action string, attrs map[string]string) resource.Intent {
	return ForPlatform(features).Intent(owner, service, action, attrs)
}

type Systemd struct{}

func (Systemd) Name() string         { return "systemd" }
func (Systemd) ArtifactKind() string { return "systemd.service" }
func (Systemd) ApplyWith() string    { return "systemctl" }
func (m Systemd) ServiceName(s Service) string {
	return firstNonEmpty(s.SystemdName, s.Name)
}
func (m Systemd) Command(op Operation, s Service) Command {
	name := m.ServiceName(s)
	switch op {
	case OperationEnable:
		return Command{Name: "systemctl", Args: []string{"enable", name}}
	case OperationReload:
		return Command{Name: "systemctl", Args: []string{"reload", name}}
	case OperationRestart:
		return Command{Name: "systemctl", Args: []string{"restart", name}}
	case OperationStatus:
		return Command{Name: "systemctl", Args: []string{"is-active", "--quiet", name}}
	default:
		return Command{}
	}
}
func (m Systemd) Plan(op Operation, s Service, hooks ...Hook) Plan {
	return operationPlan(op, m.Command(op, s), hooks...)
}
func (m Systemd) Intent(owner string, service Service, action string, attrs map[string]string) resource.Intent {
	return serviceIntent(owner, firstNonEmpty(service.SystemdArtifactKind, m.ArtifactKind()), m.ServiceName(service), action, m.ApplyWith(), attrs)
}

type OpenRC struct{}

func (OpenRC) Name() string         { return "openrc" }
func (OpenRC) ArtifactKind() string { return "openrc.service" }
func (OpenRC) ApplyWith() string    { return "rc-service" }
func (m OpenRC) ServiceName(s Service) string {
	return normalizeRCServiceName(firstNonEmpty(s.OpenRCName, s.SystemdName, s.Name))
}
func (m OpenRC) Command(op Operation, s Service) Command {
	name := m.ServiceName(s)
	switch op {
	case OperationEnable:
		return Command{Name: "rc-update", Args: []string{"add", name, "default"}}
	case OperationReload:
		return Command{Name: "rc-service", Args: []string{name, "reload"}}
	case OperationRestart:
		return Command{Name: "rc-service", Args: []string{name, "restart"}}
	case OperationStatus:
		return Command{Name: "rc-service", Args: []string{name, "status"}}
	default:
		return Command{}
	}
}
func (m OpenRC) Plan(op Operation, s Service, hooks ...Hook) Plan {
	return operationPlan(op, m.Command(op, s), hooks...)
}
func (m OpenRC) Intent(owner string, service Service, action string, attrs map[string]string) resource.Intent {
	return serviceIntent(owner, m.ArtifactKind(), m.ServiceName(service), action, m.ApplyWith(), attrs)
}

type RCD struct{}

func (RCD) Name() string         { return "rc.d" }
func (RCD) ArtifactKind() string { return "rc.d.service" }
func (RCD) ApplyWith() string    { return "service" }
func (m RCD) ServiceName(s Service) string {
	return normalizeRCServiceName(firstNonEmpty(s.RCDName, s.SystemdName, s.Name))
}
func (m RCD) Command(op Operation, s Service) Command {
	name := m.ServiceName(s)
	switch op {
	case OperationEnable:
		return Command{Name: "sysrc", Args: []string{name + "_enable=YES"}}
	case OperationReload:
		return Command{Name: "service", Args: []string{name, "reload"}}
	case OperationRestart:
		return Command{Name: "service", Args: []string{name, "restart"}}
	case OperationStatus:
		return Command{Name: "service", Args: []string{name, "status"}}
	default:
		return Command{}
	}
}
func (m RCD) Plan(op Operation, s Service, hooks ...Hook) Plan {
	return operationPlan(op, m.Command(op, s), hooks...)
}
func (m RCD) Intent(owner string, service Service, action string, attrs map[string]string) resource.Intent {
	return serviceIntent(owner, m.ArtifactKind(), m.ServiceName(service), action, m.ApplyWith(), attrs)
}

type NixOS struct{}

func (NixOS) Name() string         { return "nixos" }
func (NixOS) ArtifactKind() string { return "nixos.service" }
func (NixOS) ApplyWith() string    { return "nixos-module" }
func (m NixOS) ServiceName(s Service) string {
	name := firstNonEmpty(s.NixName, s.SystemdName, s.Name)
	return strings.TrimSuffix(name, ".service")
}
func (m NixOS) Command(op Operation, s Service) Command {
	name := m.ServiceName(s)
	switch op {
	case OperationEnable, OperationReload, OperationRestart:
		return Command{Name: "nixos-rebuild", Args: []string{"switch"}}
	case OperationStatus:
		return Command{Name: "systemctl", Args: []string{"is-active", "--quiet", name + ".service"}}
	default:
		return Command{}
	}
}
func (m NixOS) Plan(op Operation, s Service, hooks ...Hook) Plan {
	return operationPlan(op, m.Command(op, s), hooks...)
}
func (m NixOS) Intent(owner string, service Service, action string, attrs map[string]string) resource.Intent {
	return serviceIntent(owner, m.ArtifactKind(), m.ServiceName(service), action, m.ApplyWith(), attrs)
}

func operationPlan(op Operation, defaultCommand Command, hooks ...Hook) Plan {
	var before, after []Command
	replaceDefault := false
	for _, hook := range hooks {
		if hook.Operation != op || hook.Command.Name == "" {
			continue
		}
		if hook.ReplaceDefault {
			replaceDefault = true
		}
		if hook.BeforeDefault {
			before = append(before, hook.Command)
		} else {
			after = append(after, hook.Command)
		}
	}
	commands := append([]Command(nil), before...)
	if !replaceDefault && defaultCommand.Name != "" {
		commands = append(commands, defaultCommand)
	}
	commands = append(commands, after...)
	return Plan{Operation: op, Commands: commands}
}

func serviceIntent(owner, kind, name, action, applyWith string, attrs map[string]string) resource.Intent {
	if attrs == nil {
		attrs = map[string]string{}
	}
	return resource.Intent{
		Artifact:  resource.Artifact{Kind: kind, Name: name, Owner: owner, Attributes: attrs},
		Action:    action,
		ApplyWith: applyWith,
	}
}

var unsafeServiceName = regexp.MustCompile(`[^A-Za-z0-9_]+`)

func normalizeRCServiceName(value string) string {
	value = strings.TrimSuffix(strings.TrimSpace(value), ".service")
	value = unsafeServiceName.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "routerd_service"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
