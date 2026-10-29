// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type wireGuardStore interface {
	ReadObjectStatus(string, string, string) (map[string]any, error)
	SaveObjectStatus(string, string, string, map[string]any) error
	ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	DeleteObject(string, string, string) error
}

const wireGuardNativeStatusName = "wireguard-native"

type wireGuardNativeRecord struct {
	Resource string      `json:"resource"`
	Name     string      `json:"name"`
	Token    string      `json:"token"`
	OS       platform.OS `json:"os"`
	IfIndex  int         `json:"ifindex"`
	Pending  bool        `json:"pending"`
	Ports    []int       `json:"ports"`
}

type nativeWireGuardLink struct {
	Exists  bool
	IfIndex int
	Token   string
	Kind    string
}

func (c WireGuardController) currentOS() platform.OS {
	if c.OS != "" {
		return c.OS
	}
	return platform.CurrentOS()
}

func (c WireGuardController) readNativeWireGuard() ([]wireGuardNativeRecord, error) {
	status, err := c.Store.ReadObjectStatus(api.RouterAPIVersion, "Router", wireGuardNativeStatusName)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, nil
	}
	var records []wireGuardNativeRecord
	switch entries := status["interfaces"].(type) {
	case []wireGuardNativeRecord:
		records = append([]wireGuardNativeRecord(nil), entries...)
		for i := range records {
			records[i].Ports = append([]int(nil), records[i].Ports...)
		}
	case []any:
		for _, entry := range entries {
			fields, ok := entry.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid WireGuard native record")
			}
			record := wireGuardNativeRecord{}
			for key, target := range map[string]*string{"resource": &record.Resource, "name": &record.Name, "token": &record.Token} {
				value, ok := fields[key].(string)
				if !ok {
					return nil, fmt.Errorf("invalid WireGuard native %s", key)
				}
				*target = value
			}
			osName, ok := fields["os"].(string)
			if !ok {
				return nil, fmt.Errorf("invalid WireGuard native OS")
			}
			record.OS = platform.OS(osName)
			index, ok := fields["ifindex"].(float64)
			if !ok || index != math.Trunc(index) || index < 0 || index > math.MaxInt32 {
				return nil, fmt.Errorf("invalid WireGuard native interface index")
			}
			record.IfIndex = int(index)
			record.Pending, ok = fields["pending"].(bool)
			if !ok {
				return nil, fmt.Errorf("invalid WireGuard native pending state")
			}
			ports, ok := fields["ports"].([]any)
			if !ok {
				return nil, fmt.Errorf("invalid WireGuard native ports")
			}
			for _, item := range ports {
				port, ok := item.(float64)
				if !ok || port != math.Trunc(port) || port < 1 || port > 65535 {
					return nil, fmt.Errorf("invalid WireGuard native port")
				}
				record.Ports = append(record.Ports, int(port))
			}
			records = append(records, record)
		}
	default:
		return nil, fmt.Errorf("invalid WireGuard native journal")
	}
	seen := map[string]bool{}
	for _, record := range records {
		if record.Name == "" || record.Resource == "" || len(record.Token) != 32 || record.OS != platform.OSLinux && record.OS != platform.OSFreeBSD || seen[record.Name] || record.IfIndex < 0 || record.OS == platform.OSLinux && !record.Pending && record.IfIndex == 0 {
			return nil, fmt.Errorf("invalid WireGuard native identity")
		}
		if _, err := hex.DecodeString(record.Token); err != nil {
			return nil, fmt.Errorf("invalid WireGuard native owner token")
		}
		seen[record.Name] = true
		for _, port := range record.Ports {
			if port < 1 || port > 65535 {
				return nil, fmt.Errorf("invalid WireGuard native port")
			}
		}
	}
	return records, nil
}

func (c WireGuardController) saveNativeWireGuard(records []wireGuardNativeRecord) error {
	if records == nil {
		records = []wireGuardNativeRecord{}
	}
	for i := range records {
		if records[i].Ports == nil {
			records[i].Ports = []int{}
		}
	}
	return c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", wireGuardNativeStatusName, map[string]any{"interfaces": records})
}

func (c WireGuardController) saveNativeWireGuardRecord(record wireGuardNativeRecord) error {
	records, err := c.readNativeWireGuard()
	if err != nil {
		return err
	}
	found := false
	for i, previous := range records {
		if previous.Name == record.Name {
			records[i], found = record, true
			break
		}
	}
	if !found {
		records = append(records, record)
	}
	return c.saveNativeWireGuard(records)
}

