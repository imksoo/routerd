// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

type bridgeActivationStore struct {
	mapStore
	readErr        bool
	saveHealthyErr bool
	saveAbsentErr  bool
}

func (s *bridgeActivationStore) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	if s.readErr {
		return nil, errors.New("fixture read failure")
	}
	return s.mapStore.ReadObjectStatus(version, kind, name)
}
func (s *bridgeActivationStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if s.saveHealthyErr && status["phase"] == "Healthy" || s.saveAbsentErr && status["phase"] == "Absent" {
		return errors.New("fixture acknowledgement failure")
	}
	return s.mapStore.SaveObjectStatus(version, kind, name, status)
}

type bridgeKernelFixture struct {
	links       map[string]net.Interface
	alias       map[string]string
	settings    map[string]map[string]string
	master      map[string]string
	commands    []string
	nextIndex   int
	failCommand string
	failDelete  bool
	failRead    bool
	created     int
	deleted     []int
}

func bridgeNativeFixture(t *testing.T, store *bridgeActivationStore) (BridgeController, *bridgeKernelFixture) {
	t.Helper()
	kernel := &bridgeKernelFixture{links: map[string]net.Interface{"ens19": {Name: "ens19", Index: 5, MTU: 1500, Flags: net.FlagUp}, "ens20": {Name: "ens20", Index: 6, MTU: 1500, Flags: net.FlagUp}}, alias: map[string]string{}, settings: map[string]map[string]string{}, master: map[string]string{}, nextIndex: 12}
	controller := BridgeController{Router: bridgeVXLANTestRouter(false), Store: store, NetworkdDir: t.TempDir(), OperatingSystem: platform.OSLinux}
	controller.Interfaces = func() ([]net.Interface, error) {
		if kernel.failRead {
			return nil, errors.New("native snapshot failed")
		}
		var links []net.Interface
		for _, link := range kernel.links {
			links = append(links, link)
		}
		return links, nil
	}
	controller.ReadFile = func(path string) ([]byte, error) {
		name := strings.Split(strings.TrimPrefix(path, "/sys/class/net/"), "/")[0]
		if filepath.Base(path) == "ifalias" {
			return []byte(kernel.alias[name]), nil
		}
		if value, ok := kernel.settings[name][filepath.Base(path)]; ok {
			return []byte(value + "\n"), nil
		}
		return nil, os.ErrNotExist
	}
	controller.Readlink = func(path string) (string, error) {
		name := strings.Split(strings.TrimPrefix(path, "/sys/class/net/"), "/")[0]
		if master := kernel.master[name]; master != "" {
			return "../../" + master, nil
		}
		return "", os.ErrNotExist
	}
	controller.Command = func(_ context.Context, name string, args ...string) error {
		command := name + " " + strings.Join(args, " ")
		kernel.commands = append(kernel.commands, command)
		if kernel.failCommand != "" && command == kernel.failCommand {
			return errors.New("fixture activation failure")
		}
		if name == "ip" && len(args) > 4 && args[0] == "link" && args[1] == "add" {
			owners, err := controller.readBridgeOwners()
			pending := false
			for _, owner := range owners {
				if owner.Plan.IfName == args[3] && !owner.Activated && !owner.Withdrawing {
					pending = true
				}
			}
			if err != nil || !pending {
				t.Fatalf("native creation preceded pending receipt: %v %v", owners, err)
			}
			ifname := args[3]
			if _, exists := kernel.links[ifname]; exists {
				return errors.New("native name collision")
			}
			values := map[string]string{}
			for i := 4; i+1 < len(args); i += 2 {
				values[args[i]] = args[i+1]
			}
			mtu := 1500
			if value := values["mtu"]; value != "" {
				mtu, _ = strconv.Atoi(value)
			}
			kernel.links[ifname] = net.Interface{Name: ifname, Index: kernel.nextIndex, MTU: mtu}
			kernel.nextIndex++
			kernel.created++
			kernel.alias[ifname] = values["alias"]
			values["multicast_snooping"] = values["mcast_snooping"]
			kernel.settings[ifname] = values
		}
		// networkd cannot attach to an undeclared NetDev. KeepMaster only
		// preserves membership established by the native Bridge owner.

		return nil
	}
	controller.SetAdminByIndex = func(index int, proof string, up bool) error {
		for name, link := range kernel.links {
			if link.Index == index && kernel.alias[name] == proof {
				if up {
					link.Flags |= net.FlagUp
				} else {
					link.Flags &^= net.FlagUp
				}
				kernel.links[name] = link
				return nil
			}
		}
		return errors.New("native identity changed")
	}
	controller.SetMasterByIndex = func(port bridgePort, bridgeIndex int, proof string) error {
		bridge := ""
		for name, link := range kernel.links {
			if link.Index == bridgeIndex && kernel.alias[name] == proof {
				bridge = name
				break
			}
		}
		member, present := kernel.links[port.IfName]
		if bridge == "" || !present || member.Index != port.IfIndex {
			return errors.New("native port or bridge identity changed")
		}
		if current := kernel.master[port.IfName]; current != "" && current != bridge {
			return errors.New("foreign native master")
		}
		owners, err := controller.readBridgeOwners()
		captured := false
		for _, owner := range owners {
			if owner.proof() == proof {
				for _, receipt := range owner.Ports {
					if receipt == port {
						captured = true
					}
				}
			}
		}
		if err != nil || !captured {
			t.Fatalf("native attach preceded durable port receipt: %v %v", owners, err)
		}
		kernel.master[port.IfName] = bridge
		return nil
	}

	controller.DeleteByIndex = func(index int, proof string) error {
		if kernel.failDelete {
			return errors.New("fixture delete failure")
		}
		for name, link := range kernel.links {
			if link.Index == index && kernel.alias[name] == proof {
				delete(kernel.links, name)
				delete(kernel.alias, name)
				delete(kernel.settings, name)
				for member, master := range kernel.master {
					if master == name {
						delete(kernel.master, member)
					}
				}
				kernel.deleted = append(kernel.deleted, index)
				return nil
			}
		}
		return errors.New("native identity changed")
	}
	return controller, kernel
}

