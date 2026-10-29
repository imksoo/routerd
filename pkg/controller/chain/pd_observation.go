// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func (s eventedStore) ReadPDLease(name string) (routerstate.PDLease, bool, error) {
	reader, ok := s.Store.(routerstate.PDLeaseReader)
	if !ok {
		return routerstate.PDLease{}, false, fmt.Errorf("PD observation reader unavailable")
	}
	return reader.ReadPDLease(name)
}

func (s eventedStore) ConfigurePDLease(name string, config routerstate.PDLeaseConfig) (routerstate.PDLease, bool, error) {
	store, ok := s.Store.(routerstate.PDLeaseStore)
	if !ok {
		return routerstate.PDLease{}, false, fmt.Errorf("PD observation writer unavailable")
	}
	return store.ConfigurePDLease(name, config)
}

func (s eventedStore) SavePDLease(name string, lease routerstate.PDLease) error {
	store, ok := s.Store.(routerstate.PDLeaseStore)
	if !ok {
		return fmt.Errorf("PD observation writer unavailable")
	}
	return store.SavePDLease(name, lease)
}

func (s eventedStore) SavePDLeaseSnapshot(name string, snapshot pdclient.Snapshot) error {
	store, ok := s.Store.(routerstate.PDLeaseStore)
	if !ok {
		return fmt.Errorf("PD observation writer unavailable")
	}
	return store.SavePDLeaseSnapshot(name, snapshot)
}

func livePDLease(store Store, name string, now time.Time) (routerstate.PDLease, bool, error) {
	reader, ok := store.(routerstate.PDLeaseReader)
	if !ok {
		return routerstate.PDLease{}, false, fmt.Errorf("PD observation reader unavailable")
	}
	lease, found, err := reader.ReadPDLease(name)
	return lease.Live(now), found, err
}

// Known lease deadlines remain authoritative while the helper is unavailable.
// Socket failure alone never revokes a lease or supplies a new transaction.
func (c DaemonStatusController) expireKnownPDLeases(ctx context.Context) error {
	if c.Router == nil {
		return nil
	}
	reader, ok := c.Store.(routerstate.PDLeaseReader)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	if clock, ok := c.Store.(interface{ Now() time.Time }); ok {
		now = clock.Now()
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv6PrefixDelegation" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		previous, found, err := reader.ReadPDLease(resource.Metadata.Name)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		next := previous.Live(now)
		if next.CurrentPrefix == previous.CurrentPrefix && next.Phase == previous.Phase && len(next.PreviousPrefixes) == len(previous.PreviousPrefixes) {
			continue
		}
		if err := RecordPDLeaseObservation(c.Store, c.Bus, c.Router, resource.Metadata.Name, previous.Config(), next, nil, daemonapi.ResourceStatus{}); err != nil {
			return err
		}
	}
	return nil
}

func (c DaemonStatusController) resetPDLeaseSourceForUnavailableSocket(ctx context.Context, socket string) error {
	defaults, _ := platform.Current()
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv6PrefixDelegation" {
			continue
		}
		expectedSocket := c.DaemonSockets[resource.Metadata.Name]
		if expectedSocket == "" {
			expectedSocket = filepath.Join(defaults.RuntimeDir, "dhcpv6-client", resource.Metadata.Name+".sock")
		}
		if expectedSocket != socket {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		previous, configured, err := c.configuredPDLease(resource.Metadata.Name)
		if err != nil {
			return err
		}
		path := defaultDHCPv6PDLeaseFile(resource.Metadata.Name)
		if c.PDLeaseSnapshotPath != nil {
			path = c.PDLeaseSnapshotPath(resource.Metadata.Name)
		}
		snapshot, readErr := readPDClientSnapshotFile(path)
		var accepted *pdclient.Snapshot
		if readErr == nil && snapshot.UpdatedAt.After(configured.SnapshotUpdatedAt) {
			if err := routerstate.ValidatePDLeaseSnapshot(configured, resource.Metadata.Name, snapshot); err == nil {
				accepted = &snapshot
			}
		}
		if configured.Config() != previous.Config() || accepted != nil {
			if err := RecordPDLeaseObservation(c.Store, c.Bus, c.Router, resource.Metadata.Name, previous.Config(), configured, accepted, daemonapi.ResourceStatus{}); err != nil {
				return err
			}
		}
		// Unavailable or foreign source bytes never revoke a same-source lease.
		// An unreadable local file is unknown, and a real source change above still
		// invalidates the previous source atomically before reporting that error.
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}

	}
	return nil
}

