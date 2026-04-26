package reconcile

import (
	"bytes"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/config"
)

type Engine struct {
	Command      func(name string, args ...string) ([]byte, error)
	OSNetworking *osNetworking
}

func New() *Engine {
	return &Engine{Command: runCommand}
}

func (e *Engine) Validate(router *api.Router) error {
	return config.Validate(router)
}

func (e *Engine) Observe(router *api.Router) (*Result, error) {
	if err := e.Validate(router); err != nil {
		return nil, err
	}
	return e.evaluate(router, false)
}

func (e *Engine) Plan(router *api.Router) (*Result, error) {
	if err := e.Validate(router); err != nil {
		return nil, err
	}
	return e.evaluate(router, true)
}

func (e *Engine) evaluate(router *api.Router, includePlan bool) (*Result, error) {
	aliases := interfaceAliases(router)
	osNet := e.detectOSNetworking()
	policies := interfacePolicies(router, osNet)
	observedV4 := e.observedIPv4Prefixes(policies)
	observedV4ByInterface := ipv4AssignmentsByInterface(observedV4)
	desiredV4 := desiredIPv4Prefixes(router, aliases)
	overlaps := ipv4Overlaps(desiredV4, observedV4)
	result := &Result{
		Generation: time.Now().Unix(),
		Timestamp:  time.Now().UTC(),
		Phase:      "Healthy",
	}

	for _, res := range router.Spec.Resources {
		rr := ResourceResult{
			ID:       res.ID(),
			Phase:    "Healthy",
			Observed: map[string]string{},
		}

		switch res.Kind {
		case "Sysctl":
			e.observeSysctl(res, includePlan, &rr)
		case "Interface":
			e.observeInterface(res, policies[res.Metadata.Name], observedV4ByInterface[res.Metadata.Name], includePlan, &rr)
		case "IPv4StaticAddress":
			e.observeIPv4Static(res, aliases, policies, overlaps[res.ID()], includePlan, &rr)
		case "IPv4DHCPAddress":
			e.observeDHCP(res, aliases, policies, "ipv4", includePlan, &rr)
		case "IPv6DHCPAddress":
			e.observeDHCP(res, aliases, policies, "ipv6", includePlan, &rr)
		case "Hostname":
			e.observeHostname(res, osNet, includePlan, &rr)
		}

		if rr.Phase == "RequiresAdoption" || rr.Phase == "Blocked" {
			result.Phase = "Blocked"
		}
		result.Resources = append(result.Resources, rr)
	}

	return result, nil
}

func (e *Engine) observeSysctl(res api.Resource, includePlan bool, rr *ResourceResult) {
	key := stringSpec(res, "key")
	desired := stringSpec(res, "value")
	runtime := boolSpecDefault(res, "runtime", true)
	persistent := boolSpec(res, "persistent")

	rr.Observed["key"] = key
	rr.Observed["desired"] = desired
	rr.Observed["runtime"] = fmt.Sprintf("%t", runtime)
	rr.Observed["persistent"] = fmt.Sprintf("%t", persistent)

	if out, err := e.Command("sysctl", "-n", key); err == nil {
		current := strings.TrimSpace(string(out))
		rr.Observed["current"] = current
		if current != desired {
			rr.Phase = "Drifted"
		}
	} else {
		rr.Phase = "Drifted"
		rr.Warnings = append(rr.Warnings, fmt.Sprintf("could not observe sysctl %s: %v", key, err))
	}

	if !includePlan {
		return
	}
	if runtime {
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure runtime sysctl %s=%s", key, desired))
	}
	if persistent {
		rr.Plan = append(rr.Plan, fmt.Sprintf("persistent sysctl %s=%s is not implemented yet", key, desired))
		rr.Warnings = append(rr.Warnings, "persistent sysctl rendering is pending OS-specific implementation")
	}
}

