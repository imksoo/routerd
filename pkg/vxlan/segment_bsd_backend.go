// SPDX-License-Identifier: BSD-3-Clause

package vxlan

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/subprocessio"
)

type freeBSDSegmentNative struct {
	scope      func(context.Context) (string, error)
	command    CommandRunner
	interfaces func() ([]net.Interface, error)
	readConfig func(context.Context, string, int) (bsdSegmentConfig, error)
}

type bsdSegmentInterface struct {
	link      SegmentLink
	addresses []netip.Addr
	members   []string
}

type bsdSegmentSnapshot struct {
	scope         string
	links         map[string]*bsdSegmentInterface
	before, after map[string]net.Interface
	readConfig    func(context.Context, string, int) (bsdSegmentConfig, error)
	ctx           context.Context
}

func (n freeBSDSegmentNative) KernelScope(ctx context.Context) (string, error) { return n.scope(ctx) }

func newFreeBSDSegmentNative(scope func(context.Context) (string, error)) SegmentNative {
	return freeBSDSegmentNative{scope: scope, command: segmentHostCommand, interfaces: net.Interfaces, readConfig: readFreeBSDSegmentConfig}
}

func segmentHostCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.WaitDelay = time.Second
	stdout := subprocessio.NewCapture(subprocessio.StdoutLimit)
	stderr := subprocessio.NewCapture(subprocessio.StderrLimit)
	command.Stdout, command.Stderr = stdout, stderr
	err := subprocessio.Run(command)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if limit := subprocessio.LimitError(stdout, stderr); limit != nil {
		return nil, limit
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (n freeBSDSegmentNative) capture(ctx context.Context) (bsdSegmentSnapshot, error) {
	scope, err := n.scope(ctx)
	if err != nil {
		return bsdSegmentSnapshot{}, err
	}
	before, err := n.interfaces()
	if err != nil {
		return bsdSegmentSnapshot{}, err
	}
	output, err := n.command(ctx, "ifconfig", "-a")
	if err != nil {
		return bsdSegmentSnapshot{}, err
	}
	links, err := parseBSDSegmentInterfaces(string(output))
	if err != nil {
		return bsdSegmentSnapshot{}, err
	}
	after, err := n.interfaces()
	if err != nil {
		return bsdSegmentSnapshot{}, err
	}
	confirmed, err := n.scope(ctx)
	if err != nil || confirmed != scope {
		return bsdSegmentSnapshot{}, errors.Join(err, errors.New("VXLAN kernel scope changed during observation"))
	}
	snapshot := bsdSegmentSnapshot{scope: scope, links: links, before: map[string]net.Interface{}, after: map[string]net.Interface{}, readConfig: n.readConfig, ctx: ctx}
	for _, iface := range before {
		snapshot.before[iface.Name] = iface
	}
	for _, iface := range after {
		snapshot.after[iface.Name] = iface
	}
	return snapshot, ctx.Err()
}

func (s bsdSegmentSnapshot) index(name string) (int, error) {
	before, wasPresent := s.before[name]
	after, present := s.after[name]
	_, printed := s.links[name]
	if !wasPresent && !present && !printed {
		return 0, nil
	}
	if !wasPresent || !present || !printed || before.Index <= 0 || before.Index != after.Index {
		return 0, fmt.Errorf("interface %s changed during VXLAN observation", name)
	}
	return after.Index, nil
}

func (s bsdSegmentSnapshot) observe(name string) (SegmentLink, error) {
	index, err := s.index(name)
	if err != nil {
		return SegmentLink{}, err
	}
	if index == 0 {
		return SegmentLink{Scope: s.scope}, nil
	}
	value := s.links[name]
	link := value.link
	link.Scope, link.Index = s.scope, index
	link.Config.MTU = s.after[name].MTU
	link.Up = s.after[name].Flags&net.FlagUp != 0
	link.Running = s.after[name].Flags&net.FlagRunning != 0
	if link.Kind != "vxlan" {
		return link, nil
	}
	config, err := s.readConfig(s.ctx, name, index)
	if err != nil {
		return SegmentLink{}, err
	}
	link.Config.VNI = config.VNI
	if config.Local.IsValid() {
		link.Config.Local = config.Local.WithZone("").Unmap().String()
	}
	link.LocalPort, link.Config.Port = config.LocalPort, config.RemotePort
	if config.Remote.IsValid() && !config.Remote.IsUnspecified() {
		if config.Remote.IsMulticast() {
			link.Config.Group = config.Remote.Unmap().String()
		} else {
			link.Config.Remotes = []string{config.Remote.WithZone("").Unmap().String()}
			link.Flood = append([]string(nil), link.Config.Remotes...)
		}
	}
	if config.MCIndex > 0 {
		link.UnderlayIndex = config.MCIndex
	} else if link.Config.Local != "" {
		local, err := netip.ParseAddr(link.Config.Local)
		if err != nil {
			return SegmentLink{}, err
		}
		for source, candidate := range s.links {
			if !slices.Contains(candidate.addresses, local.WithZone("").Unmap()) {
				continue
			}
			sourceIndex, err := s.index(source)
			if err != nil {
				return SegmentLink{}, err
			}
			if link.UnderlayIndex != 0 && link.UnderlayIndex != sourceIndex {
				return SegmentLink{}, errors.New("VXLAN local address has ambiguous native interface ownership")
			}
			link.UnderlayIndex = sourceIndex
		}
	}
	for bridge, candidate := range s.links {
		if candidate.link.Kind != "bridge" || !slices.Contains(candidate.members, name) {
			continue
		}
		bridgeIndex, err := s.index(bridge)
		if err != nil {
			return SegmentLink{}, err
		}
		if link.BridgeIndex != 0 && link.BridgeIndex != bridgeIndex {
			return SegmentLink{}, errors.New("VXLAN has ambiguous bridge membership")
		}
		link.BridgeIndex = bridgeIndex
	}
	return link, nil
}

func (n freeBSDSegmentNative) Observe(ctx context.Context, name string) (SegmentLink, error) {
	snapshot, err := n.capture(ctx)
	if err != nil {
		return SegmentLink{}, err
	}
	return snapshot.observe(name)
}

func (n freeBSDSegmentNative) ObserveOwned(ctx context.Context, config SegmentConfig, owner SegmentIdentity) (SegmentLink, error) {
	snapshot, err := n.capture(ctx)
	if err != nil {
		return SegmentLink{}, err
	}
	if owner.Index > 0 {
		for name, iface := range snapshot.after {
			if iface.Index == owner.Index {
				return snapshot.observe(name)
			}
		}
	}
	for _, name := range []string{config.IfName, owner.CreationName} {
		link, err := snapshot.observe(name)
		if err != nil {
			return SegmentLink{}, err
		}
		if link.Exists && link.Token == owner.Token {
			return link, nil
		}
	}
	// A tokenless partial clone or a foreign replacement is an observation,
	// never an absent link that would authorize another creation.
	for _, name := range []string{config.IfName, owner.CreationName} {
		link, err := snapshot.observe(name)
		if err != nil || link.Exists {
			return link, err
		}
	}
	return SegmentLink{Scope: snapshot.scope}, nil
}

func (n freeBSDSegmentNative) Resolve(ctx context.Context, config SegmentConfig, token string) (SegmentIdentity, error) {
	if config.VNI < 1 || config.VNI >= 16777215 {
		return SegmentIdentity{}, errors.New("FreeBSD VXLAN VNI must be within 1-16777214")
	}
	if len(config.Remotes) > 1 {
		return SegmentIdentity{}, errors.New("FreeBSD VXLAN supports one unicast remote; use multicast for a multipoint segment")
	}
	snapshot, err := n.capture(ctx)
	if err != nil {
		return SegmentIdentity{}, err
	}
	underlay, err := snapshot.index(config.Underlay)
	if err != nil || underlay == 0 {
		return SegmentIdentity{}, errors.Join(err, fmt.Errorf("VXLAN underlay %s is unavailable", config.Underlay))
	}
	identity := SegmentIdentity{Scope: snapshot.scope, Token: token, UnderlayIndex: underlay}
	if config.Bridge != "" {
		bridge, err := snapshot.observe(config.Bridge)
		if err != nil {
			return identity, err
		}
		if !bridge.Exists || bridge.Kind != "bridge" {
			return identity, fmt.Errorf("VXLAN bridge %s is unavailable", config.Bridge)
		}
		identity.BridgeIndex = bridge.Index
	}
	if unit, err := strconv.ParseUint(strings.TrimPrefix(config.IfName, "vxlan"), 10, 31); err == nil && config.IfName == "vxlan"+strconv.FormatUint(unit, 10) {
		identity.CreationName = config.IfName
		return identity, nil
	}
	raw, err := hex.DecodeString(token)
	if err != nil || len(raw) != 16 {
		return identity, errors.New("VXLAN creation requires a native owner token")
	}
	start := int(binary.BigEndian.Uint16(raw[:2])) % 32768
	for offset := 0; offset < 32768; offset++ {
		name := "vxlan" + strconv.Itoa((start+offset)%32768)
		if _, exists := snapshot.after[name]; !exists {
			identity.CreationName = name
			return identity, nil
		}
	}
	return identity, errors.New("no native VXLAN clone unit is available")
}

func bsdSegmentArguments(config SegmentConfig) []string {
	args := []string{"vxlanid", strconv.Itoa(config.VNI), "vxlanlocal", config.Local, "vxlanlocalport", strconv.Itoa(config.Port), "vxlanremoteport", strconv.Itoa(config.Port), "vxlanlearn"}
	if config.Group != "" {
		args = append(args, "vxlangroup", config.Group, "vxlandev", config.Underlay)
	} else if len(config.Remotes) == 1 {
		args = append(args, "vxlanremote", config.Remotes[0])
	}
	if config.MTU > 0 {
		args = append(args, "mtu", strconv.Itoa(config.MTU))
	}
	return args
}

func (n freeBSDSegmentNative) Create(ctx context.Context, config SegmentConfig, owner SegmentIdentity) error {
	resolved, err := n.Resolve(ctx, config, owner.Token)
	if err != nil {
		return err
	}
	if resolved.Scope != owner.Scope || resolved.UnderlayIndex != owner.UnderlayIndex || resolved.BridgeIndex != owner.BridgeIndex {
		return errors.New("VXLAN topology changed before clone creation")
	}
	if !strings.HasPrefix(owner.CreationName, "vxlan") {
		return errors.New("VXLAN creation name is not an exact native clone")
	}
	existing, err := n.Observe(ctx, owner.CreationName)
	if err != nil {
		return err
	}
	if existing.Exists {
		return fmt.Errorf("VXLAN native clone %s is already occupied", owner.CreationName)
	}
	args := []string{owner.CreationName, "create", "description", SegmentProof(owner.Token)}
	args = append(args, bsdSegmentArguments(config)...)
	args = append(args, "down")
	_, err = n.command(ctx, "ifconfig", args...)
	return err
}

func (n freeBSDSegmentNative) owned(ctx context.Context, config SegmentConfig, owner SegmentIdentity) (SegmentLink, error) {
	link, err := n.ObserveOwned(ctx, config, owner)
	if err != nil {
		return link, err
	}
	if err := CheckSegmentIdentity(config, owner, link); err != nil {
		return link, err
	}
	if !owner.Pending && !slices.Equal(link.Config.Remotes, config.Remotes) {
		return link, errors.New("VXLAN native unicast endpoint changed")
	}
	return link, nil
}

func (n freeBSDSegmentNative) Configure(ctx context.Context, config SegmentConfig, owner SegmentIdentity, admit func(context.Context) error) error {
	if admit == nil {
		return errors.New("VXLAN current activation admission is required")
	}
	resolved, err := n.Resolve(ctx, config, owner.Token)
	if err != nil {
		return err
	}
	if resolved.Scope != owner.Scope || resolved.UnderlayIndex != owner.UnderlayIndex || resolved.BridgeIndex != owner.BridgeIndex {
		return errors.New("VXLAN topology changed before activation")
	}
	link, err := n.owned(ctx, config, owner)
	if err != nil {
		return err
	}
	if link.Config.IfName != config.IfName {
		desired, err := n.Observe(ctx, config.IfName)
		if err != nil {
			return err
		}
		if desired.Exists {
			return errors.New("VXLAN desired interface name is occupied")
		}
		link, err = n.owned(ctx, config, owner)
		if err != nil {
			return err
		}
		if _, err := n.command(ctx, "ifconfig", link.Config.IfName, "name", config.IfName); err != nil {
			return err
		}
	}
	if _, err := n.owned(ctx, config, owner); err != nil {
		return err
	}
	args := append([]string{config.IfName, "down"}, bsdSegmentArguments(config)...)
	if _, err := n.command(ctx, "ifconfig", args...); err != nil {
		return err
	}
	if owner.BridgeIndex > 0 {
		link, err := n.owned(ctx, config, owner)
		if err != nil {
			return err
		}
		if link.BridgeIndex == 0 {
			if _, err := n.command(ctx, "ifconfig", config.Bridge, "addm", config.IfName); err != nil {
				return err
			}
		}
	}
	if err := admit(ctx); err != nil {
		return err
	}
	if _, err := n.owned(ctx, config, owner); err != nil {
		return err
	}
	_, err = n.command(ctx, "ifconfig", config.IfName, "up")
	return err
}

func (n freeBSDSegmentNative) Remove(ctx context.Context, config SegmentConfig, owner SegmentIdentity) error {
	link, err := n.owned(ctx, config, owner)
	if err != nil {
		return err
	}
	if _, err := n.command(ctx, "ifconfig", link.Config.IfName, "down"); err != nil {
		return err
	}
	link, err = n.owned(ctx, config, owner)
	if err != nil {
		return err
	}
	_, err = n.command(ctx, "ifconfig", link.Config.IfName, "destroy")
	return err
}

func parseBSDSegmentInterfaces(output string) (map[string]*bsdSegmentInterface, error) {
	links := map[string]*bsdSegmentInterface{}
	var current *bsdSegmentInterface
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			name, ok := strings.CutSuffix(fields[0], ":")
			if !ok || name == "" || len(fields) < 2 || !strings.HasPrefix(fields[1], "flags=") || links[name] != nil {
				return nil, errors.New("malformed FreeBSD native interface header")
			}
			current = &bsdSegmentInterface{link: SegmentLink{Exists: true, Config: SegmentConfig{IfName: name}}}
			links[name] = current
			continue
		}
		if current == nil {
			return nil, errors.New("native interface details precede an interface header")
		}
		switch fields[0] {
		case "description:":
			if token, owned := strings.CutPrefix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "description:")), "routerd.vxlan:"); owned {
				current.link.Token = token
			}
		case "groups:":
			if slices.Contains(fields[1:], "bridge") {
				current.link.Kind = "bridge"
			} else if slices.Contains(fields[1:], "vxlan") {
				current.link.Kind = "vxlan"
			}
		case "id":
			if len(fields) > 2 && fields[2] == "priority" {
				current.link.Kind = "bridge"
			}
		case "member:":
			if len(fields) < 2 {
				return nil, errors.New("malformed bridge member observation")
			}
			current.members = append(current.members, fields[1])
		case "inet", "inet6":
			if len(fields) < 2 {
				return nil, errors.New("native interface address is missing")
			}
			address, err := netip.ParseAddr(fields[1])
			if err != nil {
				return nil, fmt.Errorf("native interface address: %w", err)
			}
			current.addresses = append(current.addresses, address.WithZone("").Unmap())
		}
	}
	return links, nil
}
