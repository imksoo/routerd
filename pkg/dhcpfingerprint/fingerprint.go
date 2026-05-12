// SPDX-License-Identifier: BSD-3-Clause

package dhcpfingerprint

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Fingerprint struct {
	MAC              string
	Hostname         string
	VendorClass      string
	RequestedOptions []int
	ObservedAt       time.Time
	Source           string
}

type Match struct {
	OSFamily    string
	DeviceClass string
	DeviceName  string
	Confidence  int
	Signal      string
}

type Event struct {
	MAC              string
	Interface        string
	Hostname         string
	VendorClass      string
	RequestedOptions []int
	ObservedAt       time.Time
	Source           string
}

var (
	macPattern   = regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b`)
	ifacePattern = regexp.MustCompile(`\(([A-Za-z0-9_.:-]+)\)`)
)

func ParseDnsmasqLine(line string, now time.Time) (Event, bool) {
	message := normalizeDnsmasqMessage(line)
	if message == "" {
		return Event{}, false
	}
	event := Event{ObservedAt: now.UTC(), Source: "dnsmasq-log-dhcp"}
	if event.ObservedAt.IsZero() {
		event.ObservedAt = time.Now().UTC()
	}
	if match := macPattern.FindString(message); match != "" {
		event.MAC = NormalizeMAC(match)
	}
	if match := ifacePattern.FindStringSubmatch(message); len(match) == 2 {
		event.Interface = strings.TrimSpace(match[1])
	}
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "client provides name:"):
		event.Hostname = afterColon(message)
	case strings.Contains(lower, "vendor class:"):
		event.VendorClass = afterColon(message)
	case strings.Contains(lower, "requested options:"):
		event.RequestedOptions = parseRequestedOptions(afterColon(message))
	}
	return event, event.MAC != "" || event.Hostname != "" || event.VendorClass != "" || len(event.RequestedOptions) > 0
}

func Infer(fp Fingerprint) Match {
	text := strings.ToLower(strings.Join([]string{fp.Hostname, fp.VendorClass}, " "))
	options := fp.RequestedOptions
	switch {
	case containsAny(text, "nintendo", "nintendo switch"):
		return Match{OSFamily: "nintendo", DeviceClass: "gaming-console", DeviceName: "Nintendo Switch", Confidence: 95, Signal: "dhcp-fingerprint/nintendo"}
	case containsAny(text, "playstation", "sony computer entertainment", "sce"):
		return Match{OSFamily: "playstation", DeviceClass: "gaming-console", DeviceName: "PlayStation", Confidence: 90, Signal: "dhcp-fingerprint/playstation"}
	case strings.Contains(text, "xbox"):
		return Match{OSFamily: "xbox", DeviceClass: "gaming-console", DeviceName: "Xbox", Confidence: 90, Signal: "dhcp-fingerprint/xbox"}
	case containsAny(text, "msft", "microsoft"):
		return Match{OSFamily: "Windows", DeviceClass: "computer", DeviceName: "Windows", Confidence: 90, Signal: "dhcp-fingerprint/windows-vendor"}
	case containsAny(text, "android", "samsung", "xiaomi", "huawei", "oppo", "pixel"):
		return Match{OSFamily: "Android", DeviceClass: "phone", DeviceName: "Android", Confidence: 88, Signal: "dhcp-fingerprint/android-vendor"}
	case containsAny(text, "iphone", "ipad"):
		return Match{OSFamily: "Apple", DeviceClass: appleMobileClass(text), DeviceName: "iOS/iPadOS", Confidence: 88, Signal: "dhcp-fingerprint/apple-hostname"}
	case containsAny(text, "macbook", "imac", "mac mini"):
		return Match{OSFamily: "Apple", DeviceClass: "computer", DeviceName: "macOS", Confidence: 86, Signal: "dhcp-fingerprint/macos-hostname"}
	case containsAny(text, "hp", "hewlett", "canon", "epson", "brother", "ricoh", "konica"):
		return Match{OSFamily: "printer", DeviceClass: "printer", DeviceName: "Printer", Confidence: 82, Signal: "dhcp-fingerprint/printer-vendor"}
	case containsAny(text, "synology", "qnap"):
		return Match{OSFamily: "nas", DeviceClass: "nas", DeviceName: "NAS", Confidence: 84, Signal: "dhcp-fingerprint/nas-vendor"}
	case isWindowsPRL(options):
		return Match{OSFamily: "Windows", DeviceClass: "computer", DeviceName: "Windows", Confidence: 86, Signal: "dhcp-fingerprint/windows-prl"}
	case isAndroidPRL(options):
		return Match{OSFamily: "Android", DeviceClass: "phone", DeviceName: "Android", Confidence: 82, Signal: "dhcp-fingerprint/android-prl"}
	case isApplePRL(options):
		return Match{OSFamily: "Apple", DeviceClass: "", DeviceName: "Apple", Confidence: 72, Signal: "dhcp-fingerprint/apple-prl"}
	}
	return Match{}
}

func NormalizeMAC(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeDnsmasqMessage(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if idx := strings.Index(line, "dnsmasq-dhcp"); idx >= 0 {
		line = line[idx:]
	}
	if before, after, ok := strings.Cut(line, "]:"); ok && strings.Contains(before, "dnsmasq-dhcp") {
		line = after
	}
	return strings.TrimSpace(line)
}

func afterColon(value string) string {
	_, after, ok := strings.Cut(value, ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(after)
}

func parseRequestedOptions(value string) []int {
	var out []int
	for _, token := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	}) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if before, _, ok := strings.Cut(token, ":"); ok {
			token = before
		}
		n, err := strconv.Atoi(token)
		if err != nil || n <= 0 {
			continue
		}
		out = append(out, n)
	}
	return out
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func appleMobileClass(text string) string {
	if strings.Contains(text, "ipad") {
		return "tablet"
	}
	return "phone"
}

func isWindowsPRL(options []int) bool {
	return hasPrefix(options, []int{1, 15, 3, 6}) || hasSubsequence(options, []int{44, 46, 47}) || hasSubsequence(options, []int{249, 252})
}

func isAndroidPRL(options []int) bool {
	return hasPrefix(options, []int{1, 3, 6, 15}) && hasSubsequence(options, []int{26, 28, 51, 58, 59})
}

func isApplePRL(options []int) bool {
	return hasPrefix(options, []int{1, 121, 3, 6}) || hasSubsequence(options, []int{119, 252})
}

func hasPrefix(values []int, prefix []int) bool {
	if len(values) < len(prefix) {
		return false
	}
	for i := range prefix {
		if values[i] != prefix[i] {
			return false
		}
	}
	return true
}

func hasSubsequence(values []int, seq []int) bool {
	if len(seq) == 0 {
		return true
	}
	pos := 0
	for _, value := range values {
		if value == seq[pos] {
			pos++
			if pos == len(seq) {
				return true
			}
		}
	}
	return false
}
