// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"net/netip"
	"strings"

	"github.com/imksoo/routerd/pkg/dhcpfingerprint"
	"github.com/imksoo/routerd/pkg/logstore"
)

type clientFingerprint struct {
	OSFamily    string
	DeviceClass string
	Confidence  int
	Signals     []string
	Hostname    string
	Vendor      string
}

type fingerprintAccumulator struct {
	osScores      map[string]int
	classScores   map[string]int
	osClassScores map[string]int
	signals       map[string]bool
	signalScores  map[string]fingerprintSignalScore
	hostname      string
	vendor        string
	hasMulticast  bool
}

type fingerprintSignalScore struct {
	OSFamily    string
	DeviceClass string
	Score       int
}

func buildPassiveFingerprints(_ []DHCPLease, flows []logstore.TrafficFlow, queries []logstore.DNSQuery, firewallLogs []logstore.FirewallLogEntry) map[string]*fingerprintAccumulator {
	out := map[string]*fingerprintAccumulator{}
	acc := func(ip string) *fingerprintAccumulator {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			return nil
		}
		item := out[ip]
		if item == nil {
			item = &fingerprintAccumulator{osScores: map[string]int{}, classScores: map[string]int{}, osClassScores: map[string]int{}, signals: map[string]bool{}, signalScores: map[string]fingerprintSignalScore{}}
			out[ip] = item
		}
		return item
	}
	for _, query := range queries {
		item := acc(query.ClientAddress)
		if item == nil {
			continue
		}
		applyDomainFingerprint(item, query.QuestionName)
	}
	for _, flow := range flows {
		item := acc(flow.ClientAddress)
		if item == nil {
			continue
		}
		applyDomainFingerprint(item, flow.ResolvedHostname)
		applyDomainFingerprint(item, flow.TLSSNI)
		applyDomainFingerprint(item, flow.HTTPHost)
		applyDomainFingerprint(item, flow.DNSQuery)
		applyTransportFingerprint(item, flow.Protocol, flow.PeerAddress, flow.PeerPort)
		applyAppFingerprint(item, flow.AppName, flow.AppCategory, flow.AppConfidence)
	}
	for _, entry := range firewallLogs {
		ip := firewallClientAddress(entry)
		item := acc(ip)
		if item == nil {
			continue
		}
		applyDomainFingerprint(item, entry.DPITLSSNI)
		applyDomainFingerprint(item, entry.DPIHTTPHost)
		applyDomainFingerprint(item, entry.DPIDNSQuery)
		applyTransportFingerprint(item, entry.Protocol, entry.DstAddress, entry.DstPort)
		applyAppFingerprint(item, entry.DPIApp, entry.DPICategory, entry.DPIConfidence)
	}
	return out
}

