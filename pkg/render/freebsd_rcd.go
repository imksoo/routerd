package render

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"routerd/pkg/api"
)

func freeBSDRCDScripts(router *api.Router) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "SystemdUnit" {
			continue
		}
		spec, err := res.SystemdUnitSpec()
		if err != nil {
			return nil, err
		}
		if defaultString(spec.State, "present") == "absent" {
			continue
		}
		name := freeBSDServiceName(defaultString(spec.UnitName, res.Metadata.Name))
		data, err := FreeBSDRCDScript(name, spec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
	return out, nil
}

func FreeBSDRCDScript(name string, spec api.SystemdUnitSpec) ([]byte, error) {
	if len(spec.ExecStart) == 0 {
		return nil, fmt.Errorf("execStart is required")
	}
	name = freeBSDServiceName(name)
	var buf bytes.Buffer
	buf.WriteString("#!/bin/sh\n")
	buf.WriteString("#\n")
	buf.WriteString("# PROVIDE: " + name + "\n")
	buf.WriteString("# REQUIRE: NETWORKING\n")
	buf.WriteString("# KEYWORD: shutdown\n")
	buf.WriteString("\n")
	buf.WriteString(". /etc/rc.subr\n\n")
	buf.WriteString("name=" + shellSingleQuote(name) + "\n")
	buf.WriteString("rcvar=\"${name}_enable\"\n")
	buf.WriteString("pidfile=\"/var/run/${name}.pid\"\n")
	buf.WriteString("command=\"/usr/sbin/daemon\"\n")
	buf.WriteString("procname=" + shellSingleQuote(spec.ExecStart[0]) + "\n")
	buf.WriteString("command_args=\"-P ${pidfile} -r -f --")
	for _, arg := range spec.ExecStart {
		buf.WriteString(" " + shellSingleQuote(arg))
	}
	buf.WriteString("\"\n")
	if len(spec.RuntimeDirectory) > 0 || len(spec.StateDirectory) > 0 || len(spec.LogsDirectory) > 0 {
		buf.WriteString("start_precmd=\"${name}_prestart\"\n")
	}
	buf.WriteString("\n")
	if len(spec.RuntimeDirectory) > 0 || len(spec.StateDirectory) > 0 || len(spec.LogsDirectory) > 0 {
		buf.WriteString("${name}_prestart() {\n")
		for _, dir := range freeBSDServiceDirs("/var/run", spec.RuntimeDirectory) {
			buf.WriteString("  mkdir -p " + shellSingleQuote(dir) + "\n")
		}
		for _, dir := range freeBSDServiceDirs("/var/db", spec.StateDirectory) {
			buf.WriteString("  mkdir -p " + shellSingleQuote(dir) + "\n")
		}
		for _, dir := range freeBSDServiceDirs("/var/log", spec.LogsDirectory) {
			buf.WriteString("  mkdir -p " + shellSingleQuote(dir) + "\n")
		}
		buf.WriteString("}\n\n")
	}
	buf.WriteString("load_rc_config $name\n")
	buf.WriteString(": ${" + name + "_enable:=\"")
	if api.BoolDefault(spec.Enabled, true) {
		buf.WriteString("YES")
	} else {
		buf.WriteString("NO")
	}
	buf.WriteString("\"}\n")
	buf.WriteString("run_rc_command \"$1\"\n")
	return buf.Bytes(), nil
}

func freeBSDServiceDirs(root string, values []string) []string {
	var out []string
	for _, value := range values {
		value = strings.Trim(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		out = append(out, root+"/"+value)
	}
	sort.Strings(out)
	return out
}

var unsafeFreeBSDServiceName = regexp.MustCompile(`[^A-Za-z0-9_]+`)

func freeBSDServiceName(value string) string {
	value = strings.TrimSuffix(value, ".service")
	value = unsafeFreeBSDServiceName.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "routerd_service"
	}
	return value
}

func sortedByteMapKeys(values map[string][]byte) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
