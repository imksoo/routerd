// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/platform"
)

func validateInterfaceResource(res api.Resource, targetOS platform.OS) (bool, error) {
	switch res.Kind {
	case "Interface":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.InterfaceSpec()
		if err != nil {
			return true, err
		}
		if spec.IfName == "" {
			return true, fmt.Errorf("%s spec.ifname is required", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 9216) {
			return true, fmt.Errorf("%s spec.mtu must be within 576-9216", res.ID())
		}
	case "Link":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.LinkSpec()
		if err != nil {
			return true, err
		}
		if spec.IfName != "" && strings.ContainsAny(spec.IfName, " \t\n/") {
			return true, fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
	case "Bridge":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.BridgeSpec()
		if err != nil {
			return true, err
		}
		ifname := defaultString(spec.IfName, res.Metadata.Name)
		if strings.ContainsAny(ifname, " \t\n/") {
			return true, fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
		if len(ifname) > 15 {
			return true, fmt.Errorf("%s spec.ifname must be 15 characters or fewer", res.ID())
		}
		if len(spec.Members) == 0 {
			return true, fmt.Errorf("%s spec.members must not be empty", res.ID())
		}
		seenMembers := map[string]bool{}
		for i, member := range spec.Members {
			if strings.TrimSpace(member) == "" {
				return true, fmt.Errorf("%s spec.members[%d] must not be empty", res.ID(), i)
			}
			if seenMembers[member] {
				return true, fmt.Errorf("%s spec.members[%d] duplicates %q", res.ID(), i, member)
			}
			seenMembers[member] = true
		}
		if spec.MACAddress != "" {
			if _, err := net.ParseMAC(spec.MACAddress); err != nil {
				return true, fmt.Errorf("%s spec.macAddress is invalid: %w", res.ID(), err)
			}
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 9216) {
			return true, fmt.Errorf("%s spec.mtu must be within 576-9216", res.ID())
		}
		if spec.ForwardDelay != 0 && (spec.ForwardDelay < 2 || spec.ForwardDelay > 30) {
			return true, fmt.Errorf("%s spec.forwardDelay must be within 2-30", res.ID())
		}
		if spec.HelloTime != 0 && (spec.HelloTime < 1 || spec.HelloTime > 10) {
			return true, fmt.Errorf("%s spec.helloTime must be within 1-10", res.ID())
		}
	case "VXLANSegment":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.VXLANSegmentSpec()
		if err != nil {
			return true, err
		}
		ifname := defaultString(spec.IfName, res.Metadata.Name)
		if strings.ContainsAny(ifname, " \t\n/") {
			return true, fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
		if len(ifname) > 15 {
			return true, fmt.Errorf("%s spec.ifname must be 15 characters or fewer", res.ID())
		}
		if spec.VNI < 1 || spec.VNI > 16777215 {
			return true, fmt.Errorf("%s spec.vni must be within 1-16777215", res.ID())
		}
		if _, err := netip.ParseAddr(spec.LocalAddress); err != nil {
			return true, fmt.Errorf("%s spec.localAddress must be an IP address", res.ID())
		}
		if spec.UnderlayInterface == "" {
			return true, fmt.Errorf("%s spec.underlayInterface is required", res.ID())
		}
		if len(spec.Remotes) == 0 && spec.MulticastGroup == "" {
			return true, fmt.Errorf("%s spec.remotes or spec.multicastGroup is required", res.ID())
		}
		if len(spec.Remotes) > 0 && spec.MulticastGroup != "" {
			return true, fmt.Errorf("%s spec.remotes and spec.multicastGroup are mutually exclusive", res.ID())
		}
		for i, remote := range spec.Remotes {
			if _, err := netip.ParseAddr(remote); err != nil {
				return true, fmt.Errorf("%s spec.remotes[%d] must be an IP address", res.ID(), i)
			}
		}
		if spec.MulticastGroup != "" {
			addr, err := netip.ParseAddr(spec.MulticastGroup)
			if err != nil || !addr.Is4() || !addr.IsMulticast() {
				return true, fmt.Errorf("%s spec.multicastGroup must be an IPv4 multicast address", res.ID())
			}
		}
		if spec.UDPPort != 0 && (spec.UDPPort < 1 || spec.UDPPort > 65535) {
			return true, fmt.Errorf("%s spec.udpPort must be within 1-65535", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 9216) {
			return true, fmt.Errorf("%s spec.mtu must be within 576-9216", res.ID())
		}
		switch spec.L2Filter {
		case "", "default", "none":
		default:
			return true, fmt.Errorf("%s spec.l2Filter must be default or none", res.ID())
		}
	case "WireGuardInterface":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.WireGuardInterfaceSpec()
		if err != nil {
			return true, err
		}
		ifname := res.Metadata.Name
		if strings.ContainsAny(ifname, " \t\n/") {
			return true, fmt.Errorf("%s metadata.name must be usable as a WireGuard interface name", res.ID())
		}
		if len(ifname) > 15 {
			return true, fmt.Errorf("%s metadata.name must be 15 characters or fewer", res.ID())
		}
		if spec.ListenPort != 0 && (spec.ListenPort < 1 || spec.ListenPort > 65535) {
			return true, fmt.Errorf("%s spec.listenPort must be within 1-65535", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 9216) {
			return true, fmt.Errorf("%s spec.mtu must be within 576-9216", res.ID())
		}
		if spec.FwMark != 0 || spec.Table != 0 {
			return true, fmt.Errorf("%s spec.fwmark and spec.table are not supported; routerd derives WireGuard fwmark and routing table ownership automatically", res.ID())
		}
		if strings.ContainsAny(spec.PrivateKeyFile, "\n\r") {
			return true, fmt.Errorf("%s spec.privateKeyFile is invalid", res.ID())
		}
	case "WireGuardPeer":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.WireGuardPeerSpec()
		if err != nil {
			return true, err
		}
		if spec.Interface == "" {
			return true, fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.PublicKey == "" {
			return true, fmt.Errorf("%s spec.publicKey is required", res.ID())
		}
		if len(spec.AllowedIPs) == 0 {
			return true, fmt.Errorf("%s spec.allowedIPs is required", res.ID())
		}
		for i, allowed := range spec.AllowedIPs {
			if _, err := netip.ParsePrefix(allowed); err != nil {
				return true, fmt.Errorf("%s spec.allowedIPs[%d] must be an IP prefix", res.ID(), i)
			}
		}
		if spec.PersistentKeepalive < 0 || spec.PersistentKeepalive > 65535 {
			return true, fmt.Errorf("%s spec.persistentKeepalive must be within 0-65535", res.ID())
		}
		if strings.ContainsAny(spec.PresharedKeyFile, "\n\r") {
			return true, fmt.Errorf("%s spec.presharedKeyFile is invalid", res.ID())
		}
	case "TailscaleNode":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.TailscaleNodeSpec()
		if err != nil {
			return true, err
		}
		switch defaultString(spec.State, "present") {
		case "present", "absent":
		default:
			return true, fmt.Errorf("%s spec.state must be present or absent", res.ID())
		}
		if spec.AuthKey != "" && (spec.AuthKeyEnv != "" || spec.AuthKeyFile != "") {
			return true, fmt.Errorf("%s spec.authKey is mutually exclusive with spec.authKeyEnv and spec.authKeyFile", res.ID())
		}
		if spec.Operator != "" || spec.BinaryPath != "" {
			return true, fmt.Errorf("%s spec.operator and spec.binaryPath are not supported; routerd derives tailscale runtime mechanics from the platform", res.ID())
		}
		for field, value := range map[string]string{
			"hostname":    spec.Hostname,
			"loginServer": spec.LoginServer,
			"authKeyEnv":  spec.AuthKeyEnv,
			"authKeyFile": spec.AuthKeyFile,
		} {
			if strings.ContainsAny(value, "\x00\n\r") {
				return true, fmt.Errorf("%s spec.%s contains invalid characters", res.ID(), field)
			}
		}
		if spec.AuthKeyEnv != "" && !validEnvironmentName(spec.AuthKeyEnv) {
			return true, fmt.Errorf("%s spec.authKeyEnv must be an environment variable name", res.ID())
		}
		if spec.AuthKeyFile != "" {
			if err := validateSystemdEnvironmentFilePath(spec.AuthKeyFile); err != nil {
				return true, fmt.Errorf("%s spec.authKeyFile %w", res.ID(), err)
			}
		}
		for i, route := range spec.AdvertiseRoutes {
			if _, err := netip.ParsePrefix(route); err != nil {
				return true, fmt.Errorf("%s spec.advertiseRoutes[%d] must be an IP prefix", res.ID(), i)
			}
		}
		for i, tag := range spec.AdvertiseTags {
			if strings.TrimSpace(tag) == "" || strings.ContainsAny(tag, " \t\n\r\x00") {
				return true, fmt.Errorf("%s spec.advertiseTags[%d] must be a Tailscale tag", res.ID(), i)
			}
		}
	case "IPsecConnection":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPsecConnectionSpec()
		if err != nil {
			return true, err
		}
		if _, err := netip.ParseAddr(spec.LocalAddress); err != nil {
			return true, fmt.Errorf("%s spec.localAddress must be an IP address", res.ID())
		}
		if _, err := netip.ParseAddr(spec.RemoteAddress); err != nil {
			return true, fmt.Errorf("%s spec.remoteAddress must be an IP address", res.ID())
		}
		if spec.PreSharedKey == "" && spec.CertificateRef == "" {
			return true, fmt.Errorf("%s spec.preSharedKey or spec.certificateRef is required", res.ID())
		}
		if spec.PreSharedKey != "" && spec.CertificateRef != "" {
			return true, fmt.Errorf("%s spec.preSharedKey and spec.certificateRef are mutually exclusive", res.ID())
		}
		if _, err := netip.ParsePrefix(spec.LeftSubnet); err != nil {
			return true, fmt.Errorf("%s spec.leftSubnet must be an IP prefix", res.ID())
		}
		if _, err := netip.ParsePrefix(spec.RightSubnet); err != nil {
			return true, fmt.Errorf("%s spec.rightSubnet must be an IP prefix", res.ID())
		}
		switch spec.CloudProviderHint {
		case "", "aws", "azure", "gcp":
		default:
			return true, fmt.Errorf("%s spec.cloudProviderHint must be aws, azure, or gcp", res.ID())
		}
	case "VRF":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.VRFSpec()
		if err != nil {
			return true, err
		}
		ifname := defaultString(spec.IfName, res.Metadata.Name)
		if strings.ContainsAny(ifname, " \t\n/") {
			return true, fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
		if len(ifname) > 15 {
			return true, fmt.Errorf("%s spec.ifname must be 15 characters or fewer", res.ID())
		}
		if spec.RouteTable < 1 {
			return true, fmt.Errorf("%s spec.routeTable is required", res.ID())
		}
	case "VXLANTunnel":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.VXLANTunnelSpec()
		if err != nil {
			return true, err
		}
		ifname := defaultString(spec.IfName, res.Metadata.Name)
		if strings.ContainsAny(ifname, " \t\n/") {
			return true, fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
		if len(ifname) > 15 {
			return true, fmt.Errorf("%s spec.ifname must be 15 characters or fewer", res.ID())
		}
		if spec.VNI < 1 || spec.VNI > 16777215 {
			return true, fmt.Errorf("%s spec.vni must be within 1-16777215", res.ID())
		}
		if _, err := netip.ParseAddr(spec.LocalAddress); err != nil {
			return true, fmt.Errorf("%s spec.localAddress must be an IP address", res.ID())
		}
		if spec.UnderlayInterface == "" {
			return true, fmt.Errorf("%s spec.underlayInterface is required", res.ID())
		}
		for i, peer := range spec.Peers {
			if _, err := netip.ParseAddr(peer); err != nil {
				return true, fmt.Errorf("%s spec.peers[%d] must be an IP address", res.ID(), i)
			}
		}
		if spec.UDPPort != 0 && (spec.UDPPort < 1 || spec.UDPPort > 65535) {
			return true, fmt.Errorf("%s spec.udpPort must be within 1-65535", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 9216) {
			return true, fmt.Errorf("%s spec.mtu must be within 576-9216", res.ID())
		}
	case "IPv4StaticAddress":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4StaticAddressSpec()
		if err != nil {
			return true, err
		}
		addr := spec.Address
		if addr == "" {
			return true, fmt.Errorf("%s spec.address is required", res.ID())
		}
		if _, err := netip.ParsePrefix(addr); err != nil {
			return true, fmt.Errorf("%s spec.address is invalid: %w", res.ID(), err)
		}
		if spec.AllowOverlap && spec.AllowOverlapReason == "" {
			return true, fmt.Errorf("%s spec.allowOverlapReason is required when allowOverlap is true", res.ID())
		}
	case "VirtualAddress":
		if err := validateVirtualAddressResource(res, targetOS); err != nil {
			return true, err
		}
	default:
		return false, nil
	}
	return true, nil
}