func (e *Engine) observeInterface(res api.Resource, policy interfacePolicy, observedV4 []ipv4Assignment, includePlan bool, rr *ResourceResult) {
	rr.Observed["ifname"] = policy.IfName
	rr.Observed["managed"] = fmt.Sprintf("%t", policy.Managed)
	rr.Observed["owner"] = policy.Owner

	if exists, up := e.interfaceState(policy.IfName); exists {
		rr.Observed["exists"] = "true"
		rr.Observed["up"] = fmt.Sprintf("%t", up)
	} else {
		rr.Observed["exists"] = "false"
		rr.Phase = "Drifted"
	}

	if policy.OS.CloudInit {
		rr.Observed["cloudInit"] = "present"
	}
	if policy.OS.Netplan {
		rr.Observed["netplan"] = "present"
	}
	if policy.OS.Networkd {
		rr.Observed["networkd"] = "present"
	}
	if len(observedV4) > 0 {
		var prefixes []string
		for _, assignment := range observedV4 {
			prefixes = append(prefixes, assignment.Prefix.String())
		}
		rr.Observed["ipv4Prefixes"] = strings.Join(prefixes, ",")
	}

	if !includePlan {
		return
	}
	if !policy.Managed || policy.Owner == "external" {
		rr.Plan = append(rr.Plan, "observe only; interface is externally managed")
		return
	}
	if policy.RequiresAdoption {
		rr.Phase = "RequiresAdoption"
		rr.Plan = append(rr.Plan, "blocked: existing cloud-init/netplan networking detected; run an explicit adoption workflow before routerd manages this interface")
		rr.Conditions = append(rr.Conditions, Condition{
			Type:    "AdoptionRequired",
			Status:  "True",
			Reason:  "ExistingOSNetworking",
			Message: "routerd will not override cloud-init/netplan-managed networking automatically",
		})
		return
	}
	if boolSpec(res, "adminUp") {
		rr.Plan = append(rr.Plan, "ensure link is administratively up")
	}
}

