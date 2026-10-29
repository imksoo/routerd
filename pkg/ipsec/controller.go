// SPDX-License-Identifier: BSD-3-Clause

package ipsec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/strongswan/govici/vici"

	"github.com/imksoo/routerd/pkg/api"
)

const ReceiptName = "routerd-ipsec.json"

type connectionReceipt struct {
	Name string `json:"name"`
	Hash string `json:"hash,omitempty"`
}

// A pending receipt is written before sending any mutation. It retains every
// possibly loaded name until a successful native withdrawal or replacement.
type receipt struct {
	Version     int                 `json:"version"`
	Pending     bool                `json:"pending"`
	Connections []connectionReceipt `json:"connections"`
	SharedKeys  []string            `json:"sharedKeys"`
}

type nativeCredential struct {
	command  string
	typeName string
	flag     string
	data     []byte
}

type desiredConnection struct {
	connectionReceipt
	spec        api.IPsecConnectionSpec
	certificate []byte
}

// Controller changes only explicitly journaled routerd connection names and
// shared-key IDs. Public certificates and content-addressed private keys may be
// shared by other users of charon, so they are never globally cleared/unloaded.
type Controller struct {
	Session   *vici.Session
	Directory string
}

// Needed avoids requiring charon on routers which have never owned IPsec.
func Needed(router *api.Router, directory string) (bool, error) {
	if router != nil {
		for _, res := range router.Spec.Resources {
			if res.Kind == "IPsecConnection" {
				return true, nil
			}
		}
	}
	previous, err := readReceipt(directory)
	return len(previous.Connections) != 0 || len(previous.SharedKeys) != 0 || previous.Pending, err
}

