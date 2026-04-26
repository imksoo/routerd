package render

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
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
	Profile      string
	PrefixLength int
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
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func writeDHCPv6PD(buf *bytes.Buffer, source pdSource) {
	buf.WriteString("[Network]\nDHCP=yes\nIPv6AcceptRA=yes\n\n[DHCPv6]\n")
	switch source.Profile {
	case "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd":
		buf.WriteString("DUIDType=link-layer\n")
		buf.WriteString("UseAddress=no\n")
		buf.WriteString("UseDelegatedPrefix=yes\n")
		buf.WriteString("WithoutRA=solicit\n")
		buf.WriteString("RapidCommit=no\n")
	default:
		buf.WriteString("UseDelegatedPrefix=yes\n")
		buf.WriteString("WithoutRA=solicit\n")
	}
	if source.PrefixLength != 0 {
		buf.WriteString(fmt.Sprintf("PrefixDelegationHint=::/%d\n", source.PrefixLength))
	}
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