func (e *Engine) observeIPv4Static(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, overlaps []addressOverlap, includePlan bool, rr *ResourceResult) {
	iface := stringSpec(res, "interface")
	ifname := aliases[iface]
	policy := policies[iface]
	addr := stringSpec(res, "address")

	rr.Observed["interface"] = iface
	rr.Observed["ifname"] = ifname
	rr.Observed["address"] = addr

	if has := e.hasAddress(ifname, addr, "-4"); has {
		rr.Observed["present"] = "true"
	} else {
		rr.Observed["present"] = "false"
		rr.Phase = "Drifted"
	}

	if includePlan {
		if len(overlaps) > 0 {
			if boolSpec(res, "allowOverlap") {
				for _, overlap := range overlaps {
					rr.Warnings = append(rr.Warnings, overlap.Message)
				}
			} else {
				rr.Phase = "Blocked"
				for _, overlap := range overlaps {
					rr.Plan = append(rr.Plan, "blocked: "+overlap.Message)
				}
				rr.Conditions = append(rr.Conditions, Condition{
					Type:    "AddressOverlap",
					Status:  "True",
					Reason:  "OverlappingIPv4Prefix",
					Message: "IPv4 overlap is blocked by default; set allowOverlap with a reason only for intentional NAT/HA cases",
				})
				return
			}
		}
		if !policy.Managed || policy.Owner == "external" {
			rr.Plan = append(rr.Plan, "observe only; referenced interface is externally managed")
			return
		}
		if policy.RequiresAdoption {
			rr.Phase = "RequiresAdoption"
			rr.Plan = append(rr.Plan, "blocked: referenced interface requires adoption before routerd manages addresses")
			return
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure IPv4 address %s on %s", addr, ifname))
		if boolSpec(res, "exclusive") {
			rr.Plan = append(rr.Plan, fmt.Sprintf("remove other IPv4 addresses on %s", ifname))
		}
	}
}

func (e *Engine) observeDHCP(res api.Resource, aliases map[string]string, policies map[string]interfacePolicy, family string, includePlan bool, rr *ResourceResult) {
	iface := stringSpec(res, "interface")
	ifname := aliases[iface]
	policy := policies[iface]
	client := stringSpecDefault(res, "client", "dhcpcd")

	rr.Observed["interface"] = iface
	rr.Observed["ifname"] = ifname
	rr.Observed["family"] = family
	rr.Observed["client"] = client

	if _, err := exec.LookPath(client); err == nil {
		rr.Observed["clientAvailable"] = "true"
	} else {
		rr.Observed["clientAvailable"] = "false"
		if includePlan {
			rr.Warnings = append(rr.Warnings, fmt.Sprintf("%s is required to ensure DHCP on this host", client))
		}
	}
	if includePlan {
		if !policy.Managed || policy.Owner == "external" {
			rr.Plan = append(rr.Plan, "observe only; referenced interface is externally managed")
			return
		}
		if policy.RequiresAdoption {
			rr.Phase = "RequiresAdoption"
			rr.Plan = append(rr.Plan, "blocked: referenced interface requires adoption before routerd manages DHCP")
			return
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure %s DHCP client %s is running for %s", family, client, ifname))
	}
}

func (e *Engine) observeHostname(res api.Resource, osNet osNetworking, includePlan bool, rr *ResourceResult) {
	desired := stringSpec(res, "hostname")
	rr.Observed["desired"] = desired
	if out, err := e.Command("hostname"); err == nil {
		current := strings.TrimSpace(string(out))
		rr.Observed["current"] = current
		if current != desired {
			rr.Phase = "Drifted"
		}
	}
	if includePlan {
		if osNet.CloudInit {
			rr.Warnings = append(rr.Warnings, "cloud-init is present and may reset hostname unless configured not to manage it")
		}
		rr.Plan = append(rr.Plan, fmt.Sprintf("ensure hostname is %s", desired))
	}
}

type osNetworking struct {
	CloudInit bool
	Netplan   bool
	Networkd  bool
}

func (e *Engine) detectOSNetworking() osNetworking {
	if e.OSNetworking != nil {
		return *e.OSNetworking
	}
	var osNet osNetworking
	if _, err := os.Stat("/etc/cloud/cloud.cfg"); err == nil {
		osNet.CloudInit = true
	}
	if entries, err := os.ReadDir("/etc/netplan"); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
				osNet.Netplan = true
				break
			}
		}
	}
	if _, err := e.Command("networkctl", "list", "--no-pager"); err == nil {
		osNet.Networkd = true
	}
	return osNet
}

func (e *Engine) interfaceState(ifname string) (bool, bool) {
	out, err := e.Command("ip", "-brief", "link", "show", "dev", ifname)
	if err != nil {
		return false, false
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return true, false
	}
	return true, fields[1] == "UP" || strings.Contains(fields[1], "UP")
}

func (e *Engine) hasAddress(ifname, address, family string) bool {
	out, err := e.Command("ip", "-brief", family, "addr", "show", "dev", ifname)
	if err != nil {
		return false
	}
	return strings.Contains(string(out), address)
}

func (e *Engine) observedIPv4Prefixes(policies map[string]interfacePolicy) []ipv4Assignment {
	var assignments []ipv4Assignment
	for name, policy := range policies {
		out, err := e.Command("ip", "-brief", "-4", "addr", "show", "dev", policy.IfName)
		if err != nil {
			continue
		}
		for _, prefix := range parseIPv4Prefixes(string(out)) {
			assignments = append(assignments, ipv4Assignment{
				ResourceID: "observed/" + name + "/" + prefix.String(),
				Interface:  name,
				IfName:     policy.IfName,
				Prefix:     prefix,
				Source:     "observed",
			})
		}
	}
	return assignments
}

func interfaceAliases(router *api.Router) map[string]string {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind == "Interface" {
			aliases[res.Metadata.Name] = stringSpec(res, "ifname")
		}
	}
	return aliases
}

type ipv4Assignment struct {
	ResourceID         string
	Interface          string
	IfName             string
	Prefix             netip.Prefix
	Source             string
	AllowOverlap       bool
	AllowOverlapReason string
}

type addressOverlap struct {
	Other   ipv4Assignment
	Message string
}