func applyHostVendorFingerprint(item *fingerprintAccumulator, hostname, vendor, clientID string) {
	hostText := strings.ToLower(strings.Join([]string{hostname, clientID}, " "))
	switch {
	case containsAny(hostText, "doorbell", "nest-cam", "nest cam", "nest-doorbell"):
		addFingerprintSignal(item, "iot", "camera", 150, "hostname/camera")
	case containsAny(hostText, "bravia", "android-tv", "androidtv", "google-tv"):
		addFingerprintSignal(item, "iot", "smart-tv", 150, "hostname/smart-tv")
	case containsAny(hostText, "echo", "alexa"):
		addFingerprintSignal(item, "iot", "smart-speaker", 130, "hostname/amazon-echo")
	case containsAny(hostText, "google-nest", "google home", "google-home", "nest-mini", "nest hub"):
		addFingerprintSignal(item, "iot", "smart-speaker", 130, "hostname/google-nest")
	case strings.Contains(hostText, "chromecast"):
		addFingerprintSignal(item, "iot", "smart-tv", 130, "hostname/chromecast")
	case strings.Contains(hostText, "roku"):
		addFingerprintSignal(item, "iot", "smart-tv", 130, "hostname/roku")
	case strings.Contains(hostText, "firetv") || strings.Contains(hostText, "fire-tv"):
		addFingerprintSignal(item, "iot", "smart-tv", 130, "hostname/fire-tv")
	case strings.Contains(hostText, "switchbot"):
		addFingerprintSignal(item, "iot", "iot", 130, "hostname/switchbot")
	case containsAny(hostText, "hue", "philips-hue"):
		addFingerprintSignal(item, "iot", "lighting", 130, "hostname/hue")
	case strings.Contains(hostText, "ring"):
		addFingerprintSignal(item, "iot", "camera", 130, "hostname/ring")
	case containsAny(hostText, "eufy", "wyze"):
		addFingerprintSignal(item, "iot", "camera", 130, "hostname/camera")
	case containsAny(hostText, "roomba", "irobot", "roborock"):
		addFingerprintSignal(item, "iot", "vacuum", 130, "hostname/vacuum")
	case strings.Contains(hostText, "sonos"):
		addFingerprintSignal(item, "iot", "smart-speaker", 130, "hostname/sonos")
	case containsAny(hostText, "kasa", "tapo", "tp-link", "tplink", "aqara", "tuya", "smartlife", "shelly", "nature-remo", "broadlink", "aiseg", "ecoflow", "atom", "espressif"):
		addFingerprintSignal(item, "iot", "iot", 125, "hostname/iot")
	case strings.Contains(hostText, "synology"):
		addFingerprintSignal(item, "nas", "nas", 140, "hostname/synology")
	case strings.Contains(hostText, "qnap"):
		addFingerprintSignal(item, "nas", "nas", 140, "hostname/qnap")
	case containsAny(hostText, "hp-printer", "officejet", "laserjet", "deskjet"):
		addFingerprintSignal(item, "printer", "printer", 140, "hostname/hp-printer")
	case strings.Contains(hostText, "canon"):
		addFingerprintSignal(item, "printer", "printer", 130, "hostname/canon")
	case strings.Contains(hostText, "epson"):
		addFingerprintSignal(item, "printer", "printer", 130, "hostname/epson")
	case strings.Contains(hostText, "brother"):
		addFingerprintSignal(item, "printer", "printer", 130, "hostname/brother")
	case containsAny(hostText, "yealink", "polycom"):
		addFingerprintSignal(item, "voip", "voip", 130, "hostname/voip")
	case strings.Contains(hostText, "tesla"):
		addFingerprintSignal(item, "iot", "ev", 140, "hostname/tesla")
	case strings.Contains(hostText, "nintendo"):
		addFingerprintSignal(item, "nintendo", "gaming-console", 140, "hostname/nintendo")
	case strings.Contains(hostText, "playstation") || strings.Contains(hostText, "ps5") || strings.Contains(hostText, "ps4"):
		addFingerprintSignal(item, "playstation", "gaming-console", 140, "hostname/playstation")
	case strings.Contains(hostText, "xbox"):
		addFingerprintSignal(item, "xbox", "gaming-console", 140, "hostname/xbox")
	case strings.Contains(hostText, "steamdeck") || strings.Contains(hostText, "steam deck"):
		addFingerprintSignal(item, "steam-os", "gaming-console", 140, "hostname/steamdeck")
	case strings.Contains(hostText, "iphone"):
		addFingerprintSignal(item, "Apple", "phone", 120, "hostname=iphone")
	case strings.Contains(hostText, "ipad"):
		addFingerprintSignal(item, "Apple", "tablet", 120, "hostname=ipad")
	case strings.Contains(hostText, "macbook") || strings.Contains(hostText, "imac") || strings.Contains(hostText, "mac mini"):
		addFingerprintSignal(item, "Apple", "computer", 100, "hostname/mac")
	case strings.Contains(hostText, "windows") || strings.HasPrefix(strings.TrimSpace(hostText), "win-") || strings.Contains(hostText, "microsoft"):
		addFingerprintSignal(item, "Windows", "computer", 90, "hostname/windows")
	case strings.Contains(hostText, "samsung"):
		addFingerprintSignal(item, "Android", "phone", 110, "hostname/samsung")
	case strings.Contains(hostText, "xiaomi"):
		addFingerprintSignal(item, "Android", "phone", 110, "hostname/xiaomi")
	case strings.Contains(hostText, "huawei"):
		addFingerprintSignal(item, "Android", "phone", 110, "hostname/huawei")
	case strings.Contains(hostText, "oppo"):
		addFingerprintSignal(item, "Android", "phone", 110, "hostname/oppo")
	case strings.Contains(hostText, "android") || strings.Contains(hostText, "pixel") || strings.Contains(hostText, "oneplus") || strings.Contains(hostText, "motorola"):
		addFingerprintSignal(item, "Android", "phone", 100, "hostname/android")
	}
	vendorText := strings.ToLower(strings.TrimSpace(vendor))
	switch {
	case containsAny(vendorText, "nintendo"):
		addFingerprintSignal(item, "nintendo", "gaming-console", 80, "vendor/nintendo")
	case containsAny(vendorText, "playstation", "sony computer entertainment", "sce"):
		addFingerprintSignal(item, "playstation", "gaming-console", 80, "vendor/playstation")
	case containsAny(vendorText, "xbox", "microsoft"):
		addFingerprintSignal(item, "xbox", "gaming-console", 70, "vendor/xbox")
	case containsAny(vendorText, "bravia", "sony visual", "sony tv"):
		addFingerprintSignal(item, "iot", "smart-tv", 80, "vendor/bravia")
	case containsAny(vendorText, "synology"):
		addFingerprintSignal(item, "nas", "nas", 70, "vendor/synology")
	case containsAny(vendorText, "qnap"):
		addFingerprintSignal(item, "nas", "nas", 70, "vendor/qnap")
	case containsAny(vendorText, "hewlett", "hp inc", "canon", "epson", "brother", "ricoh", "konica"):
		addFingerprintSignal(item, "printer", "printer", 70, "vendor/printer")
	case containsAny(vendorText, "yealink", "polycom"):
		addFingerprintSignal(item, "voip", "voip", 70, "vendor/voip")
	case containsAny(vendorText, "amazon"):
		addFingerprintSignal(item, "iot", "smart-speaker", 55, "vendor/amazon")
	case containsAny(vendorText, "google"):
		addFingerprintSignal(item, "Android", "", 20, "vendor/google")
	case containsAny(vendorText, "roku"):
		addFingerprintSignal(item, "iot", "smart-tv", 55, "vendor/roku")
	case containsAny(vendorText, "ring", "irobot", "roborock", "sonos", "philips", "eufy", "wyze", "tuya", "shelly", "aqara", "tp-link", "tplink", "panasonic", "espressif", "ecoflow", "atom tech"):
		addFingerprintSignal(item, "iot", "iot", 55, "vendor/iot")
	case strings.Contains(vendorText, "apple") && !strings.Contains(vendorText, "private"):
		addFingerprintSignal(item, "Apple", "", 35, "vendor/apple")
	case containsAny(vendorText, "samsung", "xiaomi", "huawei", "oppo"):
		addFingerprintSignal(item, "Android", "phone", 55, "vendor/android-oem")
	}
}

