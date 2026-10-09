// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/hostcmd"
	"github.com/imksoo/routerd/pkg/tailscale"
)

func (h Handler) vpn(w http.ResponseWriter) {
	status, err := h.vpnStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, status)
}

func (h Handler) vpnStatus() (VPNStatus, error) {
	if h.opts.VPNStatus != nil {
		return h.opts.VPNStatus()
	}
	return hostVPNStatus()
}

func hostVPNStatus() (VPNStatus, error) {
	var status VPNStatus
	if out, err := commandOutputTimeout(2*time.Second, "wg", "show", "all", "dump"); err != nil {
		status.Errors = append(status.Errors, err.Error())
	} else {
		interfaces, err := parseWireGuardAllDump(out)
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
		} else {
			status.WireGuard = interfaces
		}
	}
	if out, err := commandOutputTimeout(2*time.Second, "tailscale", "status", "--json"); err != nil {
		status.Errors = append(status.Errors, err.Error())
	} else {
		tailscale, err := parseTailscaleStatusJSON(out)
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
		} else {
			status.Tailscale = tailscale
		}
	}
	return status, nil
}

func commandOutputTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	commandName := hostCommandPath(name)
	out, err := exec.CommandContext(ctx, commandName, args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("%s %s timed out", name, strings.Join(args, " "))
	}
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, message)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

func hostCommandPath(name string) string {
	return hostcmd.Resolve(name)
}

func parseWireGuardAllDump(data []byte) ([]WireGuardInterfaceStatus, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, nil
	}
	interfaces := map[string]*WireGuardInterfaceStatus{}
	ensure := func(name string) *WireGuardInterfaceStatus {
		item := interfaces[name]
		if item == nil {
			item = &WireGuardInterfaceStatus{Name: name}
			interfaces[name] = item
		}
		return item
	}
	for lineNo, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		switch {
		case len(fields) == 5:
			item := ensure(fields[0])
			item.PublicKey = wireGuardValue(fields[2])
			item.ListenPort = parseWireGuardInt(fields[3])
			item.FwMark = wireGuardValue(fields[4])
		case len(fields) >= 9:
			item := ensure(fields[0])
			peer := WireGuardPeerStatus{
				PublicKey:              wireGuardValue(fields[1]),
				Endpoint:               wireGuardValue(fields[3]),
				AllowedIPs:             splitWireGuardList(fields[4]),
				LatestHandshake:        parseWireGuardHandshake(fields[5]),
				TransferRxBytes:        parseWireGuardInt64(fields[6]),
				TransferTxBytes:        parseWireGuardInt64(fields[7]),
				PersistentKeepaliveSec: parseWireGuardInt(fields[8]),
			}
			item.Peers = append(item.Peers, peer)
		default:
			return nil, fmt.Errorf("wg dump line %d has %d fields", lineNo+1, len(fields))
		}
	}
	out := make([]WireGuardInterfaceStatus, 0, len(interfaces))
	for _, item := range interfaces {
		sort.Slice(item.Peers, func(i, j int) bool {
			return item.Peers[i].PublicKey < item.Peers[j].PublicKey
		})
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func wireGuardValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "(none)" || value == "off" {
		return ""
	}
	return value
}

func splitWireGuardList(value string) []string {
	value = wireGuardValue(value)
	if value == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func parseWireGuardInt(value string) int {
	value = wireGuardValue(value)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func parseWireGuardInt64(value string) int64 {
	value = wireGuardValue(value)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func parseWireGuardHandshake(value string) time.Time {
	seconds := parseWireGuardInt64(value)
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

func parseTailscaleStatusJSON(data []byte) (*TailscaleStatus, error) {
	status, err := tailscale.ParseStatusJSON(data)
	if err != nil {
		return nil, err
	}
	if status.BackendState == "" && status.DNSName == "" && len(status.Peers) == 0 {
		return nil, nil
	}
	out := &TailscaleStatus{
		BackendState:    status.BackendState,
		TailnetName:     status.TailnetName,
		MagicDNSSuffix:  status.MagicDNSSuffix,
		MagicDNSEnabled: status.MagicDNSEnabled,
		CertDomains:     status.CertDomains,
		HostName:        status.HostName,
		DNSName:         status.DNSName,
		TailscaleIPs:    status.TailscaleIPs,
		AllowedIPs:      status.AllowedIPs,
		Online:          status.Online,
		Active:          status.Active,
		ExitNode:        status.ExitNode,
		ExitNodeOption:  status.ExitNodeOption,
	}
	for _, peer := range status.Peers {
		out.Peers = append(out.Peers, TailscalePeerStatus{
			ID:             peer.ID,
			HostName:       peer.HostName,
			DNSName:        peer.DNSName,
			TailscaleIPs:   peer.TailscaleIPs,
			AllowedIPs:     peer.AllowedIPs,
			Online:         peer.Online,
			Active:         peer.Active,
			ExitNode:       peer.ExitNode,
			ExitNodeOption: peer.ExitNodeOption,
			Relay:          peer.Relay,
			LastSeen:       peer.LastSeen,
			RxBytes:        peer.RxBytes,
			TxBytes:        peer.TxBytes,
		})
	}
	return out, nil
}
