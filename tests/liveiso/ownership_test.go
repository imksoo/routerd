// SPDX-License-Identifier: BSD-3-Clause

package liveiso_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Extract the actual production shell body. Only executable and filesystem
// boundaries are redirected into the fixture; recovery logic is not copied.
func liveFunction(t *testing.T, name string) string {
	t.Helper()
	script := liveISOScript(t)
	start := strings.Index(script, name+"()\n{\n")
	if start < 0 {
		t.Fatalf("missing function %s", name)
	}
	end := strings.Index(script[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("unterminated function %s", name)
	}
	return script[start : start+end+3]
}

func shellLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func runLiveShell(t *testing.T, body string) (string, error) {
	t.Helper()
	out, err := exec.Command("bash", "-eu", "-c", body).CombinedOutput()
	return string(out), err
}

func TestLiveBuildRefusesMountedWorkdirBeforeTraversal(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "build with spaces")
	if err := os.MkdirAll(filepath.Join(work, "rootfs", "dev"), 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(work, "rootfs", "dev", "host-sentinel")
	if err := os.WriteFile(sentinel, []byte("host contents"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".routerd-live-build"), []byte("routerd-live-build-v1 "+work+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mountinfo := filepath.Join(base, "mountinfo")
	encoded := strings.ReplaceAll(filepath.Join(work, "rootfs", "dev"), " ", `\040`)
	if err := os.WriteFile(mountinfo, []byte("10 1 0:2 / "+encoded+" rw - tmpfs tmpfs rw\n"), 0600); err != nil {
		t.Fatal(err)
	}
	observe := strings.ReplaceAll(liveFunction(t, "refuse_live_mounts"), "'/proc/self/mountinfo'", strconv.Quote(mountinfo))
	out, err := runLiveShell(t, "workdir="+shellLiteral(work)+"\n"+observe+"\n"+liveFunction(t, "prepare_live_workdir")+"\nprepare_live_workdir\n")
	if err == nil || !strings.Contains(out, "mounted path below") {
		t.Fatalf("mounted workdir admitted: %v %s", err, out)
	}
	data, err := os.ReadFile(sentinel)
	info, statErr := os.Stat(sentinel)
	if err != nil || statErr != nil || string(data) != "host contents" || info.Mode().Perm() != 0400 {
		t.Fatalf("pre-walk refusal changed native contents: %q %v %v", data, err, statErr)
	}
}

func TestLiveBuildUnownedWorkdirSurvives(t *testing.T) {
	work := t.TempDir()
	sentinel := filepath.Join(work, "operator-data")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	// This guard consumes Linux mountinfo, independently of the test host OS.
	mountinfo := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(mountinfo, []byte("10 1 0:2 / / rw - tmpfs tmpfs rw\n"), 0600); err != nil {
		t.Fatal(err)
	}
	observe := strings.ReplaceAll(liveFunction(t, "refuse_live_mounts"), "'/proc/self/mountinfo'", strconv.Quote(mountinfo))
	out, err := runLiveShell(t, "workdir="+shellLiteral(work)+"\n"+observe+"\n"+liveFunction(t, "prepare_live_workdir")+"\nprepare_live_workdir\n")
	if err == nil || !strings.Contains(out, "unowned live build workdir") {
		t.Fatalf("unowned workdir admitted: %v %s", err, out)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "preserve" {
		t.Fatalf("unowned workdir was changed: %q %v", data, err)
	}
}

func TestLiveBuildFailedUnmountRetainsOnlyOwnedMount(t *testing.T) {
	// The fixture replaces the actual unmount syscall boundary, without mounts.
	body := liveFunction(t, "cleanup_mounts") + `
live_mounts=(/fixture/rootfs/dev /fixture/rootfs/proc)
umount() { [ "$2" != /fixture/rootfs/proc ]; }
if cleanup_mounts; then exit 91; fi
[ "${#live_mounts[@]}" -eq 1 ]
[ "${live_mounts[0]}" = /fixture/rootfs/proc ]
`
	if out, err := runLiveShell(t, body); err != nil {
		t.Fatalf("failed unmount lost ownership: %v %s", err, out)
	}
}

func TestFirstbootInvalidConfigCannotReplaceValidatedCache(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(strconv.FormatBool(restore), func(t *testing.T) {
			dir := t.TempDir()
			config, cache := filepath.Join(dir, "router.yaml"), filepath.Join(dir, "cached.yaml")
			if err := os.WriteFile(config, []byte("current"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cache, []byte("cached"), 0600); err != nil {
				t.Fatal(err)
			}
			validator := filepath.Join(dir, "routerd")
			if err := os.WriteFile(validator, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			name, retained, want := "cache_validated_config", cache, "cached"
			if restore {
				name, retained, want = "restore_validated_config_cache", config, "current"
			}
			fn := strings.ReplaceAll(liveFunction(t, name), "/usr/local/sbin/routerd", validator)
			body := "config_file=" + shellLiteral(config) + "\nvalidated_cache_file=" + shellLiteral(cache) + "\nvalidated_cache_dir=" + shellLiteral(dir) + "\nlog() { :; }\n" + fn + "\n" + name + "\n"
			if out, err := runLiveShell(t, body); err == nil {
				t.Fatalf("failed validation was acknowledged: %s", out)
			}
			if data, err := os.ReadFile(retained); err != nil || string(data) != want {
				t.Fatalf("failed validation overwrote %s: %q %v", retained, data, err)
			}
		})
	}
}

func TestFirstbootKeepsBootstrapDHCPUntilValidationAndRenderSucceed(t *testing.T) {
	for _, failure := range []string{"validate", "render", "none"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			network, units := filepath.Join(dir, "network"), filepath.Join(dir, "units")
			for _, path := range []string{network, units} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			dhcp := filepath.Join(network, "80-dhcp.network")
			if err := os.WriteFile(dhcp, []byte("management DHCP"), 0600); err != nil {
				t.Fatal(err)
			}
			validator := filepath.Join(dir, "routerd")
			program := "#!/bin/sh\nif [ \"$1\" = " + shellLiteral(failure) + " ]; then exit 1; fi\nprintf 'rendered unit\\n'\n"
			if err := os.WriteFile(validator, []byte(program), 0700); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(dir, "router.yaml")
			if err := os.WriteFile(config, []byte("config fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			script := liveISOScript(t)
			start := strings.Index(script, "\ninstall -d /run/routerd /var/lib/routerd \"${config_dir}\"\n")
			if start < 0 {
				t.Fatal("missing firstboot entry")
			}
			end := strings.Index(script[start:], "\nEOF\n")
			if end < 0 {
				t.Fatal("missing firstboot terminator")
			}
			tail := script[start : start+end]
			tail = strings.ReplaceAll(tail, "install -d /run/routerd /var/lib/routerd", "install -d "+shellLiteral(filepath.Join(dir, "runtime")))
			body := liveFunction(t, "prepare_routerd_service") + "\n" + liveFunction(t, "disable_bootstrap_dhcp") + "\n" + tail
			body = strings.NewReplacer("/usr/local/sbin/routerd-dns-resolver", filepath.Join(dir, "absent-resolver"), "/usr/local/sbin/routerd", validator, "/etc/systemd/system", units, "/etc/systemd/network", network).Replace(body)
			prefix := "config_file=" + shellLiteral(config) + "\nconfig_dir=" + shellLiteral(dir) + "\nlog() { :; }\nsystemctl() { :; }\napply_cloudinit_hostname() { :; }\nenable_qemu_guest_agent() { :; }\napply_ssh_bootstrap() { :; }\nrestore_config_disk_config() { return 0; }\n"
			out, err := runLiveShell(t, prefix+body)
			if (failure == "none") != (err == nil) {
				t.Fatalf("unexpected boot outcome: %v %s", err, out)
			}
			_, statErr := os.Stat(dhcp)
			if failure == "none" && !os.IsNotExist(statErr) {
				t.Fatalf("successful boot retained bootstrap: %v", statErr)
			}
			if failure != "none" && statErr != nil {
				t.Fatalf("failed boot removed bootstrap DHCP: %v", statErr)
			}
		})
	}
}