func (c WireGuardController) observeNativeWireGuard(ctx context.Context, name string) (nativeWireGuardLink, error) {
	switch c.currentOS() {
	case platform.OSLinux:
		out, err := c.runWireGuardHostCommand(ctx, "ip", "-j", "-d", "link", "show")
		if err != nil {
			return nativeWireGuardLink{}, err
		}
		var links []struct {
			Name  string `json:"ifname"`
			Index int    `json:"ifindex"`
			Alias string `json:"ifalias"`
			Info  struct {
				Kind string `json:"info_kind"`
			} `json:"linkinfo"`
		}
		if err := json.Unmarshal(out, &links); err != nil {
			return nativeWireGuardLink{}, fmt.Errorf("decode native WireGuard links: %w", err)
		}
		if links == nil {
			return nativeWireGuardLink{}, fmt.Errorf("native link inventory is not an array")
		}
		for _, link := range links {
			if link.Name == "" || link.Index < 1 {
				return nativeWireGuardLink{}, fmt.Errorf("native link inventory lacks interface identity")
			}
			if link.Name == name {
				token, owned := strings.CutPrefix(link.Alias, "routerd-wg:")
				if !owned {
					token = ""
				}
				return nativeWireGuardLink{Exists: true, IfIndex: link.Index, Token: token, Kind: link.Info.Kind}, nil
			}
		}
		return nativeWireGuardLink{}, nil
	case platform.OSFreeBSD:
		out, err := c.runWireGuardHostCommand(ctx, "ifconfig", "-l")
		if err != nil {
			return nativeWireGuardLink{}, err
		}
		found := false
		for _, candidate := range strings.Fields(string(out)) {
			found = found || candidate == name
		}
		if !found {
			return nativeWireGuardLink{}, nil
		}
		out, err = c.runWireGuardHostCommand(ctx, "ifconfig", name)
		if err != nil {
			return nativeWireGuardLink{}, err
		}
		if !strings.HasPrefix(strings.TrimSpace(string(out)), name+":") {
			return nativeWireGuardLink{}, fmt.Errorf("native interface observation lacks %s identity", name)
		}
		link := nativeWireGuardLink{Exists: true}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if value, ok := strings.CutPrefix(line, "description: "); ok {
				var owned bool
				link.Token, owned = strings.CutPrefix(value, "routerd-wg:")
				if !owned {
					link.Token = ""
				}
			}
			if value, ok := strings.CutPrefix(line, "groups: "); ok {
				for _, group := range strings.Fields(value) {
					if group == "wg" {
						link.Kind = "wireguard"
					}
				}
			}
		}
		return link, nil
	default:
		return nativeWireGuardLink{}, fmt.Errorf("WireGuard native lifecycle unsupported on %s", c.currentOS())
	}
}

func (c WireGuardController) verifyNativeWireGuard(record wireGuardNativeRecord, observed nativeWireGuardLink) error {
	if record.OS != c.currentOS() {
		return fmt.Errorf("WireGuard ownership belongs to %s, current platform is %s", record.OS, c.currentOS())
	}
	if !observed.Exists {
		return nil
	}
	if observed.Kind != "wireguard" || observed.Token != record.Token || record.IfIndex > 0 && observed.IfIndex != record.IfIndex {
		return fmt.Errorf("WireGuard interface %s exists without matching native ownership", record.Name)
	}
	return nil
}

func (c WireGuardController) ensureNativeWireGuard(ctx context.Context, resource, name string) (wireGuardNativeRecord, error) {
	records, err := c.readNativeWireGuard()
	if err != nil {
		return wireGuardNativeRecord{}, err
	}
	var record wireGuardNativeRecord
	for _, previous := range records {
		if previous.Name == name {
			record = previous
			break
		}
	}
	observed, err := c.observeNativeWireGuard(ctx, name)
	if err != nil {
		return record, err
	}
	if observed.Exists {
		if record.Resource != resource {
			return record, fmt.Errorf("WireGuard interface %s is not owned by this resource", name)
		}
		if err := c.verifyNativeWireGuard(record, observed); err != nil {
			return record, err
		}
		if record.IfIndex == 0 && observed.IfIndex > 0 {
			record.IfIndex = observed.IfIndex
			if err := c.saveNativeWireGuardRecord(record); err != nil {
				return record, err
			}
		}
		return record, nil
	}
	if record.Name == "" {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return record, err
		}
		record = wireGuardNativeRecord{Resource: resource, Name: name, Token: hex.EncodeToString(token[:]), OS: c.currentOS(), Pending: true, Ports: []int{}}
	} else if record.Resource != resource || record.OS != c.currentOS() {
		return record, fmt.Errorf("WireGuard pending creation has a different owner")
	}
	record.Pending, record.IfIndex = true, 0
	if err := c.saveNativeWireGuardRecord(record); err != nil {
		return record, err
	}
	var createErr error
	if c.currentOS() == platform.OSLinux {
		_, createErr = c.runWireGuardHostCommand(ctx, "ip", "link", "add", "name", name, "alias", "routerd-wg:"+record.Token, "type", "wireguard")
	} else {
		_, createErr = c.runWireGuardHostCommand(ctx, "ifconfig", "wg", "create", "name", name, "description", "routerd-wg:"+record.Token)
	}
	observed, err = c.observeNativeWireGuard(ctx, name)
	if err != nil {
		return record, errors.Join(createErr, err)
	}
	if !observed.Exists {
		return record, errors.Join(createErr, fmt.Errorf("WireGuard interface %s absent after create", name))
	}
	if err := c.verifyNativeWireGuard(record, observed); err != nil {
		return record, errors.Join(createErr, err)
	}
	record.IfIndex = observed.IfIndex
	if err := c.saveNativeWireGuardRecord(record); err != nil {
		return record, errors.Join(createErr, err)
	}
	return record, createErr
}

