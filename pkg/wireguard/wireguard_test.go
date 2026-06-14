// SPDX-License-Identifier: BSD-3-Clause

package wireguard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderSetConf(t *testing.T) {
	data, err := RenderSetConf(InterfaceConfig{
		Name:       "wg0",
		PrivateKey: "priv",
		ListenPort: 51820,
		FwMark:     100,
		Peers: []PeerConfig{{
			Name:                "peer-a",
			PublicKey:           "pub",
			PresharedKey:        "psk",
			AllowedIPs:          []string{"10.10.0.2/32", "fd00::2/128"},
			Endpoint:            "198.51.100.10:51820",
			PersistentKeepalive: 25,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"[Interface]",
		"PrivateKey = priv",
		"ListenPort = 51820",
		"FwMark = 100",
		"[Peer]",
		"PublicKey = pub",
		"AllowedIPs = 10.10.0.2/32, fd00::2/128",
		"Endpoint = 198.51.100.10:51820",
		"PersistentKeepalive = 25",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, got)
		}
	}
}

func TestApplyLinuxSetconfUsesStdin(t *testing.T) {
	conf := []byte("[Interface]\nPrivateKey = priv\n")
	var calls []string
	var gotStdin []byte
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "ip" && strings.Join(args, " ") == "link show wg0" {
			return nil, os.ErrNotExist
		}
		return nil, nil
	}
	runStdin := func(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		gotStdin = append([]byte(nil), stdin...)
		return nil, nil
	}
	if _, err := applyLinux(context.Background(), run, runStdin, InterfaceConfig{Name: "wg0"}, conf); err != nil {
		t.Fatal(err)
	}
	if string(gotStdin) != string(conf) {
		t.Fatalf("stdin = %q, want %q", gotStdin, conf)
	}
	assertWireGuardCall(t, calls, "wg setconf wg0 /dev/stdin")
	for _, call := range calls {
		if strings.Contains(call, "routerd-wg-") || strings.HasPrefix(call, "wg setconf wg0 /tmp/") {
			t.Fatalf("setconf used temp path in calls %#v", calls)
		}
	}
}

func TestApplyFreeBSDSetconfUsesStdin(t *testing.T) {
	conf := []byte("[Interface]\nPrivateKey = priv\n")
	var calls []string
	var gotStdin []byte
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	runStdin := func(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		gotStdin = append([]byte(nil), stdin...)
		return nil, nil
	}
	if _, err := applyFreeBSD(context.Background(), run, runStdin, InterfaceConfig{Name: "wg0"}, conf); err != nil {
		t.Fatal(err)
	}
	if string(gotStdin) != string(conf) {
		t.Fatalf("stdin = %q, want %q", gotStdin, conf)
	}
	assertWireGuardCall(t, calls, "wg setconf wg0 /dev/stdin")
	for _, call := range calls {
		if strings.Contains(call, "routerd-wg-") || strings.HasPrefix(call, "wg setconf wg0 /tmp/") {
			t.Fatalf("setconf used temp path in calls %#v", calls)
		}
	}
}

func assertWireGuardCall(t *testing.T, calls []string, want string) {
	t.Helper()
	for _, call := range calls {
		if call == want {
			return
		}
	}
	t.Fatalf("missing call %q in %#v", want, calls)
}