func (c DaemonStatusController) configuredPDLease(name string) (routerstate.PDLease, routerstate.PDLease, error) {
	store, ok := c.Store.(routerstate.PDLeaseReader)
	if !ok {
		return routerstate.PDLease{}, routerstate.PDLease{}, fmt.Errorf("PD observation writer unavailable")
	}
	var spec api.DHCPv6PrefixDelegationSpec
	found := false
	if c.Router != nil {
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind == "DHCPv6PrefixDelegation" && resource.Metadata.Name == name {
				var err error
				spec, err = resource.DHCPv6PrefixDelegationSpec()
				if err != nil {
					return routerstate.PDLease{}, routerstate.PDLease{}, err
				}
				found = true
				break
			}
		}
	}
	if !found {
		return routerstate.PDLease{}, routerstate.PDLease{}, fmt.Errorf("DHCPv6PrefixDelegation/%s is no longer configured", name)
	}
	if !api.ValidIPv6PDClient(spec.Client) {
		return routerstate.PDLease{}, routerstate.PDLease{}, fmt.Errorf("DHCPv6PrefixDelegation/%s spec.client %q is unsupported; use routerd-dhcpv6-client or omit client", name, spec.Client)
	}

	ifname := interfaceIfName(c.Router, spec.Interface)
	if ifname == "" {
		ifname = spec.Interface
	}
	previous, _, err := store.ReadPDLease(name)
	if err != nil {
		return routerstate.PDLease{}, routerstate.PDLease{}, err
	}
	duid := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(spec.ClientDUID), ":", ""))
	if duid == "" {
		ifi, err := net.InterfaceByName(ifname)
		if err != nil {
			return previous, routerstate.PDLease{}, fmt.Errorf("read interface %s for default DUID: %w", ifname, err)
		}
		duid = hex.EncodeToString(append([]byte{0, 3, 0, 1}, ifi.HardwareAddr...))
	}

	iaid := uint32(1)
	if value := strings.TrimSpace(spec.IAID); value != "" {
		parsed, err := strconv.ParseUint(value, 0, 32)
		if err != nil {
			return routerstate.PDLease{}, routerstate.PDLease{}, err
		}
		iaid = uint32(parsed)
	}
	config := routerstate.PDLeaseConfig{Client: api.EffectiveIPv6PDClient("", spec.Profile, spec.Client), Profile: stringutil.FirstNonEmpty(spec.Profile, api.IPv6PDProfileDefault), UplinkIfname: ifname, PrefixLength: api.EffectiveIPv6PDPrefixLength(spec.Profile, spec.PrefixLength), ClientDUID: duid, IAID: iaid, ExpectedDUID: previous.ExpectedDUID}
	configured, _ := previous.Configured(config)

	return previous, configured, nil
}

func (c DaemonStatusController) observePDLease(ctx context.Context, name, socket string, observed daemonapi.ResourceStatus) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	previous, configured, err := c.configuredPDLease(name)
	if err != nil {
		return err
	}

	failedObservation := func(cause error) error {
		if ctx.Err() == nil && configured.Config() != previous.Config() {
			if commitErr := RecordPDLeaseObservation(c.Store, c.Bus, c.Router, name, previous.Config(), configured, nil, daemonapi.ResourceStatus{}); commitErr != nil {
				return commitErr
			}
		}
		return cause
	}
	snapshot, err := readDaemonPDLease(ctx, socket)
	if err != nil {
		return failedObservation(err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := routerstate.ValidatePDLeaseSnapshot(configured, name, snapshot); err != nil {
		return failedObservation(err)
	}
	return RecordPDLeaseObservation(c.Store, c.Bus, c.Router, name, previous.Config(), configured, &snapshot, observed)
}

// The daemon is a process boundary. JSON is decoded here once into the complete
// typed snapshot; no encoded option/status strings are passed between consumers.
func readDaemonPDLease(ctx context.Context, socket string) (pdclient.Snapshot, error) {
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socket)
	}}}
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
	if response.StatusCode != http.StatusOK {
		return pdclient.Snapshot{}, fmt.Errorf("PD lease endpoint HTTP %d", response.StatusCode)
	}
	return decodeDaemonPDLease(response.Body)
}