func desiredIPv4Prefixes(router *api.Router, aliases map[string]string) []ipv4Assignment {
	var assignments []ipv4Assignment
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv4StaticAddress" {
			continue
		}
		prefix, err := netip.ParsePrefix(stringSpec(res, "address"))
		if err != nil {
			continue
		}
		iface := stringSpec(res, "interface")
		assignments = append(assignments, ipv4Assignment{
			ResourceID:         res.ID(),
			Interface:          iface,
			IfName:             aliases[iface],
			Prefix:             prefix.Masked(),
			Source:             "desired",
			AllowOverlap:       boolSpec(res, "allowOverlap"),
			AllowOverlapReason: stringSpec(res, "allowOverlapReason"),
		})
	}
	return assignments
}

func ipv4Overlaps(desired, observed []ipv4Assignment) map[string][]addressOverlap {
	result := map[string][]addressOverlap{}
	all := append([]ipv4Assignment{}, observed...)
	all = append(all, desired...)

	for _, current := range desired {
		for _, other := range all {
			if current.ResourceID == other.ResourceID {
				continue
			}
			if current.Interface == other.Interface {
				continue
			}
			if !prefixesOverlap(current.Prefix, other.Prefix) {
				continue
			}
			result[current.ResourceID] = append(result[current.ResourceID], addressOverlap{
				Other: other,
				Message: fmt.Sprintf(
					"IPv4 prefix %s on %s overlaps with %s prefix %s on %s",
					current.Prefix,
					current.IfName,
					other.Source,
					other.Prefix,
					other.IfName,
				),
			})
		}
	}
	return result
}

func ipv4AssignmentsByInterface(assignments []ipv4Assignment) map[string][]ipv4Assignment {
	result := map[string][]ipv4Assignment{}
	for _, assignment := range assignments {
		result[assignment.Interface] = append(result[assignment.Interface], assignment)
	}
	return result
}

func prefixesOverlap(a, b netip.Prefix) bool {
	a = a.Masked()
	b = b.Masked()
	return a.Contains(b.Addr()) || b.Contains(a.Addr())
}

func parseIPv4Prefixes(output string) []netip.Prefix {
	var prefixes []netip.Prefix
	for _, field := range strings.Fields(output) {
		if !strings.Contains(field, "/") {
			continue
		}
		prefix, err := netip.ParsePrefix(field)
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes
}

type interfacePolicy struct {
	Name             string
	IfName           string
	Managed          bool
	Owner            string
	RequiresAdoption bool
	OS               osNetworking
}

func interfacePolicies(router *api.Router, osNet osNetworking) map[string]interfacePolicy {
	policies := map[string]interfacePolicy{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		managed := boolSpec(res, "managed")
		owner := stringSpecDefault(res, "owner", ownerFromManaged(managed))
		policies[res.Metadata.Name] = interfacePolicy{
			Name:             res.Metadata.Name,
			IfName:           stringSpec(res, "ifname"),
			Managed:          managed,
			Owner:            owner,
			RequiresAdoption: managed && owner != "external" && (osNet.CloudInit || osNet.Netplan),
			OS:               osNet,
		}
	}
	return policies
}

func runCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return out, err
	}
	return out, nil
}

func ownerFromManaged(managed bool) string {
	if managed {
		return "routerd"
	}
	return "external"
}

func stringSpec(res api.Resource, key string) string {
	value, ok := res.Spec[key]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return s
}

func stringSpecDefault(res api.Resource, key, fallback string) string {
	if value := stringSpec(res, key); value != "" {
		return value
	}
	return fallback
}

func boolSpec(res api.Resource, key string) bool {
	value, ok := res.Spec[key]
	if !ok {
		return false
	}
	b, ok := value.(bool)
	return ok && b
}

func boolSpecDefault(res api.Resource, key string, fallback bool) bool {
	value, ok := res.Spec[key]
	if !ok {
		return fallback
	}
	b, ok := value.(bool)
	if !ok {
		return fallback
	}
	return b
}
