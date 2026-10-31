// SPDX-License-Identifier: BSD-3-Clause

package ipsec

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/strongswan/govici/vici"
)

// This test-owned VICI endpoint acknowledges named native operations. It does
// not implement IKE, encryption, peer reachability, or kernel policy effects.
type viciEndpoint struct {
	mu              sync.Mutex
	connections     map[string][]byte
	keys            map[string][]byte
	active          map[string]bool
	saEvents        map[string][]byte
	calls           []string
	failLoad        bool
	lieUnload       bool
	omitACK         bool
	blockLoad       bool
	directory       string
	pendingObserved bool
	loseObservation bool
	observationLost bool
}

func newVICIEndpoint(t *testing.T, directory string) (*viciEndpoint, *vici.Session) {
	t.Helper()
	endpoint := &viciEndpoint{connections: map[string][]byte{"foreign": []byte("foreign config")}, keys: map[string][]byte{"foreign": []byte("foreign secret")}, active: map[string]bool{"foreign": true}, directory: directory}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var header [4]byte
			if _, err := io.ReadFull(conn, header[:]); err != nil {
				return
			}
			size := binary.BigEndian.Uint32(header[:])
			if size > 512<<10 || size < 2 {
				return
			}
			packet := make([]byte, size)
			if _, err := io.ReadFull(conn, packet); err != nil {
				return
			}
			kind, n := packet[0], int(packet[1])
			if n+2 > len(packet) {
				return
			}
			command, body := string(packet[2:2+n]), packet[2+n:]
			if kind == 3 || kind == 4 {
				if writeVICIPacket(conn, []byte{5}) != nil {
					return
				}
				continue
			}
			if kind != 0 {
				return
			}
			endpoint.mu.Lock()
			endpoint.calls = append(endpoint.calls, command)
			block := endpoint.blockLoad && command == "load-conn"
			response, event := endpoint.reply(command, body)
			endpoint.mu.Unlock()
			if block {
				_, _ = io.Copy(io.Discard, conn)
				return
			}
			if event != nil && writeVICIPacket(conn, append([]byte{7, 7}, append([]byte("list-sa"), event...)...)) != nil {
				return
			}
			if writeVICIPacket(conn, append([]byte{1}, response...)) != nil {
				return
			}
		}
	}()
	session, err := vici.NewSession(vici.WithAddr("tcp", listener.Addr().String()), vici.WithDialContext(func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &boundedVICIConnection{Conn: conn}, nil
	}))
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close(); _ = listener.Close(); <-done })
	return endpoint, session
}

func (e *viciEndpoint) reply(command string, body []byte) ([]byte, []byte) {
	ack := viciValue("success", "yes")
	if e.omitACK && command == "load-conn" {
		return nil, nil
	}
	switch command {
	case "get-conns":
		if e.observationLost {
			return append(viciValue("success", "no"), viciValue("errmsg", "observation unavailable")...), nil
		}
		names := make([]string, 0, len(e.connections))
		for name := range e.connections {
			names = append(names, name)
		}
		slices.Sort(names)
		return viciList("conns", names), nil
	case "get-shared":
		names := make([]string, 0, len(e.keys))
		for name := range e.keys {
			names = append(names, name)
		}
		slices.Sort(names)
		return viciList("keys", names), nil
	case "load-shared":
		e.recordPending()
		e.keys[viciRequestValue(body, "id")] = bytes.Clone(body)
	case "load-key", "load-cert":
		e.recordPending()
	case "load-conn":
		e.recordPending()
		if e.failLoad {
			return append(viciValue("success", "no"), viciValue("errmsg", "load rejected")...), nil
		}
		if len(body) < 2 || body[0] != 1 || int(body[1])+2 > len(body) {
			return viciValue("success", "no"), nil
		}
		name := string(body[2 : 2+int(body[1])])
		e.connections[name] = bytes.Clone(body)
		if e.loseObservation {
			e.observationLost = true
		}
	case "unload-conn":
		e.recordPending()
		if !e.lieUnload {
			delete(e.connections, viciRequestValue(body, "name"))
		}
	case "unload-shared":
		e.recordPending()
		delete(e.keys, viciRequestValue(body, "id"))
	case "list-sas":
		name := viciRequestValue(body, "ike")
		if event := e.saEvents[name]; event != nil {
			return nil, event
		}
		if e.active[name] {
			return nil, append(append([]byte{1, byte(len(name))}, []byte(name)...), 2)
		}
	case "terminate":
		e.recordPending()
		delete(e.active, viciRequestValue(body, "ike"))
		delete(e.saEvents, viciRequestValue(body, "ike"))
	default:
		return append(viciValue("success", "no"), viciValue("errmsg", "unexpected command "+command)...), nil
	}
	return ack, nil
}