func decodeDaemonPDLease(body io.Reader) (pdclient.Snapshot, error) {
	var snapshot pdclient.Snapshot
	if err := json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&snapshot); err != nil {
		return pdclient.Snapshot{}, err
	}
	return snapshot, nil
}

func derivePreviousPDLeaseAddresses(lease routerstate.PDLease, currentPrefix string, spec api.IPv6DelegatedAddressSpec, now time.Time) ([]managedPreviousDelegatedAddress, error) {
	if spec.PrefixLength == 128 {
		return nil, nil
	}
	var result []managedPreviousDelegatedAddress
	for _, previous := range lease.ValidPreviousPrefixes(now) {
		if previous.Prefix == currentPrefix {
			continue
		}
		address, err := DeriveIPv6Address(previous.Prefix, spec.SubnetID, spec.AddressSuffix)
		if err != nil {
			return nil, err
		}
		address, err = delegatedAddressWithPrefixLength(address, spec.PrefixLength)
		if err != nil {
			return nil, err
		}
		result = append(result, managedPreviousDelegatedAddress{Address: address, ExpiresAt: previous.ExpiresAt})
	}
	return result, nil
}

type atomicPDObservationStore interface {
	bus.EventStore
	SavePDLeaseObservationAndEvent(string, routerstate.PDLeaseConfig, routerstate.PDLease, *pdclient.Snapshot, daemonapi.ResourceStatus, routerstate.StatusEventBuilder) (*daemonapi.DaemonEvent, error)
}

// The same status/event commit owner publishes the lease transition once.
// No generic daemon-status write follows this operation for the PD resource.
func RecordPDLeaseObservation(store Store, eventBus *bus.Bus, router *api.Router, name string, expected routerstate.PDLeaseConfig, lease routerstate.PDLease, snapshot *pdclient.Snapshot, observed daemonapi.ResourceStatus) error {
	if evented, ok := store.(eventedStore); ok {
		return evented.savePDLeaseObservation(name, expected, lease, snapshot, observed)
	}
	return (eventedStore{Store: store, Bus: eventBus, Router: router}).savePDLeaseObservation(name, expected, lease, snapshot, observed)
}

func (s eventedStore) savePDLeaseObservation(name string, expected routerstate.PDLeaseConfig, lease routerstate.PDLease, snapshot *pdclient.Snapshot, observed daemonapi.ResourceStatus) error {
	store, ok := s.Store.(atomicPDObservationStore)
	if !ok {
		return fmt.Errorf("typed PD observation requires an atomic state/event store")
	}
	recorded := s.Bus != nil && s.Bus.PersistsTo(store)
	if statusEventHistoryRequired(s.Router) && !recorded {
		return fmt.Errorf("EventRule PD status history requires status and bus to share a transactional event store")
	}
	build := statusTransitionEventBuilder(api.NetAPIVersion, "DHCPv6PrefixDelegation", name)
	var localEvent *daemonapi.DaemonEvent
	prepare := func(current, next map[string]any) (bool, *daemonapi.DaemonEvent) {
		write, event := build(current, next)
		if recorded {
			return write, event
		}
		localEvent = event
		return write, nil
	}
	event, err := store.SavePDLeaseObservationAndEvent(name, expected, lease, snapshot, observed, prepare)
	if err != nil {
		return err
	}
	if event != nil {
		return s.Bus.PublishRecorded(context.Background(), *event)
	}
	if localEvent != nil && s.Bus != nil {
		return s.Bus.Publish(context.Background(), *localEvent)
	}
	return nil
}

// A persisted worker snapshot is external input. Its captured transaction epoch
// and source identity are admitted by the same atomic owner as socket replies;
// LAN/native receipts never act as a second lease cache.
func readPDClientSnapshotFile(path string) (pdclient.Snapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return pdclient.Snapshot{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return pdclient.Snapshot{}, err
	}
	if len(data) > 1<<20 {
		return pdclient.Snapshot{}, fmt.Errorf("PD snapshot file exceeds input limit")
	}
	var snapshot pdclient.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return snapshot, fmt.Errorf("decode PD snapshot file: %w", err)
	}
	return snapshot, nil
}
