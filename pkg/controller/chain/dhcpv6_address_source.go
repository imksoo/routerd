// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func (r *Runner) currentDHCPv6AddressSource(declared *api.Router, name string, store eventedStore) (dhcpv6AddressSource, error) {
	var source dhcpv6AddressSource
	prepared, err := r.currentPreparedRouter()
	if err != nil {
		return source, err
	}
	if prepared.declared != declared {
		return source, fmt.Errorf("DHCPv6 configuration changed")
	}
	workers, err := observedDHCPv6Workers(prepared.startup.Router(), store.withRouter(declared))
	if err != nil {
		return source, err
	}
	var worker dhcpv6Worker
	for _, w := range workers {
		if w.Address == name {
			worker = w
			break
		}
	}
	if worker.Address == "" {
		return source, fmt.Errorf("DHCPv6Address is no longer active")
	}
	source.Config = routerstate.DHCPv6AddressSource{Worker: worker.Name, IfName: worker.IfName, IAID: 1, Mode: pdclient.RequestAddresses}
	if worker.Prefix != nil {
		source.Config.Mode = pdclient.RequestPrefixAndAddress
		if worker.Prefix.IAID != "" {
			iaid, e := strconv.ParseUint(worker.Prefix.IAID, 10, 32)
			if e != nil {
				return source, e
			}
			source.Config.IAID = uint32(iaid)
		}
		source.Config.ClientDUID = strings.ToLower(strings.ReplaceAll(worker.Prefix.ClientDUID, ":", ""))
	}
	if source.Config.ClientDUID == "" {
		iface, e := net.InterfaceByName(worker.IfName)
		if e != nil {
			return source, e
		}
		source.Config.ClientDUID = hex.EncodeToString(append([]byte{0, 3, 0, 1}, iface.HardwareAddr...))
	}
	source.Socket = worker.socket(r.Opts.DaemonSockets)
	wanted := worker.daemonSpec()
	key := supervisedDaemonKey(wanted.Binary, worker.Name)
	r.supervisedMu.Lock()
	state, exists := r.clientDaemonStates[key]
	r.supervisedMu.Unlock()
	if !exists || state.Spec.OwnerToken == "" || supervisedDaemonSpecHash(state.Spec) != supervisedDaemonSpecHash(wanted) {
		return source, nil
	}
	select {
	case <-state.Done:
		return source, nil
	default:
	}
	markers, err := readSupervisedDaemonMarkers()
	if err != nil {
		return source, err
	}
	marker, exists := markers[key]
	if !exists || marker.SpecHash != supervisedDaemonSpecHash(wanted) || marker.OwnerToken != state.Spec.OwnerToken {
		return source, nil
	}
	pids, err := matchingOwnedSupervisedDaemonPIDs(marker)
	if err != nil {
		return source, err
	}
	if len(pids) != 1 {
		return source, nil
	}
	source.Available = true
	source.SupervisorOwner = state.Spec.OwnerToken
	return source, nil
}

func dhcpv6AddressHTTPClient(socket string) *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}}
}
func readDHCPv6AddressLease(ctx context.Context, source dhcpv6AddressSource) (pdclient.Snapshot, error) {
	client := dhcpv6AddressHTTPClient(source.Socket)
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/lease", nil)
	if err != nil {
		return pdclient.Snapshot{}, err
	}
	response, err := client.Do(req)
	if err != nil {
		return pdclient.Snapshot{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || source.SupervisorOwner == "" || response.Header.Get(daemonapi.DHCPv6SupervisorOwnerHeader) != source.SupervisorOwner {
		return pdclient.Snapshot{}, fmt.Errorf("DHCPv6 lease endpoint owner or status differs")
	}
	return decodeDaemonPDLease(response.Body)
}
func declineDHCPv6Address(ctx context.Context, source dhcpv6AddressSource, request daemonapi.DHCPv6DeclineRequest) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	client := dhcpv6AddressHTTPClient(source.Socket)
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/commands/decline", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var result daemonapi.CommandResult
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || !result.Accepted {
		return fmt.Errorf("DHCPv6 Decline rejected: %s", result.Message)
	}
	return nil
}