func (e *viciEndpoint) recordPending() {
	state, err := readReceipt(e.directory)
	if err == nil && state.Pending {
		e.pendingObserved = true
	}
}

func writeVICIPacket(writer io.Writer, packet []byte) error {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(packet)))
	_, err := writer.Write(append(header[:], packet...))
	return err
}
func viciValue(key, value string) []byte {
	output := append([]byte{3, byte(len(key))}, []byte(key)...)
	output = append(output, byte(len(value)>>8), byte(len(value)))
	return append(output, []byte(value)...)
}
func viciList(key string, values []string) []byte {
	output := append([]byte{4, byte(len(key))}, []byte(key)...)
	for _, value := range values {
		output = append(output, 5, byte(len(value)>>8), byte(len(value)))
		output = append(output, []byte(value)...)
	}
	return append(output, 6)
}
func viciRequestValue(body []byte, key string) string {
	prefix := append([]byte{3, byte(len(key))}, []byte(key)...)
	index := bytes.Index(body, prefix)
	if index < 0 || index+len(prefix)+2 > len(body) {
		return ""
	}
	data := body[index+len(prefix):]
	n := int(binary.BigEndian.Uint16(data[:2]))
	if n > len(data)-2 {
		return ""
	}
	return string(data[2 : 2+n])
}
func ipsecTestRouter(remote, secret string) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"}, Metadata: api.ObjectMeta{Name: "site"}, Spec: api.IPsecConnectionSpec{LocalAddress: "198.51.100.1", RemoteAddress: remote, LeftSubnet: "10.1.0.0/24", RightSubnet: "10.2.0.0/24", PreSharedKey: secret}}}}}
}

func TestScopedVICIReconcileFailureRecoveryFreshDaemonAndLastWithdrawal(t *testing.T) {
	directory := t.TempDir()
	endpoint, session := newVICIEndpoint(t, directory)
	controller := Controller{Session: session, Directory: directory, nativeSAs: &recordedNativeSAs{}}
	router := ipsecTestRouter("203.0.113.1", "first-secret")
	endpoint.mu.Lock()
	endpoint.failLoad = true
	endpoint.mu.Unlock()
	if err := controller.Reconcile(t.Context(), router); err == nil || !strings.Contains(err.Error(), "load rejected") {
		t.Fatalf("failed load = %v", err)
	}
	state, err := readReceipt(directory)
	if err != nil || !state.Pending || !ownsConnection(state, "routerd-site") {
		t.Fatalf("failed native operation lost pending ownership: %+v %v", state, err)
	}
	endpoint.mu.Lock()
	endpoint.failLoad = false
	endpoint.mu.Unlock()
	if err := controller.Reconcile(t.Context(), router); err != nil {
		t.Fatal(err)
	}
	state, err = readReceipt(directory)
	if err != nil || state.Pending || state.Connections[0].Hash == "" {
		t.Fatalf("successful receipt = %+v %v", state, err)
	}
	endpoint.mu.Lock()
	endpoint.active["routerd-site"] = true
	endpoint.mu.Unlock()
	router = ipsecTestRouter("203.0.113.2", "replacement-secret")
	if err := controller.Reconcile(t.Context(), router); err != nil {
		t.Fatal(err)
	}
	endpoint.mu.Lock()
	if endpoint.active["routerd-site"] || !bytes.Contains(endpoint.connections["routerd-site"], []byte("203.0.113.2")) || !bytes.Contains(endpoint.keys["routerd-site"], []byte("replacement-secret")) {
		t.Error("configuration-only update did not replace native config/secret and terminate old SA")
	}
	// A charon restart loses native state while the durable receipt is unchanged.
	delete(endpoint.connections, "routerd-site")
	delete(endpoint.keys, "routerd-site")
	endpoint.mu.Unlock()
	if err := controller.Reconcile(t.Context(), router); err != nil {
		t.Fatal(err)
	}
	endpoint.mu.Lock()
	endpoint.active["routerd-site"] = true
	endpoint.lieUnload = true
	endpoint.mu.Unlock()
	empty := &api.Router{}
	if err := controller.Reconcile(t.Context(), empty); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("unverified unload was reported applied: %v", err)
	}
	state, err = readReceipt(directory)
	if err != nil || !state.Pending {
		t.Fatalf("failed withdrawal lost pending receipt: %+v %v", state, err)
	}
	endpoint.mu.Lock()
	endpoint.lieUnload = false
	endpoint.mu.Unlock()
	if err := controller.Reconcile(t.Context(), empty); err != nil {
		t.Fatal(err)
	}
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	if _, exists := endpoint.connections["routerd-site"]; exists {
		t.Error("owned native connection remains")
	}
	if _, exists := endpoint.keys["routerd-site"]; exists {
		t.Error("owned native secret remains")
	}
	if string(endpoint.connections["foreign"]) != "foreign config" || string(endpoint.keys["foreign"]) != "foreign secret" || !endpoint.active["foreign"] {
		t.Error("foreign connection/credential/SA changed")
	}
	if !endpoint.pendingObserved {
		t.Error("effect occurred without durable pending receipt")
	}
	if needed, err := Needed(empty, directory); err != nil || needed {
		t.Fatalf("completed withdrawal still requires charon: %v %v", needed, err)
	}
}