func applyDomainFingerprint(item *fingerprintAccumulator, name string) {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if name == "" {
		return
	}
	switch {
	case domainMatchesAny(name, "amazonalexa.com"):
		addUniqueFingerprintSignal(item, "iot", "smart-speaker", 110, "dns/amazon-echo:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "dms.amazon.com"):
		addUniqueFingerprintSignal(item, "iot", "smart-speaker", 70, "dns/amazon-device:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "googlecast.com"):
		addUniqueFingerprintSignal(item, "iot", "smart-tv", 110, "dns/googlecast:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "gvt1.com", "clients3.google.com", "l.google.com"):
		addUniqueFingerprintSignal(item, "iot", "smart-tv", 45, "dns/google-media:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "roku.com", "rokulabs.net"):
		addUniqueFingerprintSignal(item, "iot", "smart-tv", 120, "dns/roku:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "switchbot.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/switchbot:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "kasa-smart.com", "tplinkcloud.com", "tapo.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/tplink-iot:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "aqara.com", "lumiunited.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/aqara:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "tuyaus.com", "tuyaeu.com", "tuya.com", "smartlife.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/tuya:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "shelly.cloud", "shelly.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/shelly:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "nature.global", "nature.global.edgekey.net"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/nature-remo:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "broadlink.com.cn"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/broadlink:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "ecoflow.com"):
		addUniqueFingerprintSignal(item, "iot", "iot", 120, "dns/ecoflow:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "meethue.com"):
		addUniqueFingerprintSignal(item, "iot", "lighting", 120, "dns/hue:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "ring.com"):
		addUniqueFingerprintSignal(item, "iot", "camera", 120, "dns/ring:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "eufylife.com", "eufy.com", "anker.com", "wyze.com"):
		addUniqueFingerprintSignal(item, "iot", "camera", 120, "dns/eufy:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "irobotapi.com", "iadc.irobot.com", "roborock.com"):
		addUniqueFingerprintSignal(item, "iot", "vacuum", 120, "dns/vacuum:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "sonos.com"):
		addUniqueFingerprintSignal(item, "iot", "smart-speaker", 120, "dns/sonos:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "synology.com", "quickconnect.to"):
		addUniqueFingerprintSignal(item, "nas", "nas", 120, "dns/synology:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "qnap.com"):
		addUniqueFingerprintSignal(item, "nas", "nas", 120, "dns/qnap:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "hpconnected.com"):
		addUniqueFingerprintSignal(item, "printer", "printer", 120, "dns/hp-printer:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "cps.canon.jp", "epsonconnect.com"):
		addUniqueFingerprintSignal(item, "printer", "printer", 120, "dns/printer:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "hp.com", "canon.com", "epson.com", "epson.jp", "brother.com", "ricoh.com", "konicaminolta.com"):
		addUniqueFingerprintSignal(item, "printer", "printer", 65, "dns/printer:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "yealink.com", "polycom.com"):
		addUniqueFingerprintSignal(item, "voip", "voip", 120, "dns/voip:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "zoom.us", "zoomgov.com", "webex.com"):
		addUniqueFingerprintSignal(item, "voip", "voip", 25, "dns/conference:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "teams.microsoft.com", "skype.com"):
		addUniqueFingerprintSignal(item, "voip", "voip", 20, "dns/conference:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "zerotier.com"):
		addUniqueFingerprintSignal(item, "linux", "", 25, "dns/zerotier:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "samsung.com", "samsungcloud.com", "samsungelectronics.com"):
		addUniqueFingerprintSignal(item, "Android", "phone", 90, "dns/samsung:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "xiaomi.com", "mi.com"):
		addUniqueFingerprintSignal(item, "Android", "phone", 90, "dns/xiaomi:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "huawei.com", "hicloud.com"):
		addUniqueFingerprintSignal(item, "Android", "phone", 90, "dns/huawei:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "oppo.com"):
		addUniqueFingerprintSignal(item, "Android", "phone", 90, "dns/oppo:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "tesla.com", "teslamotors.com"):
		addUniqueFingerprintSignal(item, "iot", "ev", 120, "dns/tesla:"+shortFingerprintSignal(name))
	case containsAny(name, "bravia.dtv") || strings.Contains(name, "_androidtvremote."):
		addFingerprintSignal(item, "iot", "smart-tv", 100, "mdns/smart-tv")
	case domainMatchesAny(name, "nintendo.net", "npln.jp", "ndas.srv.nintendo.net", "gs.nintendo.net", "accounts.nintendo.com"):
		addUniqueFingerprintSignal(item, "nintendo", "gaming-console", 120, "dns/nintendo:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "playstation.net", "sonyentertainmentnetwork.com", "scea.com"):
		addUniqueFingerprintSignal(item, "playstation", "gaming-console", 120, "dns/playstation:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "xboxlive.com", "xbox.com"):
		addUniqueFingerprintSignal(item, "xbox", "gaming-console", 120, "dns/xbox:"+shortFingerprintSignal(name))
	case domainMatchesAny(name, "steampowered.com", "steamcontent.com"):
		addUniqueFingerprintSignal(item, "steam-os", "gaming-console", 120, "dns/steam:"+shortFingerprintSignal(name))
	case strings.Contains(name, "icloud.com") || strings.Contains(name, "apple.com") || strings.Contains(name, "mzstatic.com") || strings.Contains(name, "push.apple.com") || strings.Contains(name, "captive.apple.com"):
		addFingerprintSignal(item, "Apple", "", 35, "dns/apple:"+shortFingerprintSignal(name))
	case strings.Contains(name, "windowsupdate.com") || strings.Contains(name, "msftconnecttest.com") || strings.Contains(name, "microsoft.com") || strings.Contains(name, "office365.com") || strings.Contains(name, "live.com"):
		addFingerprintSignal(item, "Windows", "computer", 35, "dns/windows:"+shortFingerprintSignal(name))
	case strings.Contains(name, "connectivitycheck.gstatic.com") || strings.Contains(name, "android.clients.google.com") || strings.Contains(name, "gms.") || strings.Contains(name, "googleapis.com"):
		addUniqueFingerprintSignal(item, "Android", "", 20, "dns/android:"+shortFingerprintSignal(name))
	case strings.Contains(name, "_airplay.") || strings.Contains(name, "_raop.") || strings.Contains(name, "_companion-link.") || strings.Contains(name, "_homekit."):
		addFingerprintSignal(item, "Apple", "", 80, "mdns/apple")
	case strings.Contains(name, "_googlecast.") || strings.Contains(name, "_androidtvremote."):
		addFingerprintSignal(item, "iot", "smart-tv", 80, "mdns/googlecast")
	case strings.Contains(name, "_smb.") || strings.Contains(name, "_workstation.") || strings.Contains(name, "wpad."):
		addFingerprintSignal(item, "Windows", "computer", 35, "dns/windows-service")
	case domainMatchesAny(name, "amazonaws.com"):
		addUniqueFingerprintSignal(item, "iot", "", 15, "dns/aws-device:"+shortFingerprintSignal(name))
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func domainMatchesAny(name string, suffixes ...string) bool {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	for _, suffix := range suffixes {
		suffix = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(suffix)), ".")
		if suffix == "" {
			continue
		}
		if name == suffix || strings.HasSuffix(name, "."+suffix) {
			return true
		}
	}
	return false
}

func applyTransportFingerprint(item *fingerprintAccumulator, proto, peer string, port int) {
	proto = strings.ToLower(strings.TrimSpace(proto))
	peer = strings.ToLower(strings.TrimSpace(peer))
	if proto != "udp" {
		return
	}
	switch {
	case port == 5353 || peer == "224.0.0.251" || peer == "ff02::fb":
		item.hasMulticast = true
		addFingerprintSignal(item, "", "", 20, "multicast/mdns")
	case port == 1900 || peer == "239.255.255.250" || peer == "ff02::c":
		item.hasMulticast = true
		addFingerprintSignal(item, "iot", "iot", 55, "multicast/ssdp")
	case port == 137 || port == 138 || port == 139:
		item.hasMulticast = true
		addFingerprintSignal(item, "Windows", "computer", 60, "multicast/netbios")
	}
}

func applyAppFingerprint(item *fingerprintAccumulator, app, category string, confidence int) {
	text := strings.ToLower(strings.Join([]string{app, category}, " "))
	if text == "" {
		return
	}
	weight := 35
	if confidence >= 80 {
		weight = 70
	}
	switch {
	case strings.Contains(text, "mdns"):
		addFingerprintSignal(item, "", "", maxInt(weight, 80), "dpi/mdns")
	case strings.Contains(text, "ssdp"):
		addFingerprintSignal(item, "iot", "iot", maxInt(weight, 55), "dpi/ssdp")
	case strings.Contains(text, "netbios") || strings.Contains(text, "smb"):
		addFingerprintSignal(item, "Windows", "computer", maxInt(weight, 60), "dpi/netbios")
	}
}

func addFingerprintSignal(item *fingerprintAccumulator, osFamily, deviceClass string, score int, signal string) {
	if item == nil {
		return
	}
	if item.osScores == nil {
		item.osScores = map[string]int{}
	}
	if item.classScores == nil {
		item.classScores = map[string]int{}
	}
	if item.osClassScores == nil {
		item.osClassScores = map[string]int{}
	}
	if item.signals == nil {
		item.signals = map[string]bool{}
	}
	if item.signalScores == nil {
		item.signalScores = map[string]fingerprintSignalScore{}
	}
	if signal != "" {
		item.signals[signal] = true
		contribution := item.signalScores[signal]
		if contribution.OSFamily == "" {
			contribution.OSFamily = osFamily
		}
		if contribution.DeviceClass == "" {
			contribution.DeviceClass = deviceClass
		}
		contribution.Score += score
		item.signalScores[signal] = contribution
	}
	if osFamily != "" {
		item.osScores[osFamily] += score
	}
	if deviceClass != "" {
		item.classScores[deviceClass] += score
	}
	if osFamily != "" && deviceClass != "" {
		item.osClassScores[osClassScoreKey(osFamily, deviceClass)] += score
	}
}

func addUniqueFingerprintSignal(item *fingerprintAccumulator, osFamily, deviceClass string, score int, signal string) {
	if item != nil && signal != "" && item.signals != nil && item.signals[signal] {
		return
	}
	addFingerprintSignal(item, osFamily, deviceClass, score, signal)
}

func applyDHCPFingerprint(item *fingerprintAccumulator, fp *logstore.DHCPFingerprint) {
	if item == nil || fp == nil {
		return
	}
	applyHostVendorFingerprint(item, fp.Hostname, fp.VendorClass, "")
	if fp.Hostname != "" {
		item.hostname = fp.Hostname
	}
	if fp.VendorClass != "" {
		item.vendor = fp.VendorClass
	}
	osFamily := fp.OSFamily
	deviceClass := fp.DeviceClass
	confidence := fp.Confidence
	signal := fp.Signal
	if osFamily == "" && deviceClass == "" && len(fp.RequestedOptions) > 0 {
		match := dhcpfingerprint.Infer(dhcpfingerprint.Fingerprint{
			MAC:              fp.MAC,
			Hostname:         fp.Hostname,
			VendorClass:      fp.VendorClass,
			RequestedOptions: fp.RequestedOptions,
			ObservedAt:       fp.ObservedAt,
			Source:           fp.Source,
		})
		osFamily = match.OSFamily
		deviceClass = match.DeviceClass
		confidence = match.Confidence
		signal = match.Signal
	}
	if osFamily == "" && deviceClass == "" {
		return
	}
	if confidence <= 0 {
		confidence = 75
	}
	if signal == "" {
		signal = "dhcp-fingerprint"
	}
	if fp.DeviceName != "" {
		signal += ":" + strings.ToLower(strings.ReplaceAll(fp.DeviceName, " ", "-"))
	}
	addFingerprintSignal(item, osFamily, deviceClass, confidence, signal)
}

func latestDHCPFingerprintByMAC(groups ...[]logstore.DHCPFingerprint) map[string]*logstore.DHCPFingerprint {
	out := map[string]*logstore.DHCPFingerprint{}
	for _, group := range groups {
		for _, fp := range group {
			mac := normalizeClientMAC(fp.MAC)
			if mac == "" {
				continue
			}
			current := out[mac]
			next := fp
			if current == nil || next.ObservedAt.After(current.ObservedAt) {
				out[mac] = &next
			}
		}
	}
	return out
}

func matchFingerprintToClient(rows map[string]*clientMutableEntry, ip string, fingerprint *fingerprintAccumulator) string {
	if fingerprint == nil {
		return ""
	}
	fp := fingerprint.result()
	if fp.Confidence < 60 || fp.OSFamily == "" {
		return ""
	}
	var matched string
	var samePrefixMatched string
	for key, row := range rows {
		if row.MAC == "" {
			continue
		}
		rowFP := inferClientFingerprint(row.ClientEntry, nil, nil)
		if rowFP.OSFamily != fp.OSFamily {
			continue
		}
		if fp.DeviceClass != "" && rowFP.DeviceClass != "" && fp.DeviceClass != rowFP.DeviceClass {
			continue
		}
		if clientHasSameIPv6Prefix(row.addresses, ip, 64) {
			if samePrefixMatched != "" {
				return ""
			}
			samePrefixMatched = key
			continue
		}
		if matched != "" {
			return ""
		}
		matched = key
	}
	if samePrefixMatched != "" {
		return samePrefixMatched
	}
	return matched
}

func clientHasSameIPv6Prefix(addresses map[string]bool, ip string, bits int) bool {
	addrText, _, _ := strings.Cut(strings.TrimSpace(ip), "/")
	addr, err := netip.ParseAddr(addrText)
	if err != nil || !addr.Is6() || addr.Is4In6() {
		return false
	}
	prefix := netip.PrefixFrom(addr, bits).Masked()
	for candidate := range addresses {
		candidateText, _, _ := strings.Cut(strings.TrimSpace(candidate), "/")
		other, err := netip.ParseAddr(candidateText)
		if err == nil && other.Is6() && !other.Is4In6() && prefix.Contains(other) {
			return true
		}
	}
	return false
}

func passiveCorrelationKey(fingerprint *fingerprintAccumulator, ip string) string {
	if fingerprint == nil {
		return ip
	}
	fp := fingerprint.result()
	if fp.Confidence >= 60 && fingerprint.hostname != "" {
		return "host:" + strings.ToLower(fingerprint.hostname)
	}
	return ip
}

func inferClientFingerprint(entry ClientEntry, passive map[string]*fingerprintAccumulator, dhcpFingerprint *logstore.DHCPFingerprint) clientFingerprint {
	acc := &fingerprintAccumulator{osScores: map[string]int{}, classScores: map[string]int{}, osClassScores: map[string]int{}, signals: map[string]bool{}, signalScores: map[string]fingerprintSignalScore{}}
	applyHostVendorFingerprint(acc, entry.Hostname, entry.Vendor, "")
	applyDHCPFingerprint(acc, dhcpFingerprint)
	for _, peer := range entry.Peers {
		applyDomainFingerprint(acc, peer)
	}
	for _, address := range entry.Addresses {
		if passive != nil {
			acc.merge(passive[address])
		}
	}
	return acc.result()
}

func (f *fingerprintAccumulator) merge(other *fingerprintAccumulator) {
	if f == nil || other == nil {
		return
	}
	if f.osScores == nil {
		f.osScores = map[string]int{}
	}
	if f.classScores == nil {
		f.classScores = map[string]int{}
	}
	if f.osClassScores == nil {
		f.osClassScores = map[string]int{}
	}
	if f.signals == nil {
		f.signals = map[string]bool{}
	}
	if f.signalScores == nil {
		f.signalScores = map[string]fingerprintSignalScore{}
	}
	if len(other.signalScores) > 0 {
		for signal, contribution := range other.signalScores {
			if f.signals[signal] {
				continue
			}
			addFingerprintSignal(f, contribution.OSFamily, contribution.DeviceClass, contribution.Score, signal)
		}
	} else {
		for key, value := range other.osScores {
			f.osScores[key] += value
		}
		for key, value := range other.classScores {
			f.classScores[key] += value
		}
		for key, value := range other.osClassScores {
			f.osClassScores[key] += value
		}
		for key := range other.signals {
			f.signals[key] = true
		}
	}
	f.hostname = firstNonEmptyString(f.hostname, other.hostname)
	f.vendor = firstNonEmptyString(f.vendor, other.vendor)
	f.hasMulticast = f.hasMulticast || other.hasMulticast
}

func (f *fingerprintAccumulator) result() clientFingerprint {
	if f == nil {
		return clientFingerprint{}
	}
	osFamily, osScore := bestFingerprintScore(f.osScores)
	deviceClass, classScore := f.bestDeviceClassForOS(osFamily)
	confidence := osScore
	if classScore > 0 {
		confidence += classScore / 3
	}
	if len(f.signals) > 1 {
		confidence += 10
	}
	if confidence > 100 {
		confidence = 100
	}
	if confidence < 25 {
		return clientFingerprint{}
	}
	return clientFingerprint{
		OSFamily:    osFamily,
		DeviceClass: deviceClass,
		Confidence:  confidence,
		Signals:     sortedSet(f.signals),
		Hostname:    f.hostname,
		Vendor:      f.vendor,
	}
}

func (f *fingerprintAccumulator) bestDeviceClassForOS(osFamily string) (string, int) {
	if osFamily != "" {
		filtered := map[string]int{}
		prefix := osFamily + "|"
		for key, value := range f.osClassScores {
			if strings.HasPrefix(key, prefix) {
				filtered[strings.TrimPrefix(key, prefix)] += value
			}
		}
		if len(filtered) > 0 {
			return bestFingerprintScore(filtered)
		}
	}
	return bestFingerprintScore(f.classScores)
}

func osClassScoreKey(osFamily, deviceClass string) string {
	return osFamily + "|" + deviceClass
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func bestFingerprintScore(scores map[string]int) (string, int) {
	var best string
	var bestScore int
	for key, score := range scores {
		if score > bestScore || (score == bestScore && key < best) {
			best = key
			bestScore = score
		}
	}
	return best, bestScore
}

func shortFingerprintSignal(name string) string {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	parts := strings.Split(name, ".")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return name
}

func macVendor(mac string) string {
	oui := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(mac), "-", ":"))
	parts := strings.Split(oui, ":")
	if len(parts) < 3 {
		return ""
	}
	oui = strings.Join(parts[:3], ":")
	vendors := map[string]string{
		"00:01:4A": "Sony",
		"00:13:A9": "Sony",
		"00:16:B8": "Sony",
		"00:19:C5": "Sony",
		"00:1A:80": "Sony",
		"00:1D:0D": "Sony",
		"00:24:BE": "Sony",
		"00:50:F2": "Microsoft Xbox",
		"00:F6:20": "Google",
		"04:03:D6": "Nintendo",
		"04:5D:4B": "Sony",
		"0C:56:5C": "Nintendo",
		"18:2A:7B": "Nintendo",
		"18:74:2E": "Amazon",
		"18:EC:E7": "Panasonic",
		"20:16:D8": "Microsoft Xbox",
		"28:18:78": "Microsoft Xbox",
		"30:24:32": "Nintendo",
		"30:59:B7": "Microsoft Xbox",
		"30:75:12": "Sony",
		"34:AF:2C": "Nintendo",
		"3C:A9:AB": "Apple",
		"40:F4:07": "Nintendo",
		"44:65:0D": "Amazon",
		"48:A5:E7": "Nintendo",
		"48:D6:D5": "Google",
		"4E:20:15": "Apple private address",
		"50:1A:C5": "Microsoft Xbox",
		"50:F5:DA": "Amazon",
		"54:42:49": "Sony",
		"58:BD:A3": "Nintendo",
		"5C:BA:37": "Microsoft Xbox",
		"60:45:BD": "Microsoft Xbox",
		"60:6B:BD": "Sony",
		"64:E8:33": "EcoFlow",
		"68:54:FD": "Amazon",
		"70:48:F7": "Nintendo",
		"70:77:81": "Sony",
		"74:C2:46": "Amazon",
		"7C:1E:52": "Microsoft Xbox",
		"7C:BB:8A": "Nintendo",
		"7C:DD:E9": "ATOM tech Inc.",
		"80:81:9F": "Nintendo",
		"84:C7:EA": "Sony",
		"84:D6:D0": "Amazon",
		"88:71:E5": "Amazon",
		"8C:CD:E8": "Nintendo",
		"98:41:5C": "Nintendo",
		"98:5F:D3": "Microsoft Xbox",
		"A4:C0:E1": "Nintendo",
		"AC:63:BE": "Amazon",
		"AC:9B:0A": "Sony",
		"B4:52:7D": "Sony",
		"B8:68:70": "Apple",
		"B8:78:2E": "Nintendo",
		"CC:9E:00": "Nintendo",
		"D8:10:68": "Amazon",
		"D8:9D:67": "Microsoft Xbox",
		"E0:E7:51": "Nintendo",
		"E8:4E:CE": "Nintendo",
		"EC:FA:BC": "Espressif",
		"F0:BF:97": "Sony",
		"FC:A1:83": "Amazon",
	}
	if vendor, ok := vendors[oui]; ok {
		return vendor
	}
	return "OUI " + oui
}