func TestBridgeActivationRetriesUnacknowledgedNativeEffects(t *testing.T) {
	for _, failure := range []string{"networkctl reload", "networkctl reconfigure ens19", "applied-status"} {
		t.Run(failure, func(t *testing.T) {
			store := &bridgeActivationStore{mapStore: mapStore{}}
			controller, kernel := bridgeNativeFixture(t, store)
			if failure == "applied-status" {
				store.saveHealthyErr = true
			} else {
				kernel.failCommand = failure
			}
			if err := controller.Reconcile(context.Background()); err == nil {
				t.Fatal("failed effect was acknowledged")
			}
			owners, err := controller.readBridgeOwners()
			if err != nil || len(owners) != 1 || owners[0].IfIndex != 12 {
				t.Fatalf("lost recovery receipt: %v %v", owners, err)
			}
			kernel.commands = nil
			kernel.failCommand = ""
			store.saveHealthyErr = false
			restarted := controller
			if err := restarted.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			if kernel.created != 1 {
				t.Fatalf("retry duplicated native creation: %d", kernel.created)
			}
			if failure != "applied-status" && strings.Join(kernel.commands, ";") != "networkctl reload;networkctl reconfigure ens19;networkctl reconfigure br-l2" {
				t.Fatalf("desired files hid missing activation: %v", kernel.commands)
			}
			kernel.commands = nil
			controller.DryRun = true
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			controller.DryRun = false
			if err := controller.Reconcile(context.Background()); err != nil || len(kernel.commands) != 0 {
				t.Fatalf("dry-run discarded live receipt: %v %v", err, kernel.commands)
			}
		})
	}
}

