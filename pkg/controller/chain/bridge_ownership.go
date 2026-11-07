// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

// One receipt remains authoritative through install, activation and withdrawal.
// A new topology is never installed until the previous exact generation is gone.
type bridgeOwner struct {
	Version     int          `json:"version"`
	Resource    string       `json:"resource"`
	Scope       string       `json:"scope"`
	Token       string       `json:"token"`
	IfIndex     int          `json:"ifIndex"`
	Activated   bool         `json:"activated"`
	Withdrawing bool         `json:"withdrawing"`
	Ports       []bridgePort `json:"ports"`
	Plan        bridgePlan   `json:"plan"`
}

type bridgePort struct {
	IfName  string `json:"ifname"`
	IfIndex int    `json:"ifIndex"`
}

type bridgePlan struct {
	IfName            string            `json:"ifname"`
	Members           []string          `json:"members"`
	MTU               int               `json:"mtu"`
	MACAddress        string            `json:"macAddress"`
	STP               bool              `json:"stp"`
	MulticastSnooping bool              `json:"multicastSnooping"`
	ForwardDelay      int               `json:"forwardDelay"`
	HelloTime         int               `json:"helloTime"`
	Files             []bridgeOwnedFile `json:"files"`
}

type bridgeOwnedFile struct {
	Path string `json:"path"`
	Data []byte `json:"data"`
}

func (p bridgePlan) digest() string {
	data, _ := json.Marshal(p)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func (o bridgeOwner) proof() string { return "routerd.bridge=" + o.Token }

func (c BridgeController) bridgePlan(resource api.Resource) (bridgePlan, error) {
	spec, err := resource.BridgeSpec()
	if err != nil {
		return bridgePlan{}, err
	}
	plan := bridgePlan{IfName: firstNonEmpty(spec.IfName, resource.Metadata.Name), MTU: spec.MTU, MACAddress: spec.MACAddress, STP: api.BoolDefault(spec.STP, true), MulticastSnooping: api.BoolDefault(spec.MulticastSnooping, false), ForwardDelay: chainFirstNonZero(spec.ForwardDelay, 4), HelloTime: chainFirstNonZero(spec.HelloTime, 2)}
	aliases := bridgeInterfaceAliases(c.Router)
	for _, member := range spec.Members {
		// Segment owns its port together with its link/FDB acquisition.
		if vxlanSegmentMember(c.Router, member) {
			continue
		}
		ifname := aliases[member]
		if ifname == "" {
			return plan, fmt.Errorf("Bridge/%s member %s is unavailable", resource.Metadata.Name, member)
		}
		plan.Members = append(plan.Members, ifname)
	}
	sort.Strings(plan.Members)
	files, err := render.NetworkdBridgeResourceFiles(c.Router, resource.Metadata.Name)
	if err != nil {
		return plan, err
	}
	for _, file := range files {
		relative, err := filepath.Rel("/etc/systemd/network", file.Path)
		if err != nil {
			return plan, err
		}
		plan.Files = append(plan.Files, bridgeOwnedFile{Path: relative, Data: file.Data})
	}
	sort.Slice(plan.Files, func(i, j int) bool { return plan.Files[i].Path < plan.Files[j].Path })
	return plan, nil
}

func (c BridgeController) bridgeOwnerPath(resource string) string {
	return filepath.Join(c.networkdPath("/etc/systemd/network"), fmt.Sprintf(".routerd-bridge-%x.json", sha256.Sum256([]byte(resource))))
}

func (c BridgeController) readBridgeOwners() ([]bridgeOwner, error) {
	base := c.networkdPath("/etc/systemd/network")
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var owners []bridgeOwner
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".routerd-bridge-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.IsDir() {
			return nil, fmt.Errorf("invalid Bridge receipt directory %s", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(base, entry.Name()))
		if err != nil {
			return nil, err
		}
		var owner bridgeOwner
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&owner); err != nil {
			return nil, fmt.Errorf("decode Bridge receipt: %w", err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return nil, fmt.Errorf("trailing data in Bridge receipt %s", entry.Name())
		}
		token, err := hex.DecodeString(owner.Token)
		if err != nil || len(token) != 24 || owner.Version != 2 || owner.Resource == "" || owner.Scope == "" || owner.Plan.IfName == "" || owner.IfIndex < 0 || filepath.Base(c.bridgeOwnerPath(owner.Resource)) != entry.Name() {
			return nil, fmt.Errorf("invalid Bridge ownership receipt %s", entry.Name())
		}
		for _, file := range owner.Plan.Files {
			if !validBridgeArtifactPath(file.Path) || !bytes.HasPrefix(file.Data, []byte(routerdGeneratedNetworkdHeader+"\n")) {
				return nil, fmt.Errorf("invalid owned Bridge artifact %q", file.Path)
			}
		}
		owners = append(owners, owner)
	}
	return owners, nil
}

