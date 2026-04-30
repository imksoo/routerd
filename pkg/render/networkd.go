package render

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"routerd/pkg/api"
)

type File struct {
	Path string
	Data []byte
}

type pdSource struct {
	Name         string
	IfName       string
	Client       string
	Profile      string
	PrefixLength int
	IAID         string
	DUIDType     string
	DUIDRawData  string
}

func NetworkdDropins(router *api.Router) ([]File, error) {
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
	raIfaces := map[string]bool{}
	dhcp6cPDIfaces := map[string]bool{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "IPv6RAAddress":
			spec, err := res.IPv6RAAddressSpec()
			if err != nil {
				return nil, err
			}
			if ifname := aliases[spec.Interface]; ifname != "" {
				raIfaces[ifname] = api.BoolDefault(spec.Managed, true)
			}
			continue
		case "IPv6PrefixDelegation":
		default:
			continue
		}
		spec, err := res.IPv6PrefixDelegationSpec()
		if err != nil {
			return nil, err
		}
		profile := defaultString(spec.Profile, api.IPv6PDProfileDefault)
		pds[res.Metadata.Name] = pdSource{
			Name:         res.Metadata.Name,
			IfName:       aliases[spec.Interface],
			Client:       defaultString(spec.Client, "networkd"),
			Profile:      profile,
			PrefixLength: api.EffectiveIPv6PDPrefixLength(profile, spec.PrefixLength),
			IAID:         strings.TrimSpace(spec.IAID),
			DUIDType:     api.EffectiveIPv6PDDUIDType(profile, spec.DUIDType),
			DUIDRawData:  spec.DUIDRawData,
		}
		if defaultString(spec.Client, "networkd") == "dhcp6c" {
			if ifname := aliases[spec.Interface]; ifname != "" {
				dhcp6cPDIfaces[ifname] = true
			}
		}
	}

	var files []File
	var raNames []string
	for ifname, managed := range raIfaces {
		if managed {
			raNames = append(raNames, ifname)
		}
	}
	sort.Strings(raNames)
	for _, ifname := range raNames {
		var buf bytes.Buffer
		buf.WriteString("[Network]\nIPv6AcceptRA=yes\n")
		if dhcp6cPDIfaces[ifname] {
			buf.WriteString("\n[IPv6AcceptRA]\nDHCPv6Client=no\nUseDNS=no\nUseDomains=no\n")
		}
		files = append(files, File{
			Path: filepath.Join(networkdDropinDir(ifname), "89-routerd-ipv6-ra.conf"),
			Data: buf.Bytes(),
		})
	}
	var names []string
	for name := range pds {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		source := pds[name]
		if source.Client != "networkd" {
			continue
		}
		ifname := source.IfName
		if ifname == "" {
			return nil, fmt.Errorf("IPv6PrefixDelegation %q has empty ifname", name)
		}
		var buf bytes.Buffer
		writeDHCPv6PD(&buf, source)
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
		if source.Client != "networkd" {
			continue
		}
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
	}
	if source.DUIDRawData != "" {
		buf.WriteString("DUIDRawData=" + formatColonHex(source.DUIDRawData) + "\n")
	}
	switch source.Profile {
	case api.IPv6PDProfileNTTNGNDirectHikariDenwa, api.IPv6PDProfileNTTHGWLANPD:
		buf.WriteString("UseAddress=no\n")
		buf.WriteString("UseDelegatedPrefix=yes\n")
		buf.WriteString("WithoutRA=solicit\n")
		buf.WriteString("RapidCommit=no\n")
	default:
		buf.WriteString("UseDelegatedPrefix=yes\n")
		buf.WriteString("WithoutRA=solicit\n")
	}
	if source.PrefixLength != 0 && !api.IsNTTIPv6PDProfile(source.Profile) {
		buf.WriteString(fmt.Sprintf("PrefixDelegationHint=::/%d\n", source.PrefixLength))
	}
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