func TestScopedVICIRefusesUnownedNameAndSharedKeyCollisions(t *testing.T) {
	for _, collision := range []string{"connection", "shared key"} {
		t.Run(collision, func(t *testing.T) {
			directory := t.TempDir()
			endpoint, session := newVICIEndpoint(t, directory)
			endpoint.mu.Lock()
			if collision == "connection" {
				endpoint.connections["routerd-site"] = []byte("unowned")
			} else {
				endpoint.keys["routerd-site"] = []byte("unowned")
			}
			endpoint.mu.Unlock()
			err := (Controller{Session: session, Directory: directory, nativeSAs: &recordedNativeSAs{}}).Reconcile(t.Context(), ipsecTestRouter("203.0.113.1", "secret"))
			if err == nil || !strings.Contains(err.Error(), "unowned IPsec "+collision) {
				t.Fatalf("collision = %v", err)
			}
			if _, err := os.Stat(filepath.Join(directory, ReceiptName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("collision acquired ownership: %v", err)
			}
			endpoint.mu.Lock()
			defer endpoint.mu.Unlock()
			for _, command := range endpoint.calls {
				if command != "get-conns" && command != "get-shared" {
					t.Errorf("unowned mutation %s", command)
				}
			}
		})
	}
}

func TestScopedVICIRequiresPositiveACKAndPropagatesCancellation(t *testing.T) {
	for _, mode := range []string{"missing ACK", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			endpoint, session := newVICIEndpoint(t, directory)
			endpoint.mu.Lock()
			endpoint.omitACK = mode == "missing ACK"
			endpoint.blockLoad = mode == "cancel"
			endpoint.mu.Unlock()
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			err := (Controller{Session: session, Directory: directory, nativeSAs: &recordedNativeSAs{}}).Reconcile(ctx, ipsecTestRouter("203.0.113.1", "secret"))
			if mode == "missing ACK" && (err == nil || !strings.Contains(err.Error(), "did not acknowledge")) {
				t.Fatalf("missing ACK=%v", err)
			}
			if mode == "cancel" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("cancel=%v", err)
			}
			state, readErr := readReceipt(directory)
			if readErr != nil || !state.Pending {
				t.Fatalf("uncertain operation lost receipt: %+v %v", state, readErr)
			}
		})
	}
}

func TestBoundedVICIRejectsOversizedPeerHeaderBeforeDecoder(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	go func() {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], 1<<30)
		_, _ = right.Write(header[:])
	}()
	connection := &boundedVICIConnection{Conn: left}
	var header [4]byte
	if _, err := io.ReadFull(connection, header[:]); err == nil || !strings.Contains(err.Error(), "invalid VICI packet length") {
		t.Fatalf("oversized header=%v", err)
	}
}

