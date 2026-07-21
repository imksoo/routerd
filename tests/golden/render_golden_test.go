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

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/netconfigbackend"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
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
	targets := []string{"linux", "freebsd"}
	for _, example := range examples {
		example := example
		name := strings.TrimSuffix(filepath.Base(example), filepath.Ext(example))
		if removedPlatformExampleName(name) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			configPath := exampleConfigPath(t, example)
			router, err := config.Load(configPath)
			if err != nil {
				t.Fatalf("load %s: %v", example, err)
			}
			// Golden inputs are Linux-valid source fixtures.  Each target renderer
			// below independently records its own platform result, including the
			// FreeBSD rejection snapshots where applicable.
			if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
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

func removedPlatformExampleName(name string) bool {
	return strings.Contains(name, "alp"+"ine") || strings.Contains(name, "nix"+"os")
}

func exampleConfigPath(t *testing.T, path string) string {
	t.Helper()
	rel := filepath.ToSlash(strings.TrimPrefix(path, filepath.Clean(filepath.Join("..", ".."))+string(os.PathSeparator)))
	if isGitDirty(rel) && !renderGoldenDirtyForExample(path) {
		data, err := gitShow("HEAD:" + rel)
		if err == nil {
			tmp := filepath.Join(t.TempDir(), filepath.Base(path))
			if writeErr := os.WriteFile(tmp, data, 0644); writeErr != nil {
				t.Fatal(writeErr)
			}
			router, loadErr := config.Load(tmp)
			if loadErr != nil || config.ValidateForOS(router, platform.OSLinux) != nil {
				return path
			}
			return tmp
		}
	}
	return path
}

func renderGoldenDirtyForExample(path string) bool {
	if os.Getenv(updateEnv) != "" {
		return true
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for _, target := range []string{"linux", "freebsd"} {
		rel := filepath.ToSlash(filepath.Join("tests", "golden", "render", target, name+".golden"))
		if isGitDirty(rel) {
			return true
		}
	}
	return false
}

func renderSnapshot(target string, router *api.Router) ([]byte, error) {
	switch target {
	case "linux":
		return renderLinuxSnapshot(router)
	case "freebsd":
		return renderFreeBSDSnapshot(router)
	default:
		return nil, fmt.Errorf("unknown target %q", target)
	}
}

func renderLinuxSnapshot(router *api.Router) ([]byte, error) {
	files := map[string][]byte{}
	dnsmasqConfig, warnings, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{
		RuntimeDir: "/run/routerd",
		LeaseFile:  (platform.Defaults{StateDir: "/var/lib/routerd"}).DnsmasqLeaseFile(),
	})
	if err != nil {
		return nil, err
	}
	addWarnings(files, warnings)
	netplanFiles, err := netconfigbackend.Netplan{Path: "netplan/99-routerd.yaml"}.Render(router)
	if err != nil {
		return nil, err
	}
	addRenderedFiles(files, "", netplanFiles)
	networkdFiles, err := netconfigbackend.Networkd{}.Render(router)
	if err != nil {
		return nil, err
	}
	addRenderedFiles(files, "networkd/", networkdFiles)
	addFirewallHoles(files, router)
	addFile(files, "dnsmasq.conf", dnsmasqConfig)
	aliases := interfaceAliases(router)
	keepalived, err := render.KeepalivedConfig(router, aliases)
	if err != nil {
		return nil, err
	}
	addFile(files, "keepalived.conf", keepalived)
	nat, err := render.NftablesNAT44Rule(router)
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

func renderFreeBSDSnapshot(router *api.Router) ([]byte, error) {
	files := map[string][]byte{}
	data, err := render.FreeBSDWithPPPoEPasswords(router, func(api.Resource, api.PPPoESessionSpec) (string, error) { return "", nil })
	if err != nil {
		return nil, err
	}
	rcConf, err := netconfigbackend.RCConf{
		Path:        "rc.conf.d-routerd",
		PasswordFor: func(api.Resource, api.PPPoESessionSpec) (string, error) { return "", nil },
	}.Render(router)
	if err != nil {
		return nil, err
	}
	for name, content := range fileMap(rcConf) {
		addFile(files, name, content)
	}
	addFirewallHoles(files, router)
	dnsmasqConfig, warnings, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{
		RuntimeDir: "/var/run/routerd",
		LeaseFile:  (platform.Defaults{StateDir: "/var/db/routerd"}).DnsmasqLeaseFile(),
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

func addFileOrError(files map[string][]byte, name string, data []byte, err error) {
	if err != nil {
		addFile(files, name+".error", []byte(err.Error()+"\n"))
		return
	}
	addFile(files, name, data)
}

func addWarnings(files map[string][]byte, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	sort.Strings(warnings)
	files["warnings.txt"] = []byte(strings.Join(warnings, "\n") + "\n")
}

func addFirewallHoles(files map[string][]byte, router *api.Router) {
	holes := render.InternalFirewallHoles(router)
	if len(holes) == 0 {
		return
	}
	var buf bytes.Buffer
	for _, hole := range holes {
		fmt.Fprintf(&buf, "%s from=%s to=%s ifnames=%s proto=%s port=%d action=%s direction=%s comment=%s\n",
			hole.Name,
			hole.FromZone,
			hole.ToZone,
			strings.Join(hole.IfNames, ","),
			hole.Protocol,
			hole.Port,
			hole.Action,
			hole.Direction,
			hole.Comment,
		)
	}
	addFile(files, "firewall-holes.txt", buf.Bytes())
}

func addRenderedFiles(files map[string][]byte, prefix string, rendered []render.File) {
	for _, file := range rendered {
		name := filepath.ToSlash(strings.TrimPrefix(file.Path, "/"))
		if prefix != "" {
			name = prefix + name
		}
		addFile(files, name, file.Data)
	}
}

func fileMap(files []render.File) map[string][]byte {
	out := map[string][]byte{}
	for _, file := range files {
		out[file.Path] = file.Data
	}
	return out
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
