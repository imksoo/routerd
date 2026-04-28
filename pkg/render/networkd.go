package render

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	routerstate "routerd/pkg/state"
)

type File struct {
	Path string
	Data []byte
}

type pdSource struct {
	Name         string
	IfName       string
	Profile      string
	PrefixLength int
	IAID         string
	DUIDType     string
	DUIDRawData  string
	PrefixHint   string
}

func NetworkdDropins(router *api.Router) ([]File, error) {
	return NetworkdDropinsWithState(router, nil)
}

func NetworkdDropinsWithState(router *api.Router, store *routerstate.Store) ([]File, error) {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err != nil {
			return nil, err
		}
		aliases[res.Metadata.Name] = spec.IfName
	}

	pds := map[string]pdSource{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6PrefixDelegation" {
			continue
		}
		spec, err := res.IPv6PrefixDelegationSpec()
		if err != nil {
			return nil, err
		}
		pds[res.Metadata.Name] = pdSource{
			Name:         res.Metadata.Name,
			IfName:       aliases[spec.Interface],
			Profile:      defaultString(spec.Profile, "default"),
			PrefixLength: effectiveIPv6PDPrefixLength(defaultString(spec.Profile, "default"), spec.PrefixLength),
			IAID:         spec.IAID,
			DUIDType:     spec.DUIDType,
			DUIDRawData:  spec.DUIDRawData,
			PrefixHint:   prefixHintFromState(res.Metadata.Name, spec, store),
		}
	}

	var files []File
	var names []string
	for name := range pds {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ifname := pds[name].IfName
		if ifname == "" {
			return nil, fmt.Errorf("IPv6PrefixDelegation %q has empty ifname", name)
		}
		var buf bytes.Buffer
		writeDHCPv6PD(&buf, pds[name])
		files = append(files, File{
			Path: filepath.Join(networkdDropinDir(ifname), "90-routerd-dhcp6-pd.conf"),
			Data: buf.Bytes(),
		})
	}

	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return nil, err
		}
		ifname := aliases[spec.Interface]
		source := pds[spec.PrefixDelegation]
		if ifname == "" || source.IfName == "" {
			return nil, fmt.Errorf("%s references interface with empty ifname", res.ID())
		}
		var buf bytes.Buffer
		buf.WriteString("[Network]\n")
		if spec.SendRA {
			buf.WriteString("IPv6SendRA=yes\n")
		}
		buf.WriteString("DHCPPrefixDelegation=yes\n\n[DHCPPrefixDelegation]\n")
		buf.WriteString("UplinkInterface=" + source.IfName + "\n")
		buf.WriteString("SubnetId=" + defaultString(spec.SubnetID, "0") + "\n")
		buf.WriteString("Assign=yes\n")
		buf.WriteString("Token=" + spec.AddressSuffix + "\n")
		buf.WriteString("Announce=" + boolString(spec.Announce || spec.SendRA) + "\n")
		files = append(files, File{
			Path: filepath.Join(networkdDropinDir(ifname), "90-routerd-dhcp6-pd.conf"),
			Data: buf.Bytes(),
		})
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "NTPClient" {
			continue
		}
		spec, err := res.NTPClientSpec()
		if err != nil {
			return nil, err
		}
		if !spec.Managed || spec.Interface == "" {
			continue
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			return nil, fmt.Errorf("%s references interface with empty ifname", res.ID())
		}
		var servers []string
		for _, server := range spec.Servers {
			server = strings.TrimSpace(server)
			if server != "" {
				servers = append(servers, server)
			}
		}
		if len(servers) == 0 {
			return nil, fmt.Errorf("%s spec.servers is required", res.ID())
		}
		var buf bytes.Buffer
		buf.WriteString("[Network]\n")
		buf.WriteString("NTP=" + strings.Join(servers, " ") + "\n")
		files = append(files, File{
			Path: filepath.Join(networkdDropinDir(ifname), "91-routerd-ntp.conf"),
			Data: buf.Bytes(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func writeDHCPv6PD(buf *bytes.Buffer, source pdSource) {
	buf.WriteString("[Network]\nDHCP=yes\nIPv6AcceptRA=yes\n\n[DHCPv6]\n")
	if source.IAID != "" {
		buf.WriteString("IAID=" + normalizeIAIDForRender(source.IAID) + "\n")
	}
	if source.DUIDType != "" {
		buf.WriteString("DUIDType=" + source.DUIDType + "\n")
	} else if source.Profile == "ntt-ngn-direct-hikari-denwa" || source.Profile == "ntt-hgw-lan-pd" {
		buf.WriteString("DUIDType=link-layer\n")
	}
	if source.DUIDRawData != "" {
		buf.WriteString("DUIDRawData=" + formatColonHex(source.DUIDRawData) + "\n")
	}
	switch source.Profile {
	case "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd":
		buf.WriteString("UseAddress=no\n")
		buf.WriteString("UseDelegatedPrefix=yes\n")
		buf.WriteString("WithoutRA=solicit\n")
		buf.WriteString("RapidCommit=no\n")
	default:
		buf.WriteString("UseDelegatedPrefix=yes\n")
		buf.WriteString("WithoutRA=solicit\n")
	}
	if source.PrefixLength != 0 {
		hint := source.PrefixHint
		if hint == "" {
			hint = fmt.Sprintf("::/%d", source.PrefixLength)
		}
		buf.WriteString("PrefixDelegationHint=" + hint + "\n")
	}
}

func prefixHintFromState(name string, spec api.IPv6PrefixDelegationSpec, store *routerstate.Store) string {
	if store == nil || (spec.HintFromState != nil && !*spec.HintFromState) {
		return ""
	}
	base := "ipv6PrefixDelegation." + name
	lease, _ := routerstate.PDLeaseFromStore(store, base)
	prefix, ok := routerstate.PDLeaseHintPrefix(lease, time.Now().UTC())
	if !ok {
		return ""
	}
	return prefix
}

func normalizeIAIDForRender(value string) string {
	parsed, ok := parseIAID(value)
	if !ok {
		return value
	}
	return fmt.Sprintf("%d", parsed)
}

func parseIAID(value string) (uint32, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		parsed, err := strconv.ParseUint(value[2:], 16, 32)
		return uint32(parsed), err == nil
	}
	if len(value) == 8 && isHexString(value) {
		parsed, err := strconv.ParseUint(value, 16, 32)
		return uint32(parsed), err == nil
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	return uint32(parsed), err == nil
}

func formatColonHex(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, ":", "")
	if len(value)%2 != 0 || !isHexString(value) {
		return strings.TrimSpace(value)
	}
	var parts []string
	for i := 0; i < len(value); i += 2 {
		parts = append(parts, value[i:i+2])
	}
	return strings.Join(parts, ":")
}

func isHexString(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func effectiveIPv6PDPrefixLength(profile string, configured int) int {
	if configured != 0 {
		return configured
	}
	if profile == "ntt-ngn-direct-hikari-denwa" || profile == "ntt-hgw-lan-pd" {
		return 60
	}
	return 0
}

func networkdDropinDir(ifname string) string {
	return "/etc/systemd/network/10-netplan-" + sanitizeNetworkdName(ifname) + ".network.d"
}

func sanitizeNetworkdName(name string) string {
	return strings.NewReplacer("/", "-", "\x00", "").Replace(name)
}

func boolString(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