func TestScopedVICICorruptReceiptHasNoNativeEffects(t *testing.T) {
	directory := t.TempDir()
	endpoint, session := newVICIEndpoint(t, directory)
	if err := os.WriteFile(filepath.Join(directory, ReceiptName), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := (Controller{Session: session, Directory: directory, nativeSAs: &recordedNativeSAs{}}).Reconcile(t.Context(), ipsecTestRouter("203.0.113.1", "secret")); err == nil {
		t.Fatal("accepted corrupt ownership")
	}
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	if len(endpoint.calls) != 0 {
		t.Fatal(fmt.Sprint(endpoint.calls))
	}
}

func TestScopedVICICertificateUsesBlobAndNeverUnloadsSharedNativeKeys(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "x509"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "private"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "x509ca"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "x509ca", "issuer.pem"), []byte("test-owned CA certificate bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	certificate := []byte("test-owned certificate bytes")
	key := []byte("test-owned private key bytes")
	if err := os.WriteFile(filepath.Join(directory, "x509", "site.pem"), certificate, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "private", "site.pem"), key, 0600); err != nil {
		t.Fatal(err)
	}
	endpoint, session := newVICIEndpoint(t, directory)
	router := ipsecTestRouter("203.0.113.1", "")
	spec, _ := router.Spec.Resources[0].IPsecConnectionSpec()
	spec.CertificateRef = "site.pem"
	router.Spec.Resources[0].Spec = spec
	controller := Controller{Session: session, Directory: directory, nativeSAs: &recordedNativeSAs{}}
	if err := controller.Reconcile(t.Context(), router); err != nil {
		t.Fatal(err)
	}
	endpoint.mu.Lock()
	if !bytes.Contains(endpoint.connections["routerd-site"], certificate) || !bytes.Contains(endpoint.connections["routerd-site"], []byte("pubkey")) {
		t.Error("certificateRef was not supplied as an auth certificate blob")
	}
	if !slices.Contains(endpoint.calls, "load-cert") {
		t.Error("owned trust certificate was not offered to charon")
	}
	if !slices.Contains(endpoint.calls, "load-key") {
		t.Error("configured private key was never offered to charon")
	}
	endpoint.mu.Unlock()
	if err := controller.Reconcile(t.Context(), &api.Router{}); err != nil {
		t.Fatal(err)
	}
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	for _, command := range endpoint.calls {
		if command == "clear-creds" || command == "unload-key" {
			t.Errorf("shared credential affected by %s", command)
		}
	}
}

func TestScopedVICIInterruptedReplacementCannotTrustOldHashOnRestore(t *testing.T) {
	directory := t.TempDir()
	endpoint, session := newVICIEndpoint(t, directory)
	controller := Controller{Session: session, Directory: directory, nativeSAs: &recordedNativeSAs{}}
	original := ipsecTestRouter("203.0.113.1", "original")
	if err := controller.Reconcile(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	endpoint.mu.Lock()
	endpoint.loseObservation = true
	endpoint.mu.Unlock()
	err := controller.Reconcile(t.Context(), ipsecTestRouter("203.0.113.2", "replacement"))
	if err == nil || !strings.Contains(err.Error(), "observation unavailable") {
		t.Fatalf("native replacement lost final observation: %v", err)
	}
	endpoint.mu.Lock()
	endpoint.loseObservation = false
	endpoint.observationLost = false
	endpoint.active["routerd-site"] = true
	endpoint.mu.Unlock()
	// The last completed receipt still has the original hash, but native state
	// may already contain the replacement. Restoring must terminate those SAs.
	if err := controller.Reconcile(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	if endpoint.active["routerd-site"] || !bytes.Contains(endpoint.connections["routerd-site"], []byte("203.0.113.1")) {
		t.Error("restore trusted stale receipt hash and retained replacement SA")
	}
}

func TestScopedVICIRejectsUnimportedContainerBeforeNativeMutation(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"x509", "pkcs12"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "x509", "site.pem"), []byte("test-owned certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "pkcs12", "bundle.p12"), []byte("container"), 0600); err != nil {
		t.Fatal(err)
	}
	endpoint, session := newVICIEndpoint(t, directory)
	router := ipsecTestRouter("203.0.113.1", "")
	spec, _ := router.Spec.Resources[0].IPsecConnectionSpec()
	spec.CertificateRef = "site.pem"
	router.Spec.Resources[0].Spec = spec
	err := (Controller{Session: session, Directory: directory, nativeSAs: &recordedNativeSAs{}}).Reconcile(t.Context(), router)
	if err == nil || !strings.Contains(err.Error(), "PKCS#12") {
		t.Fatalf("unimported key container implied successful authentication configuration: %v", err)
	}
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	if len(endpoint.calls) != 0 {
		t.Fatalf("credential preflight failure touched charon: %v", endpoint.calls)
	}
}