func TestBridgeActivationReadFailureAndForeignArtifactHaveNoEffects(t *testing.T) {
	store := &bridgeActivationStore{mapStore: mapStore{}, readErr: true}
	controller, kernel := bridgeNativeFixture(t, store)
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("read failure accepted")
	}
	if entries, _ := os.ReadDir(controller.NetworkdDir); len(entries) != 0 || len(kernel.commands) != 0 {
		t.Fatal("failed read caused effects")
	}
	store.readErr = false
	path := filepath.Join(controller.NetworkdDir, "30-routerd-br-l2.network")
	if err := os.WriteFile(path, []byte("# operator owned\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(context.Background()); err == nil || len(kernel.commands) != 0 {
		t.Fatalf("foreign artifact accepted: %v %v", err, kernel.commands)
	}
	if data, _ := os.ReadFile(path); string(data) != "# operator owned\n" {
		t.Fatalf("foreign artifact overwritten: %s", data)
	}
}

func TestBridgeRecreationAndRemovalRetainFailedCleanup(t *testing.T) {
	store := &bridgeActivationStore{mapStore: mapStore{}}
	controller, kernel := bridgeNativeFixture(t, store)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated := bridgeVXLANTestRouter(false)
	updated.Spec.Resources = append(updated.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "new-lan"}, Spec: api.InterfaceSpec{IfName: "ens20"}})
	for i, resource := range updated.Spec.Resources {
		if resource.Kind == "Bridge" {
			spec, _ := resource.BridgeSpec()
			spec.IfName = "br-new"
			spec.Members = []string{"new-lan"}
			updated.Spec.Resources[i].Spec = spec
		}
	}
	controller.Router = updated
	kernel.failDelete = true
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("failed deletion was acknowledged")
	}
	owners, err := controller.readBridgeOwners()
	if err != nil || len(owners) != 1 || !owners[0].Withdrawing || owners[0].Plan.IfName != "br-l2" || kernel.created != 1 {
		t.Fatalf("failed cleanup lost previous identity: %v %v", owners, err)
	}
	kernel.failDelete = false
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, old := kernel.links["br-l2"]; old || kernel.created != 2 || len(kernel.deleted) != 1 || kernel.deleted[0] != 12 {
		t.Fatalf("old/new ownership confused: %+v", kernel)
	}
	if _, err := os.Stat(filepath.Join(controller.NetworkdDir, "10-netplan-ens19.network.d", "88-routerd-bridge.conf")); !os.IsNotExist(err) {
		t.Fatalf("retired member artifact remains: %v", err)
	}
	controller.Router = &api.Router{}
	store.saveAbsentErr = true
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("failed withdrawal publication accepted")
	}
	if owners, err := controller.readBridgeOwners(); err != nil || len(owners) != 1 {
		t.Fatalf("failed final save dropped withdrawal receipt: %v %v", owners, err)
	}
	store.saveAbsentErr = false
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owners, err := controller.readBridgeOwners(); err != nil || len(owners) != 0 {
		t.Fatalf("receipt remains after proven withdrawal: %v %v", owners, err)
	}
	if _, exists := kernel.links["br-new"]; exists {
		t.Fatal("removed bridge remains")
	}
}

func TestBridgeWithdrawalPreservesForeignReplacement(t *testing.T) {
	store := &bridgeActivationStore{mapStore: mapStore{}}
	controller, kernel := bridgeNativeFixture(t, store)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	kernel.links["br-l2"] = net.Interface{Name: "br-l2", Index: 99, Flags: net.FlagUp}
	kernel.alias["br-l2"] = "operator"
	foreign := filepath.Join(controller.NetworkdDir, "30-routerd-br-l2.network")
	if err := os.WriteFile(foreign, []byte("# operator replacement\n"), 0600); err != nil {
		t.Fatal(err)
	}
	controller.Router = &api.Router{}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if kernel.links["br-l2"].Index != 99 || len(kernel.deleted) != 0 {
		t.Fatal("foreign native replacement mutated")
	}
	if data, _ := os.ReadFile(foreign); string(data) != "# operator replacement\n" {
		t.Fatalf("foreign artifact changed: %s", data)
	}
}

