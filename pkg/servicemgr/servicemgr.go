// SPDX-License-Identifier: BSD-3-Clause

package servicemgr

import (
	"regexp"
	"strings"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resource"
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

type Service struct {
	Name                string
	SystemdName         string
	SystemdArtifactKind string
	RCDName             string
}

type Manager interface {
	Name() string
	ArtifactKind() string
	ApplyWith() string
	ServiceName(Service) string
	Command(Operation, Service) Command
	Intent(owner string, service Service, action string, attrs map[string]string) resource.Intent
}

func ForPlatform(features platform.Features) Manager {
	switch {
	case features.HasRCD:
		return RCD{}
	default:
		return Systemd{}
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
	return stringutil.FirstNonBlank(s.SystemdName, s.Name)
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
func (m Systemd) Intent(owner string, service Service, action string, attrs map[string]string) resource.Intent {
	return serviceIntent(owner, stringutil.FirstNonBlank(service.SystemdArtifactKind, m.ArtifactKind()), m.ServiceName(service), action, m.ApplyWith(), attrs)
}

type RCD struct{}

func (RCD) Name() string         { return "rc.d" }
func (RCD) ArtifactKind() string { return "rc.d.service" }
func (RCD) ApplyWith() string    { return "service" }
func (m RCD) ServiceName(s Service) string {
	return normalizeRCServiceName(stringutil.FirstNonBlank(s.RCDName, s.SystemdName, s.Name))
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
func (m RCD) Intent(owner string, service Service, action string, attrs map[string]string) resource.Intent {
	return serviceIntent(owner, m.ArtifactKind(), m.ServiceName(service), action, m.ApplyWith(), attrs)
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
