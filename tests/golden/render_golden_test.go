// SPDX-License-Identifier: BSD-3-Clause

package golden_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/config"
	"routerd/pkg/render"
)

const updateEnv = "ROUTERD_UPDATE_GOLDEN"

func TestRenderGoldenExamples(t *testing.T) {
	examples, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) == 0 {
		t.Fatal("no examples found")
	}
	sort.Strings(examples)
	targets := []string{"linux", "alpine", "freebsd", "nixos"}
	for _, example := range examples {
		example := example
		name := strings.TrimSuffix(filepath.Base(example), filepath.Ext(example))
		t.Run(name, func(t *testing.T) {
			configPath := exampleConfigPath(t, example)
			router, err := config.Load(configPath)
			if err != nil {
				t.Fatalf("load %s: %v", example, err)
			}
			if err := config.Validate(router); err != nil {
				t.Fatalf("validate %s: %v", example, err)
			}
			for _, target := range targets {
				target := target
				t.Run(target, func(t *testing.T) {
					got, err := renderSnapshot(target, router)
					if err != nil {
						got = []byte("ERROR: " + err.Error() + "\n")
					}
					goldenPath := filepath.Join("render", target, name+".golden")
					assertGolden(t, goldenPath, got)
				})
			}
		})
	}
}

func exampleConfigPath(t *testing.T, path string) string {
	t.Helper()
	rel := filepath.ToSlash(strings.TrimPrefix(path, filepath.Clean(filepath.Join("..", ".."))+string(os.PathSeparator)))
	if isGitDirty(rel) {
		data, err := gitShow("HEAD:" + rel)
		if err == nil {
			tmp := filepath.Join(t.TempDir(), filepath.Base(path))
			if writeErr := os.WriteFile(tmp, data, 0644); writeErr != nil {
				t.Fatal(writeErr)
			}
			return tmp
		}
	}
	return path
}

func renderSnapshot(target string, router *api.Router) ([]byte, error) {
	switch target {
	case "linux":
		return renderLinuxSnapshot(router)
	case "alpine":
		return renderAlpineSnapshot(router)
	case "freebsd":
		return renderFreeBSDSnapshot(router)
	case "nixos":
		data, err := render.NixOSModule(router)
		if err != nil {
			return nil, err
		}
		return sectionedFiles(map[string][]byte{"routerd-generated.nix": data}), nil
	default:
		return nil, fmt.Errorf("unknown target %q", target)
	}
}

func renderLinuxSnapshot(router *api.Router) ([]byte, error) {
	files := map[string][]byte{}
	dnsmasqConfig, warnings, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{
		RuntimeDir: "/run/routerd",
		LeaseFile:  "/var/lib/routerd/dnsmasq/dnsmasq.leases",
	})
	if err != nil {
		return nil, err
	}
	addWarnings(files, warnings)
	addFile(files, "dnsmasq.conf", dnsmasqConfig)
	aliases := interfaceAliases(router)
	keepalived, err := render.KeepalivedConfig(router, aliases)
	if err != nil {
		return nil, err
	}
	addFile(files, "keepalived.conf", keepalived)
	frr, err := render.FRRConfig(router)
	if err != nil {
		return nil, err
	}
	addFile(files, "frr.conf", frr)
	frrDaemons, err := render.FRRDaemons(nil, router)
	if err != nil {
		return nil, err
	}
	addFile(files, "frr-daemons", frrDaemons)
	nat, err := render.NftablesIPv4SourceNAT(router)
	if err != nil {
		return nil, err
	}
	addFile(files, "nftables-nat.nft", nat)
	firewall, err := render.NftablesFirewall(router, render.InternalFirewallHoles(router))
	if err != nil {
		return nil, err
	}
	addFile(files, "nftables-filter.nft", firewall)
	return sectionedFiles(files), nil
}

func renderAlpineSnapshot(router *api.Router) ([]byte, error) {
	files := map[string][]byte{}
	data, err := render.OpenRC(router)
	if err != nil {
		return nil, err
	}
	dnsmasqConfig, warnings, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{
		RuntimeDir: "/run/routerd",
		LeaseFile:  "/var/lib/routerd/dnsmasq/dnsmasq.leases",
	})
	if err != nil {
		return nil, err
	}
	addWarnings(files, warnings)
	addFile(files, "dnsmasq.conf", dnsmasqConfig)
	keepalived, err := render.KeepalivedConfig(router, interfaceAliases(router))
	if err != nil {
		return nil, err
	}
	addFile(files, "keepalived.conf", keepalived)
	for name, content := range data.InitScripts {
		addFile(files, "openrc-"+name, content)
	}
	return sectionedFiles(files), nil
}

func renderFreeBSDSnapshot(router *api.Router) ([]byte, error) {
	files := map[string][]byte{}
	data, err := render.FreeBSDWithPPPoEPasswords(router, func(api.Resource, api.PPPoEInterfaceSpec) (string, error) { return "", nil })
	if err != nil {
		return nil, err
	}
	addFile(files, "rc.conf.d-routerd", data.RCConf)
	dnsmasqConfig, warnings, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{
		RuntimeDir: "/var/run/routerd",
		LeaseFile:  "/var/db/routerd/dnsmasq/dnsmasq.leases",
	})
	if err != nil {
		return nil, err
	}
	addWarnings(files, warnings)
	if len(dnsmasqConfig) > 0 {
		addFile(files, "dnsmasq.conf", dnsmasqConfig)
		addFile(files, "rc.d-routerd_dnsmasq", render.DnsmasqRCScript("/usr/local/etc/routerd/dnsmasq.conf", "/var/run/routerd", "/var/db/routerd/dnsmasq", "/usr/local/sbin/dnsmasq"))
	}
	addFile(files, "dhclient.conf", data.DHCPClient)
	addFile(files, "mpd5.conf", data.MPD5)
	addFile(files, "pf.conf", data.PF)
	addFile(files, "install-packages.sh", data.PackageInstall)
	for name, content := range data.RCDScripts {
		addFile(files, "rc.d-"+name, content)
	}
	return sectionedFiles(files), nil
}

func addFile(files map[string][]byte, name string, data []byte) {
	if len(bytes.TrimSpace(data)) == 0 {
		return
	}
	files[name] = data
}

func addWarnings(files map[string][]byte, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	sort.Strings(warnings)
	files["warnings.txt"] = []byte(strings.Join(warnings, "\n") + "\n")
}

func sectionedFiles(files map[string][]byte) []byte {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	for _, name := range names {
		fmt.Fprintf(&buf, "### %s\n", name)
		buf.Write(files[name])
		if len(files[name]) == 0 || files[name][len(files[name])-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes()
}

func interfaceAliases(router *api.Router) map[string]string {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err == nil {
			aliases[res.Metadata.Name] = spec.IfName
		}
	}
	return aliases
}

func assertGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv(updateEnv) == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v; run `make update-render-golden`", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("render output changed for %s; run `make update-render-golden` after reviewing the diff", path)
	}
}

func isGitDirty(path string) bool {
	cmd := exec.Command("git", "diff", "--quiet", "--", path)
	cmd.Dir = filepath.Join("..", "..")
	return cmd.Run() != nil
}

func gitShow(ref string) ([]byte, error) {
	cmd := exec.Command("git", "show", ref)
	cmd.Dir = filepath.Join("..", "..")
	return cmd.Output()
}