func TestBridgePendingCreateRecoveryRequiresPrivateNativeProof(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(strconv.FormatBool(foreign), func(t *testing.T) {
			store := &bridgeActivationStore{mapStore: mapStore{}}
			controller, kernel := bridgeNativeFixture(t, store)
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			owners, err := controller.readBridgeOwners()
			if err != nil {
				t.Fatal(err)
			}
			owner := owners[0]
			owner.IfIndex = 0
			owner.Activated = false
			if err := controller.writeBridgeOwner(owner); err != nil {
				t.Fatal(err)
			}
			if foreign {
				kernel.alias["br-l2"] = "operator"
			}
			kernel.commands = nil
			err = controller.Reconcile(context.Background())
			if foreign {
				if err == nil || len(kernel.commands) != 0 || kernel.links["br-l2"].Flags&net.FlagUp == 0 {
					t.Fatalf("pending receipt adopted a tokenless interface: %v %v", err, kernel.commands)
				}
				return
			}
			if err != nil || kernel.created != 1 {
				t.Fatalf("pending token recovery duplicated native creation: %v %d", err, kernel.created)
			}
		})
	}
}

func TestBridgeUnknownSnapshotAndForeignMasterDoNotGrantOwnership(t *testing.T) {
	store := &bridgeActivationStore{mapStore: mapStore{}}
	controller, kernel := bridgeNativeFixture(t, store)
	kernel.master["ens19"] = "operator-bridge"
	if err := controller.Reconcile(context.Background()); err == nil || kernel.created != 0 {
		t.Fatalf("foreign association was adopted: %v", err)
	}
	delete(kernel.master, "ens19")
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(controller.bridgeOwnerPath("legacy-l2"))
	if err != nil {
		t.Fatal(err)
	}
	kernel.failRead = true
	kernel.commands = nil
	controller.Router = &api.Router{}
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("failed native observation acknowledged withdrawal")
	}
	after, err := os.ReadFile(controller.bridgeOwnerPath("legacy-l2"))
	if err != nil || string(before) != string(after) || len(kernel.commands) != 0 {
		t.Fatalf("unknown observation lost receipt or caused effects: %v", err)
	}
}

func TestBridgeRemovalPreservesOtherOwnedBridge(t *testing.T) {
	store := &bridgeActivationStore{mapStore: mapStore{}}
	controller, kernel := bridgeNativeFixture(t, store)
	controller.Router.Spec.Resources = append(controller.Router.Spec.Resources,
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "second-lan"}, Spec: api.InterfaceSpec{IfName: "ens20"}},
		api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "secondary"}, Spec: api.BridgeSpec{IfName: "br-second", Members: []string{"second-lan"}}})
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := kernel.links["br-second"]
	retained := make([]api.Resource, 0, len(controller.Router.Spec.Resources))
	for _, resource := range controller.Router.Spec.Resources {
		if resource.Kind == "Bridge" && resource.Metadata.Name == "legacy-l2" {
			continue
		}
		retained = append(retained, resource)
	}
	controller.Router = &api.Router{Spec: api.RouterSpec{Resources: retained}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(kernel.deleted) != 1 || kernel.deleted[0] != 12 || kernel.links["br-second"].Index != second.Index || kernel.master["ens20"] != "br-second" {
		t.Fatalf("removal affected another native owner: %+v", kernel)
	}
	owners, err := controller.readBridgeOwners()
	if err != nil || len(owners) != 1 || owners[0].Resource != "secondary" {
		t.Fatalf("other receipt changed: %v %v", owners, err)
	}
}

func TestBridgeCancellationAfterNativePreparationDoesNotEnableForwarding(t *testing.T) {
	store := &bridgeActivationStore{mapStore: mapStore{}}
	controller, kernel := bridgeNativeFixture(t, store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	native := controller.Command
	controller.Command = func(ctx context.Context, name string, args ...string) error {
		err := native(ctx, name, args...)
		if name == "networkctl" && strings.Join(args, " ") == "reconfigure br-l2" {
			cancel()
		}
		return err
	}
	if err := controller.Reconcile(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled activation result: %v", err)
	}
	if kernel.master["ens19"] != "" || kernel.links["br-l2"].Flags&net.FlagUp != 0 {
		t.Fatal("cancelled preparation enabled native forwarding")
	}
	owners, err := controller.readBridgeOwners()
	if err != nil || len(owners) != 1 || owners[0].Activated {
		t.Fatalf("cancelled activation lost pending recovery: %v %v", owners, err)
	}
}