func validBridgeArtifactPath(path string) bool {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || path == ".." || strings.HasPrefix(path, "../") {
		return false
	}
	if filepath.Dir(path) == "." {
		return strings.HasPrefix(path, "30-routerd-") && strings.HasSuffix(path, ".network")
	}
	return strings.HasSuffix(filepath.Dir(path), ".network.d") && filepath.Base(path) == "88-routerd-bridge.conf" && strings.Count(path, "/") == 1
}

func (c BridgeController) writeBridgeOwner(owner bridgeOwner) error {
	path := c.bridgeOwnerPath(owner.Resource)
	data, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".routerd-bridge-pending-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (c BridgeController) reconcileOwnedBridges(ctx context.Context) error {
	scope, err := c.bridgeScope(ctx)
	if err != nil {
		return err
	}
	desired := make(map[string]api.Resource)
	plans := make(map[string]bridgePlan)
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "Bridge" {
			continue
		}
		desired[resource.Metadata.Name] = resource
		plan, err := c.bridgePlan(resource)
		if err != nil {
			return err
		}
		plans[resource.Metadata.Name] = plan
		if _, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "Bridge", resource.Metadata.Name); err != nil {
			return err
		}
	}
	owners, err := c.readBridgeOwners()
	if err != nil {
		return err
	}
	current := make(map[string]bridgeOwner)
	for _, owner := range owners {
		if _, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "Bridge", owner.Resource); err != nil {
			return err
		}
		if err := c.checkBridgeScope(ctx, owner); err != nil {
			return err
		}
		plan, retained := plans[owner.Resource]
		native, err := c.bridgeInterface(owner.Plan.IfName)
		if err != nil {
			return err
		}
		nativeGone := owner.IfIndex > 0 && (native == nil || native.Index != owner.IfIndex)
		drifted := false
		if native != nil && !nativeGone {
			alias, err := c.readFile(filepath.Join("/sys/class/net", native.Name, "ifalias"))
			if err != nil {
				return err
			}
			if owner.IfIndex > 0 && strings.TrimSpace(string(alias)) != owner.proof() {
				nativeGone = true
			} else if owner.Activated {
				healthy, err := c.bridgeHealthy(owner, *native)
				if err != nil {
					return err
				}
				drifted = !healthy
			}
		}
		if err := c.checkBridgeScope(ctx, owner); err != nil {
			return err
		}
		if !retained || owner.Withdrawing || nativeGone || drifted || plan.digest() != owner.Plan.digest() {
			if c.DryRun {
				continue
			}
			if err := c.withdrawBridge(ctx, owner); err != nil {
				return err
			}
			continue
		}
		current[owner.Resource] = owner
	}
	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		resource, plan := desired[name], plans[name]
		if c.DryRun {
			if err := c.savePlannedBridgeStatus(resource); err != nil {
				return err
			}
			continue
		}
		owner, owned := current[name]
		if !owned {
			existing, err := c.bridgeInterface(plan.IfName)
			if err != nil {
				return err
			}
			if existing != nil {
				return fmt.Errorf("Bridge/%s refuses unowned native interface %s", name, plan.IfName)
			}
			if err := c.checkBridgeMemberOwners(plan); err != nil {
				return err
			}
			for _, file := range plan.Files {
				_, err := os.ReadFile(filepath.Join(c.networkdPath("/etc/systemd/network"), file.Path))
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				if err == nil {
					return fmt.Errorf("refusing unowned Bridge artifact %s", file.Path)
				}
			}
			raw := make([]byte, 24)
			if _, err := rand.Read(raw); err != nil {
				return err
			}
			owner = bridgeOwner{Version: 2, Resource: name, Scope: scope, Token: hex.EncodeToString(raw), Plan: plan}
			if err := c.checkBridgeScope(ctx, owner); err != nil {
				return err
			}
			if err := c.writeBridgeOwner(owner); err != nil {
				return err
			}
		}
		if !owner.Activated {
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", name, map[string]any{"phase": "Pending", "reason": "NativeActivationPending", "ifname": plan.IfName}); err != nil {
				return err
			}
		}
		if err := c.applyOwnedBridge(ctx, &owner); err != nil {
			saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", name, map[string]any{"phase": "Error", "reason": "NativeActivationFailed", "ifname": plan.IfName, "error": err.Error()})
			return errors.Join(err, saveErr)
		}
		if err := c.saveAppliedBridgeStatus(ctx, owner); err != nil {
			return err
		}
	}
	return nil
}