func (c Controller) Reconcile(ctx context.Context, router *api.Router) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Session == nil || c.Directory == "" || router == nil {
		return errors.New("IPsec session, directory and router are required")
	}
	if err := os.MkdirAll(c.Directory, 0700); err != nil {
		return err
	}
	guard, err := os.OpenFile(filepath.Join(c.Directory, ".routerd-ipsec.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer guard.Close()
	if err := syscall.Flock(int(guard.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another IPsec reconciliation owns the receipt: %w", err)
	}
	defer syscall.Flock(int(guard.Fd()), syscall.LOCK_UN)
	previous, err := readReceipt(c.Directory)
	if err != nil {
		return err
	}
	if previous.Pending {
		// An interrupted request may have loaded a different configuration before
		// losing its final acknowledgement. Its old content hash proves nothing
		// about the SAs which may have started in that interval.
		for i := range previous.Connections {
			previous.Connections[i].Hash = ""
		}
	}
	desired, credentials, err := prepareConnections(router, c.Directory)
	if err != nil {
		return err
	}
	loaded, err := nativeNames(ctx, c.Session, "get-conns", "conns")
	if err != nil {
		return err
	}
	shared, err := nativeNames(ctx, c.Session, "get-shared", "keys")
	if err != nil {
		return err
	}
	pending := receipt{Version: 1, Pending: true, Connections: slices.Clone(previous.Connections), SharedKeys: slices.Clone(previous.SharedKeys)}
	for _, conn := range desired {
		if !ownsConnection(previous, conn.Name) && slices.Contains(loaded, conn.Name) {
			return fmt.Errorf("refuse to replace unowned IPsec connection %s", conn.Name)
		}
		if !ownsConnection(pending, conn.Name) {
			pending.Connections = append(pending.Connections, connectionReceipt{Name: conn.Name})
		}
		if conn.spec.PreSharedKey != "" {
			if !slices.Contains(previous.SharedKeys, conn.Name) && slices.Contains(shared, conn.Name) {
				return fmt.Errorf("refuse to replace unowned IPsec shared key %s", conn.Name)
			}
			if !slices.Contains(pending.SharedKeys, conn.Name) {
				pending.SharedKeys = append(pending.SharedKeys, conn.Name)
			}
		}
	}
	if err := writeReceipt(c.Directory, pending); err != nil {
		return err
	}
	for _, old := range pending.Connections {
		current := desiredByName(desired, old.Name)
		if current != nil && old.Hash == current.Hash {
			continue
		}
		if !ownsConnection(previous, old.Name) {
			continue // no authority to terminate SAs before our first mutation
		}
		// Stop admission under the old configuration before terminating its SAs.
		if slices.Contains(loaded, old.Name) {
			request, err := vici.MarshalMessage(struct {
				Name string `vici:"name"`
			}{old.Name})
			if err != nil {
				return err
			}
			if err := nativeAck(ctx, c.Session, "unload-conn", request); err != nil {
				return err
			}
		}
		if err := terminateConnection(ctx, c.Session, old.Name); err != nil {
			return err
		}
	}
	for _, credential := range credentials {
		request, err := vici.MarshalMessage(struct {
			Type string `vici:"type"`
			Flag string `vici:"flag"`
			Data string `vici:"data"`
		}{credential.typeName, credential.flag, string(credential.data)})
		if err != nil {
			return err
		}
		if err := nativeAck(ctx, c.Session, credential.command, request); err != nil {
			return err
		}
	}
	final := receipt{Version: 1, Connections: []connectionReceipt{}, SharedKeys: []string{}}
	for _, conn := range desired {
		if conn.spec.PreSharedKey != "" {
			request, err := vici.MarshalMessage(struct {
				ID     string   `vici:"id"`
				Type   string   `vici:"type"`
				Data   string   `vici:"data"`
				Owners []string `vici:"owners"`
			}{conn.Name, "IKE", conn.spec.PreSharedKey, []string{conn.spec.LocalAddress, conn.spec.RemoteAddress}})
			if err != nil {
				return err
			}
			if err := nativeAck(ctx, c.Session, "load-shared", request); err != nil {
				return err
			}
			final.SharedKeys = append(final.SharedKeys, conn.Name)
		}
		message, err := connectionMessage(conn)
		if err != nil {
			return err
		}
		response, err := c.Session.Call(ctx, "load-conn", message)
		if err := checkAck("load-conn", response, err); err != nil {
			return err
		}
		final.Connections = append(final.Connections, conn.connectionReceipt)
	}
	for _, key := range pending.SharedKeys {
		if !slices.Contains(final.SharedKeys, key) && slices.Contains(shared, key) {
			request, err := vici.MarshalMessage(struct {
				ID string `vici:"id"`
			}{key})
			if err != nil {
				return err
			}
			if err := nativeAck(ctx, c.Session, "unload-shared", request); err != nil {
				return err
			}
		}
	}
	loaded, err = nativeNames(ctx, c.Session, "get-conns", "conns")
	if err != nil {
		return err
	}
	shared, err = nativeNames(ctx, c.Session, "get-shared", "keys")
	if err != nil {
		return err
	}
	for _, conn := range pending.Connections {
		if slices.Contains(loaded, conn.Name) != ownsConnection(final, conn.Name) {
			return fmt.Errorf("IPsec connection %s does not match acknowledged desired state", conn.Name)
		}
	}
	for _, key := range pending.SharedKeys {
		if slices.Contains(shared, key) != slices.Contains(final.SharedKeys, key) {
			return fmt.Errorf("IPsec shared key %s does not match acknowledged desired state", key)
		}
	}
	return writeReceipt(c.Directory, final)
}

func ownsConnection(state receipt, name string) bool {
	return slices.ContainsFunc(state.Connections, func(conn connectionReceipt) bool { return conn.Name == name })
}

func desiredByName(connections []desiredConnection, name string) *desiredConnection {
	for i := range connections {
		if connections[i].Name == name {
			return &connections[i]
		}
	}
	return nil
}

func prepareConnections(router *api.Router, directory string) ([]desiredConnection, []nativeCredential, error) {
	var desired []desiredConnection
	needsPrivateKeys := false
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPsecConnection" {
			continue
		}
		spec, err := res.IPsecConnectionSpec()
		if err != nil {
			return nil, nil, err
		}
		conn := desiredConnection{connectionReceipt: connectionReceipt{Name: "routerd-" + res.Metadata.Name}, spec: spec}
		if res.Metadata.Name == "" || filepath.Base(res.Metadata.Name) != res.Metadata.Name || desiredByName(desired, conn.Name) != nil {
			return nil, nil, fmt.Errorf("invalid or duplicate IPsec connection name %q", res.Metadata.Name)
		}
		data, err := RenderSwanctl(conn.Name, spec)
		if err != nil {
			return nil, nil, err
		}
		hash := sha256.New()
		_, _ = hash.Write(data)
		if spec.CertificateRef != "" {
			path := spec.CertificateRef
			if !filepath.IsAbs(path) {
				path = filepath.Join(directory, "x509", path)
			}
			conn.certificate, err = readCredential(path)
			if err != nil {
				return nil, nil, err
			}
			_, _ = hash.Write(conn.certificate)
			needsPrivateKeys = true
		}
		conn.Hash = hex.EncodeToString(hash.Sum(nil))
		desired = append(desired, conn)
	}
	var credentials []nativeCredential
	if needsPrivateKeys {
		for _, source := range []struct{ directory, command, typeName, flag string }{
			{"private", "load-key", "any", ""}, {"rsa", "load-key", "rsa", ""},
			{"ecdsa", "load-key", "ecdsa", ""}, {"pkcs8", "load-key", "any", ""},
			{"x509", "load-cert", "X509", "NONE"}, {"x509ca", "load-cert", "X509", "CA"},
			{"x509ocsp", "load-cert", "X509", "OCSP"}, {"x509aa", "load-cert", "X509", "AA"},
			{"x509ac", "load-cert", "X509_AC", "NONE"}, {"x509crl", "load-cert", "X509_CRL", "NONE"},
		} {
			entries, err := os.ReadDir(filepath.Join(directory, source.directory))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, nil, err
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				data, err := readCredential(filepath.Join(directory, source.directory, entry.Name()))
				if err != nil {
					return nil, nil, err
				}
				credentials = append(credentials, nativeCredential{source.command, source.typeName, source.flag, data})
			}
		}
		entries, err := os.ReadDir(filepath.Join(directory, "pkcs12"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				return nil, nil, errors.New("automatic PKCS#12 loading is unsupported; provision PEM/DER certificate and private key files for scoped VICI loading")
			}
		}
	}
	return desired, credentials, nil
}