func (c WireGuardController) cleanupNativeWireGuard(ctx context.Context, active map[string]string) error {
	if c.DryRun {
		return nil
	}
	records, err := c.readNativeWireGuard()
	if err != nil {
		return err
	}
	for _, record := range records {
		if active[record.Resource] == record.Name {
			continue
		}
		observed, err := c.observeNativeWireGuard(ctx, record.Name)
		if err != nil {
			return err
		}
		if err := c.verifyNativeWireGuard(record, observed); err != nil {
			return err
		}
		if observed.Exists {
			record.Pending = true
			if err := c.saveNativeWireGuardRecord(record); err != nil {
				return err
			}
			var err error
			if c.currentOS() == platform.OSLinux {
				_, err = c.runWireGuardHostCommand(ctx, "ip", "link", "delete", "dev", record.Name)
			} else {
				_, err = c.runWireGuardHostCommand(ctx, "ifconfig", record.Name, "destroy")
			}
			if err != nil {
				return err
			}
			observed, err = c.observeNativeWireGuard(ctx, record.Name)
			if err != nil {
				return err
			}
			if observed.Exists {
				return fmt.Errorf("WireGuard interface %s remains after native destroy", record.Name)
			}
		}
		for _, port := range record.Ports {
			if err := c.deleteOwnedWireGuardInputAccept(ctx, port, record.Token); err != nil {
				return err
			}
		}
		current, err := c.readNativeWireGuard()
		if err != nil {
			return err
		}
		remaining := make([]wireGuardNativeRecord, 0, len(current))
		for _, other := range current {
			if other.Name != record.Name {
				remaining = append(remaining, other)
			}
		}
		if err := c.saveNativeWireGuard(remaining); err != nil {
			return err
		}
	}
	return nil
}

func (c WireGuardController) reconcileNativeWireGuardPorts(ctx context.Context, record wireGuardNativeRecord, port int) (wireGuardNativeRecord, error) {
	if c.currentOS() != platform.OSLinux {
		return record, nil
	}
	wanted := false
	for _, existing := range record.Ports {
		wanted = wanted || existing == port
	}
	if port > 0 && !wanted {
		record.Ports = append(append([]int(nil), record.Ports...), port)
		record.Pending = true
		if err := c.saveNativeWireGuardRecord(record); err != nil {
			return record, err
		}
	}
	if port > 0 {
		if err := c.ensureOwnedWireGuardInputAccept(ctx, port, record.Token); err != nil {
			return record, err
		}
	}
	for _, previous := range record.Ports {
		if previous != port {
			if err := c.deleteOwnedWireGuardInputAccept(ctx, previous, record.Token); err != nil {
				return record, err
			}
		}
	}
	record.Ports = []int{}
	if port > 0 {
		record.Ports = []int{port}
	}
	return record, nil
}

func wireGuardInputRule(action string, port int, token string) []string {
	args := []string{action, "INPUT"}
	if action == "-I" {
		args = append(args, "1")
	}
	return append(args, "-p", "udp", "--dport", fmt.Sprint(port), "-m", "comment", "--comment", "routerd-wg:"+token, "-j", "ACCEPT")
}

func (c WireGuardController) ensureOwnedWireGuardInputAccept(ctx context.Context, port int, token string) error {
	if out, err := c.runWireGuardHostCommand(ctx, "iptables", wireGuardInputRule("-C", port, token)...); err == nil {
		return nil
	} else if !wireGuardIPTablesRuleMissing(out, err) {
		return fmt.Errorf("observe owned WireGuard input rule: %w", err)
	}
	if _, err := c.runWireGuardHostCommand(ctx, "iptables", wireGuardInputRule("-I", port, token)...); err != nil {
		return fmt.Errorf("install owned WireGuard input rule: %w", err)
	}
	return nil
}

func (c WireGuardController) deleteOwnedWireGuardInputAccept(ctx context.Context, port int, token string) error {
	if c.currentOS() != platform.OSLinux {
		return nil
	}
	if out, err := c.runWireGuardHostCommand(ctx, "iptables", wireGuardInputRule("-D", port, token)...); err != nil && !wireGuardIPTablesRuleMissing(out, err) {
		return fmt.Errorf("withdraw owned WireGuard input rule: %w", err)
	}
	return nil
}