// A successful complete interface-list snapshot establishes absence. Lookup
// errors alone cannot distinguish a missing link from a failed observation.
func (c BridgeController) bridgeInterface(name string) (*net.Interface, error) {
	list := c.Interfaces
	if list == nil {
		list = net.Interfaces
	}
	interfaces, err := list()
	if err != nil {
		return nil, err
	}
	for _, iface := range interfaces {
		if iface.Name == name {
			return &iface, nil
		}
	}
	return nil, nil
}

func (c BridgeController) observedOwnedBridge(owner bridgeOwner) (*net.Interface, error) {
	iface, err := c.bridgeInterface(owner.Plan.IfName)
	if err != nil || iface == nil {
		return iface, err
	}
	alias, err := c.readFile(filepath.Join("/sys/class/net", iface.Name, "ifalias"))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(alias)) != owner.proof() || owner.IfIndex > 0 && owner.IfIndex != iface.Index {
		return nil, fmt.Errorf("Bridge/%s native ownership changed for %s", owner.Resource, iface.Name)
	}
	if !c.isKernelBridge(iface.Name) {
		return nil, fmt.Errorf("owned interface %s is not a bridge", iface.Name)
	}
	return iface, nil
}

func (c BridgeController) applyOwnedBridge(ctx context.Context, owner *bridgeOwner) (result error) {
	defer func() {
		if result == nil {
			return
		}
		cleanupCtx := context.WithoutCancel(ctx)
		if err := c.checkBridgeScope(cleanupCtx, *owner); err != nil {
			result = errors.Join(result, err)
			return
		}
		iface, err := c.observedOwnedBridge(*owner)
		if err == nil && iface != nil {
			if err := c.checkBridgeScope(cleanupCtx, *owner); err != nil {
				result = errors.Join(result, err)
				return
			}
			if downErr := c.setBridgeAdmin(iface.Index, owner.proof(), false); downErr != nil {
				result = errors.Join(result, downErr)
			}
		}
	}()
	if err := c.checkBridgeScope(ctx, *owner); err != nil {
		return err
	}
	iface, err := c.observedOwnedBridge(*owner)
	if err != nil {
		return err
	}
	if iface == nil {
		if owner.IfIndex > 0 {
			return fmt.Errorf("owned Bridge %s disappeared; withdrawal is required before recreation", owner.Plan.IfName)
		}
		// Linux 6.8 does not apply IFLA_IFALIAS during RTM_NEWLINK. Bind
		// the successful exclusive create ACK to a requested native index;
		// only this invocation may then install the cookie on that identity.
		digest := sha256.Sum256([]byte(owner.Token))
		createdIndex := int(binary.BigEndian.Uint32(digest[:4])&0x3fffffff) + 1024
		args := []string{"link", "add", "name", owner.Plan.IfName, "index", strconv.Itoa(createdIndex)}
		if owner.Plan.MTU > 0 {
			args = append(args, "mtu", strconv.Itoa(owner.Plan.MTU))
		}
		if owner.Plan.MACAddress != "" {
			args = append(args, "address", owner.Plan.MACAddress)
		}
		args = append(args, "type", "bridge", "stp_state", strconv.Itoa(boolInt(owner.Plan.STP)), "mcast_snooping", strconv.Itoa(boolInt(owner.Plan.MulticastSnooping)), "forward_delay", strconv.Itoa(owner.Plan.ForwardDelay*100), "hello_time", strconv.Itoa(owner.Plan.HelloTime*100))
		if err := c.checkBridgeScope(ctx, *owner); err != nil {
			return err
		}
		if err := c.command(ctx, "ip", args...); err != nil {
			return err
		}
		// Keep the requested index in memory for failure cleanup, but do
		// not acknowledge it on disk before the actual cookie readback.
		owner.IfIndex = createdIndex
		pending, err := c.readBridgeOwners()
		if err != nil {
			return err
		}
		current := false
		for _, receipt := range pending {
			if receipt.Resource == owner.Resource && receipt.Token == owner.Token && receipt.Scope == owner.Scope && receipt.IfIndex == 0 && !receipt.Activated && !receipt.Withdrawing && receipt.Plan.digest() == owner.Plan.digest() {
				current = true
				break
			}
		}
		if !current {
			return errors.New("Bridge creation receipt changed before cookie installation")
		}
		if err := c.checkBridgeScope(ctx, *owner); err != nil {
			return err
		}
		setAlias := c.SetAliasByIndex
		if setAlias == nil {
			setAlias = setBridgeAliasByIndex
		}
		if err := setAlias(createdIndex, owner.Plan.IfName, owner.proof()); err != nil {
			return err
		}
		iface, err = c.observedOwnedBridge(*owner)
		if err != nil {
			return err
		}
		if iface == nil {
			return fmt.Errorf("Bridge %s create has no native readback", owner.Plan.IfName)
		}
		if err := c.checkBridgeScope(ctx, *owner); err != nil {
			return err
		}
		owner.IfIndex = iface.Index
		if err := c.writeBridgeOwner(*owner); err != nil {
			return err
		}
	}
	if owner.IfIndex == 0 {
		if err := c.checkBridgeScope(ctx, *owner); err != nil {
			return err
		}
		owner.IfIndex = iface.Index
		if err := c.writeBridgeOwner(*owner); err != nil {
			return err
		}
	}
	filesPresent := true
	for _, file := range owner.Plan.Files {
		current, err := os.ReadFile(filepath.Join(c.networkdPath("/etc/systemd/network"), file.Path))
		if errors.Is(err, os.ErrNotExist) {
			filesPresent = false
			continue
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(current, file.Data) {
			return fmt.Errorf("Bridge artifact changed by another owner: %s", file.Path)
		}
	}
	if owner.Activated {
		healthy, err := c.bridgeHealthy(*owner, *iface)
		if err != nil {
			return err
		}
		if err := c.checkBridgeScope(ctx, *owner); err != nil {
			return err
		}
		if healthy && filesPresent {
			return nil
		}
		owner.Activated = false
		if err := c.writeBridgeOwner(*owner); err != nil {
			return err
		}
	}
	if err := c.checkBridgeMemberOwners(owner.Plan); err != nil {
		return err
	}
	var ports []bridgePort
	for _, member := range owner.Plan.Members {
		port, err := c.bridgeInterface(member)
		if err != nil {
			return err
		}
		if port == nil {
			return fmt.Errorf("Bridge member %s is unavailable", member)
		}
		ports = append(ports, bridgePort{IfName: member, IfIndex: port.Index})
	}
	if err := c.checkBridgeScope(ctx, *owner); err != nil {
		return err
	}
	owner.Ports = ports
	if err := c.writeBridgeOwner(*owner); err != nil {
		return err
	}
	for _, file := range owner.Plan.Files {
		if err := c.checkBridgeScope(ctx, *owner); err != nil {
			return err
		}
		if err := c.installBridgeFile(file); err != nil {
			return err
		}
	}
	if err := c.checkBridgeScope(ctx, *owner); err != nil {
		return err
	}
	if err := c.command(ctx, "networkctl", "reload"); err != nil {
		return err
	}
	for _, member := range append(append([]string(nil), owner.Plan.Members...), owner.Plan.IfName) {
		if err := c.checkBridgeScope(ctx, *owner); err != nil {
			return err
		}
		if err := c.command(ctx, "networkctl", "reconfigure", member); err != nil {
			return err
		}
	}
	for _, port := range owner.Ports {
		if err := c.checkBridgeScope(ctx, *owner); err != nil {
			return err
		}
		attach := c.SetMasterByIndex
		if attach == nil {
			attach = setBridgeMasterByIndex
		}
		if err := attach(port, iface.Index, owner.proof()); err != nil {
			return err
		}
	}
	if err := c.checkBridgeScope(ctx, *owner); err != nil {
		return err
	}
	if err := c.setBridgeAdmin(iface.Index, owner.proof(), true); err != nil {
		return err
	}
	iface, err = c.observedOwnedBridge(*owner)
	if err != nil {
		return err
	}
	if iface == nil {
		return fmt.Errorf("Bridge disappeared during activation")
	}
	healthy, err := c.bridgeHealthy(*owner, *iface)
	if err != nil {
		return err
	}
	if !healthy {
		return fmt.Errorf("Bridge %s native activation readback mismatched", iface.Name)
	}
	if err := c.checkBridgeScope(ctx, *owner); err != nil {
		return err
	}
	owner.Activated = true
	return c.writeBridgeOwner(*owner)
}

func (c BridgeController) checkBridgeMemberOwners(plan bridgePlan) error {
	for _, member := range plan.Members {
		target, err := c.readlink(filepath.Join("/sys/class/net", member, "master"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if filepath.Base(target) != plan.IfName {
			return fmt.Errorf("Bridge %s refuses foreign master %s on member %s", plan.IfName, filepath.Base(target), member)
		}
	}
	return nil
}

func (c BridgeController) bridgeHealthy(owner bridgeOwner, iface net.Interface) (bool, error) {
	p := owner.Plan
	if iface.Flags&net.FlagUp == 0 || p.MTU > 0 && iface.MTU != p.MTU || p.MACAddress != "" && !strings.EqualFold(iface.HardwareAddr.String(), p.MACAddress) {
		return false, nil
	}
	for field, want := range map[string]int{"stp_state": boolInt(p.STP), "multicast_snooping": boolInt(p.MulticastSnooping), "forward_delay": p.ForwardDelay * 100, "hello_time": p.HelloTime * 100} {
		data, err := c.readFile(filepath.Join("/sys/class/net", iface.Name, "bridge", field))
		if err != nil {
			return false, err
		}
		got, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return false, err
		}
		if got != want {
			return false, nil
		}
	}
	for _, member := range p.Members {
		port, err := c.bridgeInterface(member)
		if err != nil {
			return false, err
		}
		captured := false
		for _, receipt := range owner.Ports {
			if port != nil && receipt.IfName == member && receipt.IfIndex == port.Index {
				captured = true
				break
			}
		}
		if !captured {
			return false, nil
		}
		target, err := c.readlink(filepath.Join("/sys/class/net", member, "master"))
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if filepath.Base(target) != p.IfName {
			return false, nil
		}
	}
	return true, nil
}

func (c BridgeController) setBridgeAdmin(index int, proof string, up bool) error {
	if c.SetAdminByIndex != nil {
		return c.SetAdminByIndex(index, proof, up)
	}
	return setBridgeAdminByIndex(index, proof, up)
}

func (c BridgeController) withdrawBridge(ctx context.Context, owner bridgeOwner) error {
	if err := c.checkBridgeScope(ctx, owner); err != nil {
		return err
	}
	if !owner.Withdrawing {
		owner.Withdrawing = true
		if err := c.writeBridgeOwner(owner); err != nil {
			return err
		}
	}
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", owner.Resource, map[string]any{"phase": "Pending", "reason": "NativeWithdrawalPending", "ifname": owner.Plan.IfName}); err != nil {
		return err
	}
	if err := c.checkBridgeScope(ctx, owner); err != nil {
		return err
	}
	iface, err := c.bridgeInterface(owner.Plan.IfName)
	if err != nil {
		return err
	}
	owned := false
	if iface != nil {
		alias, err := c.readFile(filepath.Join("/sys/class/net", iface.Name, "ifalias"))
		if err != nil {
			return err
		}
		owned = strings.TrimSpace(string(alias)) == owner.proof() && (owner.IfIndex == 0 || owner.IfIndex == iface.Index)
		if owner.IfIndex == 0 && !owned {
			return errors.New("Bridge creation outcome is unconfirmed; retain its receipt until native absence is observed")
		}
		if owned {
			if !c.isKernelBridge(iface.Name) {
				return fmt.Errorf("owned native link is not a Bridge")
			}
			if err := c.checkBridgeScope(ctx, owner); err != nil {
				return err
			}
			if err := c.setBridgeAdmin(iface.Index, owner.proof(), false); err != nil {
				return err
			}
		}
	}
	for _, file := range owner.Plan.Files {
		path := filepath.Join(c.networkdPath("/etc/systemd/network"), file.Path)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if bytes.Equal(data, file.Data) {
			if err := c.checkBridgeScope(ctx, owner); err != nil {
				return err
			}
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	if err := c.checkBridgeScope(ctx, owner); err != nil {
		return err
	}
	if err := c.command(ctx, "networkctl", "reload"); err != nil {
		return err
	}
	if owned {
		remove := c.DeleteByIndex
		if remove == nil {
			remove = deleteBridgeByIndex
		}
		if err := c.checkBridgeScope(ctx, owner); err != nil {
			return err
		}
		if err := remove(iface.Index, owner.proof()); err != nil {
			return err
		}
		after, err := c.bridgeInterface(owner.Plan.IfName)
		if err != nil {
			return err
		}
		if after != nil && after.Index == iface.Index {
			return fmt.Errorf("Bridge %s still exists after native delete", iface.Name)
		}
	}
	if err := c.checkBridgeScope(ctx, owner); err != nil {
		return err
	}
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", owner.Resource, map[string]any{"phase": "Absent", "reason": "OwnedGenerationWithdrawn"}); err != nil {
		return err
	}
	if err := c.checkBridgeScope(ctx, owner); err != nil {
		return err
	}
	return os.Remove(c.bridgeOwnerPath(owner.Resource))
}

// Publish a complete artifact only if its path is still absent. A pending
// generation may reuse its own exact bytes, but never overwrite another file.
func (c BridgeController) installBridgeFile(artifact bridgeOwnedFile) error {
	path := filepath.Join(c.networkdPath("/etc/systemd/network"), artifact.Path)
	if current, err := os.ReadFile(path); err == nil {
		if bytes.Equal(current, artifact.Data) {
			return nil
		}
		return fmt.Errorf("Bridge artifact changed by another owner: %s", artifact.Path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".routerd-bridge-file-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0644); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(artifact.Data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Link(temporary, path)
}

func vxlanSegmentMember(router *api.Router, member string) bool {
	for _, resource := range router.Spec.Resources {
		if (resource.Kind == "VXLANSegment" || resource.Kind == "VXLANTunnel") && resource.Metadata.Name == member {
			return true
		}
	}
	return false
}