func readCredential(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65536))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > 65535 {
		return nil, fmt.Errorf("IPsec credential %s is empty or exceeds the VICI field limit", path)
	}
	return data, nil
}

func readReceipt(directory string) (receipt, error) {
	data, err := os.ReadFile(filepath.Join(directory, ReceiptName))
	if errors.Is(err, os.ErrNotExist) {
		return receipt{Version: 1}, nil
	}
	if err != nil {
		return receipt{}, err
	}
	var state receipt
	if err := json.Unmarshal(data, &state); err != nil {
		return receipt{}, fmt.Errorf("read IPsec receipt: %w", err)
	}
	if state.Version != 1 {
		return receipt{}, errors.New("unsupported IPsec receipt format; use the current routerd format")
	}
	for _, conn := range state.Connections {
		if !validOwnedName(conn.Name) {
			return receipt{}, fmt.Errorf("invalid owned IPsec connection %q", conn.Name)
		}
	}
	for _, key := range state.SharedKeys {
		if !validOwnedName(key) {
			return receipt{}, fmt.Errorf("invalid owned IPsec shared key %q", key)
		}
	}
	return state, nil
}

func validOwnedName(name string) bool {
	return strings.HasPrefix(name, "routerd-") && len(name) > len("routerd-") && filepath.Base(name) == name
}