func TestResolveKeyFiles(t *testing.T) {
	dir := t.TempDir()
	privateKey := filepath.Join(dir, "private.key")
	presharedKey := filepath.Join(dir, "peer.psk")
	if err := os.WriteFile(privateKey, []byte("priv-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(presharedKey, []byte("psk-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := ResolveKeyFiles(InterfaceConfig{
		Name:           "wg0",
		PrivateKeyFile: privateKey,
		Peers: []PeerConfig{{
			Name:             "peer-a",
			PublicKey:        "pub",
			AllowedIPs:       []string{"10.10.0.2/32"},
			PresharedKeyFile: presharedKey,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrivateKey != "priv-from-file" || cfg.Peers[0].PresharedKey != "psk-from-file" {
		t.Fatalf("resolved keys = %+v", cfg)
	}
}

func TestEnsurePrivateKeyFileCreatesMissingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "wg0.key")
	if err := EnsurePrivateKeyFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %v, want 0600", info.Mode().Perm())
	}
	key, err := readSecretFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PublicKeyFromPrivateKey(key); err != nil {
		t.Fatalf("generated key is invalid: %v", err)
	}
}

func TestEnsurePrivateKeyFileDoesNotOverwriteExistingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wg0.key")
	key, err := GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateKeyFile(path); err != nil {
		t.Fatal(err)
	}
	got, err := readSecretFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != key {
		t.Fatalf("existing key was overwritten")
	}
}

func TestEnsurePrivateKeyFileRejectsEmptyExistingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wg0.key")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateKeyFile(path); err == nil || !strings.Contains(err.Error(), "empty key file") {
		t.Fatalf("EnsurePrivateKeyFile error = %v, want empty key file", err)
	}
}

func TestApplyDryRunDoesNotGeneratePrivateKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "wg0.key")
	controller := Controller{DryRun: true}
	if _, err := controller.Apply(context.Background(), InterfaceConfig{Name: "wg0", PrivateKeyFile: path}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dry-run created private key file: %v", err)
	}
}

func TestApplyGeneratesPrivateKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "wg0.key")
	var setconf string
	controller := Controller{
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "ip" && strings.Join(args, " ") == "link show wg0" {
				return nil, os.ErrNotExist
			}
			return nil, nil
		},
		CommandStdin: func(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			if name == "wg" && strings.Join(args, " ") == "setconf wg0 /dev/stdin" {
				setconf = string(stdin)
			}
			return nil, nil
		},
	}
	if _, err := controller.Apply(context.Background(), InterfaceConfig{Name: "wg0", PrivateKeyFile: path}); err != nil {
		t.Fatal(err)
	}
	key, err := readSecretFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PublicKeyFromPrivateKey(key); err != nil {
		t.Fatalf("generated key is invalid: %v", err)
	}
	if !strings.Contains(setconf, "PrivateKey = "+key) {
		t.Fatalf("setconf did not use generated key:\n%s", setconf)
	}
}

func TestParseDump(t *testing.T) {
	data := "priv\tpub\t51820\toff\npeerpub\tpsk\t203.0.113.2:51820\t10.0.0.2/32\t1710000000\t100\t200\t25\n"
	status, err := ParseInterfaceDump("wg0", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if status.Name != "wg0" || status.PublicKey != "pub" || status.ListenPort != 51820 {
		t.Fatalf("interface status = %+v", status)
	}
	peers, err := ParseDump([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 {
		t.Fatalf("peers = %+v, want one", peers)
	}
	if peers[0].PublicKey != "peerpub" || peers[0].LatestEndpoint != "203.0.113.2:51820" {
		t.Fatalf("peer = %+v", peers[0])
	}
	if want := time.Unix(1710000000, 0).UTC(); !peers[0].LatestHandshake.Equal(want) {
		t.Fatalf("handshake = %s, want %s", peers[0].LatestHandshake, want)
	}
	if peers[0].TransferRxBytes != 100 || peers[0].TransferTxBytes != 200 {
		t.Fatalf("transfer = %+v", peers[0])
	}
}

func TestParseAllDump(t *testing.T) {
	data := "wg0\tpriv\tpub\t51820\toff\nwg0\tpeerpub\tpsk\t203.0.113.2:51820\t10.0.0.2/32\t1710000000\t100\t200\t25\n"
	interfaces, err := ParseAllDump([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(interfaces) != 1 {
		t.Fatalf("interfaces = %+v, want one", interfaces)
	}
	if interfaces[0].Name != "wg0" || interfaces[0].PublicKey != "pub" || len(interfaces[0].Peers) != 1 {
		t.Fatalf("interface = %+v", interfaces[0])
	}
}
