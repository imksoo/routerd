package render

import (
	"fmt"
	"strconv"
	"strings"

	"routerd/pkg/api"
)

func SystemdUnit(name string, spec api.SystemdUnitSpec) []byte {
	unitName := spec.UnitName
	if unitName == "" {
		unitName = name
	}
	description := spec.Description
	if description == "" {
		description = "routerd managed " + unitName
	}
	after := spec.After
	if len(after) == 0 {
		after = []string{"network-online.target"}
	}
	wants := spec.Wants
	if len(wants) == 0 {
		wants = []string{"network-online.target"}
	}
	wantedBy := spec.WantedBy
	if len(wantedBy) == 0 {
		wantedBy = []string{"multi-user.target"}
	}
	restart := spec.Restart
	if restart == "" {
		restart = "always"
	}
	restartSec := spec.RestartSec
	if restartSec == "" {
		restartSec = "5s"
	}
	protectSystem := spec.ProtectSystem
	if protectSystem == "" {
		protectSystem = "no"
	}
	protectHome := spec.ProtectHome
	if protectHome == "" {
		protectHome = "yes"
	}
	noNewPrivileges := api.BoolDefault(spec.NoNewPrivileges, true)
	privateTmp := api.BoolDefault(spec.PrivateTmp, true)

	var b strings.Builder
	b.WriteString("# Managed by routerd. Do not edit by hand.\n")
	b.WriteString("[Unit]\n")
	b.WriteString("Description=" + systemdValue(description) + "\n")
	if len(after) > 0 {
		b.WriteString("After=" + strings.Join(after, " ") + "\n")
	}
	if len(wants) > 0 {
		b.WriteString("Wants=" + strings.Join(wants, " ") + "\n")
	}
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	if len(spec.Environment) > 0 {
		for _, env := range spec.Environment {
			b.WriteString("Environment=" + strconv.Quote(env) + "\n")
		}
	}
	if spec.User != "" {
		b.WriteString("User=" + spec.User + "\n")
	}
	if spec.Group != "" {
		b.WriteString("Group=" + spec.Group + "\n")
	}
	if spec.WorkingDirectory != "" {
		b.WriteString("WorkingDirectory=" + spec.WorkingDirectory + "\n")
	}
	b.WriteString("ExecStart=" + quoteCommand(spec.ExecStart) + "\n")
	b.WriteString("Restart=" + restart + "\n")
	b.WriteString("RestartSec=" + restartSec + "\n")
	writeSpaceList(&b, "RuntimeDirectory", spec.RuntimeDirectory)
	if spec.RuntimeDirectoryPreserve != "" {
		b.WriteString("RuntimeDirectoryPreserve=" + spec.RuntimeDirectoryPreserve + "\n")
	}
	writeSpaceList(&b, "StateDirectory", spec.StateDirectory)
	writeSpaceList(&b, "LogsDirectory", spec.LogsDirectory)
	writeSpaceList(&b, "ReadWritePaths", spec.ReadWritePaths)
	b.WriteString("NoNewPrivileges=" + yesNo(noNewPrivileges) + "\n")
	b.WriteString("PrivateTmp=" + yesNo(privateTmp) + "\n")
	b.WriteString("ProtectHome=" + protectHome + "\n")
	b.WriteString("ProtectSystem=" + protectSystem + "\n")
	writeSpaceList(&b, "RestrictAddressFamilies", spec.RestrictAddressFamilies)
	writeSpaceList(&b, "CapabilityBoundingSet", spec.CapabilityBoundingSet)
	writeSpaceList(&b, "AmbientCapabilities", spec.AmbientCapabilities)
	b.WriteString("\n[Install]\n")
	if len(wantedBy) > 0 {
		b.WriteString("WantedBy=" + strings.Join(wantedBy, " ") + "\n")
	}
	return []byte(b.String())
}

func quoteCommand(args []string) string {
	var quoted []string
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t\"'\\") {
			quoted = append(quoted, strconv.Quote(arg))
			continue
		}
		quoted = append(quoted, arg)
	}
	return strings.Join(quoted, " ")
}

func writeSpaceList(b *strings.Builder, key string, values []string) {
	if len(values) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("%s=%s\n", key, strings.Join(values, " ")))
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func systemdValue(value string) string {
	return strings.ReplaceAll(value, "\n", " ")
}