func writeReceipt(directory string, state receipt) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".routerd-ipsec-*")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(path, filepath.Join(directory, ReceiptName)); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func nativeNames(ctx context.Context, session *vici.Session, command, field string) ([]string, error) {
	response, err := session.Call(ctx, command, nil)
	if err != nil {
		return nil, fmt.Errorf("IPsec %s: %w", command, err)
	}
	names, ok := response.Get(field).([]string)
	if !ok {
		return nil, fmt.Errorf("IPsec %s omitted %s observation", command, field)
	}
	return names, nil
}

func nativeAck(ctx context.Context, session *vici.Session, command string, message *vici.Message) error {
	response, err := session.Call(ctx, command, message)
	return checkAck(command, response, err)
}

func checkAck(command string, response *vici.Message, err error) error {
	if err != nil {
		return fmt.Errorf("IPsec %s: %w", command, err)
	}
	if response == nil || response.Get("success") != "yes" {
		return fmt.Errorf("IPsec %s did not acknowledge success", command)
	}
	return nil
}

func terminateConnection(ctx context.Context, session *vici.Session, name string) error {
	active, err := connectionHasSAs(ctx, session, name)
	if err != nil || !active {
		return err
	}
	request, err := vici.MarshalMessage(struct {
		IKE     string `vici:"ike"`
		Force   bool   `vici:"force"`
		Timeout int    `vici:"timeout"`
	}{name, true, 5000})
	if err != nil {
		return err
	}
	if err := nativeAck(ctx, session, "terminate", request); err != nil {
		return err
	}
	active, err = connectionHasSAs(ctx, session, name)
	if err != nil {
		return err
	}
	if active {
		return fmt.Errorf("IPsec connection %s still has active SAs after termination", name)
	}
	return nil
}

func connectionHasSAs(ctx context.Context, session *vici.Session, name string) (bool, error) {
	request, err := vici.MarshalMessage(struct {
		IKE string `vici:"ike"`
	}{name})
	if err != nil {
		return false, err
	}
	found := false
	for message, err := range session.CallStreaming(ctx, "list-sas", "list-sa", request) {
		if err != nil {
			return false, fmt.Errorf("IPsec list-sas: %w", err)
		}
		if len(message.Keys()) != 0 {
			found = true
		}
	}
	return found, nil
}

func connectionMessage(conn desiredConnection) (*vici.Message, error) {
	type authentication struct {
		Auth  string   `vici:"auth"`
		Certs []string `vici:"certs"`
	}
	type child struct {
		Local  []string `vici:"local_ts"`
		Remote []string `vici:"remote_ts"`
		ESP    []string `vici:"esp_proposals"`
		Start  string   `vici:"start_action"`
	}
	type connection struct {
		Local      []string         `vici:"local_addrs"`
		Remote     []string         `vici:"remote_addrs"`
		Proposals  []string         `vici:"proposals"`
		LocalAuth  authentication   `vici:"local"`
		RemoteAuth authentication   `vici:"remote"`
		Children   map[string]child `vici:"children"`
	}
	spec := conn.spec
	local := authentication{Auth: authMode(spec)}
	if len(conn.certificate) != 0 {
		local.Certs = []string{string(conn.certificate)}
	}
	// The only maps here describe VICI's named sections at its wire boundary.
	return vici.MarshalMessage(map[string]connection{conn.Name: {
		Local: []string{spec.LocalAddress}, Remote: []string{spec.RemoteAddress}, Proposals: defaultList(spec.Phase1Proposals, "aes256-sha256-modp2048"),
		LocalAuth: local, RemoteAuth: authentication{Auth: authMode(spec)},
		Children: map[string]child{"net": {Local: []string{spec.LeftSubnet}, Remote: []string{spec.RightSubnet}, ESP: defaultList(spec.Phase2Proposals, "aes256-sha256"), Start: "trap"}},
	}})
}
