// SPDX-License-Identifier: BSD-3-Clause

package liveiso_test

import (
	"os"
	"strings"
	"testing"
)

func TestLiveAutostartGuardsDuplicateServe(t *testing.T) {
	data, err := os.ReadFile("../../scripts/build-live-iso.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	required := []string{
		"routerd_serve_running()",
		"pgrep -x routerd",
		"pidof routerd",
		"tr '\\000' ' ' < \"/proc/${pid}/cmdline\"",
		"marker=/run/routerd/live-autostart.done",
		"if routerd_serve_running; then",
		"routerd serve was already running before config handoff; restarting after restore reason=LiveISOStaleServeRestarted",
		"rc-service routerd restart",
		"elif [ -x /etc/init.d/routerd ]; then",
		"rc-service routerd start",
		"rc-update show default",
		"rc-update del routerd default",
		"failed to remove routerd from default runlevel; relying on stale serve restart path",
		"elif [ ! -S \"${socket}\" ]; then",
		"nohup \"${routerd}\" serve",
		"cat > \"${overlay_root}/etc/init.d/routerd\"",
		"command_args=\"serve --config /usr/local/etc/routerd/router.yaml --socket /run/routerd/routerd.sock --status-socket /run/routerd/routerd-status.sock\"",
	}
	for _, needle := range required {
		if !strings.Contains(script, needle) {
			t.Fatalf("live autostart script missing %q", needle)
		}
	}
	if strings.Index(script, "if routerd_serve_running; then") > strings.Index(script, "nohup \"${routerd}\" serve") {
		t.Fatalf("duplicate serve guard must run before nohup routerd serve")
	}
	if strings.Contains(script, "rc-update add routerd default") {
		t.Fatalf("live autostart must not add routerd to default runlevel before config restore")
	}
}

func TestLiveAutostartEnsuresLoopbackBeforeRouterd(t *testing.T) {
	data, err := os.ReadFile("../../scripts/build-live-iso.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	required := []string{
		"ensure_loopback()",
		"ip link set lo up",
		"ip addr show dev lo",
		"ip addr add 127.0.0.1/8 dev lo",
		"ifconfig lo up",
		"ensure_loopback",
	}
	for _, needle := range required {
		if !strings.Contains(script, needle) {
			t.Fatalf("live autostart loopback setup missing %q", needle)
		}
	}

	loopbackIdx := strings.Index(script, "\nensure_loopback\n")
	if loopbackIdx < 0 {
		t.Fatal("ensure_loopback call not found")
	}
	for _, later := range []string{
		"/usr/share/routerd/live-persistence.sh init",
		"\"${routerd}\" validate",
		"rc-service routerd start",
		"nohup \"${routerd}\" serve",
	} {
		idx := strings.Index(script, later)
		if idx < 0 {
			t.Fatalf("%q not found in script", later)
		}
		if loopbackIdx > idx {
			t.Fatalf("ensure_loopback must run before %q", later)
		}
	}
}

func TestLivePersistenceSupportsLabeledConfigImport(t *testing.T) {
	data, err := os.ReadFile("../../scripts/build-live-iso.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	required := []string{
		"config_source_file=/run/routerd/live-config-source",
		"config_checksum_file=/run/routerd/live-config-sha256",
		"secrets_dir=/usr/local/etc/routerd/secrets",
		"blkid -L ROUTERD_CONFIG",
		"/dev/sr*",
		"iso9660|udf",
		"read_only_config_media",
		"$5 == \"part\" || $5 == \"rom\"",
		"select_config_source()",
		"${mount_dir}/${persist_dir_name}/hosts/${host}.yaml",
		"${mount_dir}/${persist_dir_name}/hosts/${mac}.yaml",
		"${mount_dir}/${persist_dir_name}/router.yaml",
		"restore_secrets",
		"${src_parent}/secrets",
		"${mount_dir}/${persist_dir_name}/secrets",
		"install -m 0600 \"${secret}\" \"${dest}\"",
		"save_secrets_to_usb",
		"record_config_source",
		"sha256sum \"${src}\"",
	}
	for _, needle := range required {
		if !strings.Contains(script, needle) {
			t.Fatalf("live persistence script missing %q", needle)
		}
	}
}

func TestLiveISOIncludesCDROMModulesForConfigMedia(t *testing.T) {
	data, err := os.ReadFile("../../scripts/build-live-iso.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	required := []string{
		"sr-mod",
		"cdrom",
		"isofs",
		"ata_piix",
		"ata_generic",
		"alpine_dev=cdrom:iso9660",
	}
	for _, needle := range required {
		if !strings.Contains(script, needle) {
			t.Fatalf("live ISO boot config missing %q", needle)
		}
	}
}

func TestLiveISOSSHOptIn(t *testing.T) {
	data, err := os.ReadFile("../../scripts/build-live-iso.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)

	// live-ssh.sh must be created and made executable in the overlay
	required := []string{
		"cat > \"${overlay_root}/usr/share/routerd/live-ssh.sh\"",
		"chmod 0755 \"${overlay_root}/usr/share/routerd/live-ssh.sh\"",
		// SSH is gated on the kernel cmdline flag
		"cmdline_value routerd.ssh",
		`[ "${val}" = "1" ]`,
		// Authorized keys are read from config media, never baked in
		"find_authorized_keys()",
		"${mount_dir}/${persist_dir_name}/authorized_keys",
		"${mount_dir}/${persist_dir_name}/hosts/${host}/authorized_keys",
		"${mount_dir}/${persist_dir_name}/hosts/${mac}/authorized_keys",
		// sshd must be started after keys are installed
		"install -m 0600 \"${keys_src}\" /root/.ssh/authorized_keys",
		"start_sshd",
		// sshd configuration must prohibit password auth and allow pubkey only
		"PermitRootLogin prohibit-password",
		"PasswordAuthentication no",
		"PubkeyAuthentication yes",
		// SSH setup is invoked from live-autostart.sh
		"/usr/share/routerd/live-ssh.sh",
		// Safety: not starting sshd without keys
		"routerd.ssh=1 set but no authorized_keys found on config media; not starting sshd",
		// MOTD mentions the SSH opt-in
		"routerd.ssh=1",
		"authorized_keys",
	}
	for _, needle := range required {
		if !strings.Contains(script, needle) {
			t.Fatalf("live SSH opt-in script missing %q", needle)
		}
	}

	// live-ssh.sh call must appear after install.sh --deps-only in live-autostart.sh
	depsIdx := strings.Index(script, "install.sh --deps-only")
	// The invocation in live-autostart.sh is distinct from the script creation block.
	sshInvoke := "/usr/share/routerd/live-ssh.sh >>"
	sshIdx := strings.Index(script, sshInvoke)
	if depsIdx < 0 {
		t.Fatal("install.sh --deps-only not found in script")
	}
	if sshIdx < 0 {
		t.Fatalf("%q call not found in script", sshInvoke)
	}
	if sshIdx < depsIdx {
		t.Fatal("live-ssh.sh must be called after install.sh --deps-only so openssh is available")
	}

	// Password auth must never be enabled – no PermitRootLogin yes / PasswordAuthentication yes
	if strings.Contains(script, "PasswordAuthentication yes") {
		t.Fatal("live SSH must not enable password authentication")
	}
	if strings.Contains(script, "PermitRootLogin yes") {
		t.Fatal("live SSH must not use PermitRootLogin yes; use prohibit-password")
	}
}

func TestLiveISOStartsQEMUGuestAgent(t *testing.T) {
	data, err := os.ReadFile("../../scripts/build-live-iso.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	required := []string{
		"start_qemu_guest_agent()",
		"is_virtual_environment",
		"for service in qemu-ga qemu-guest-agent; do",
		"rc-update add \"${service}\" default",
		"rc-service \"${service}\" restart",
		"command -v qemu-ga",
		"qemu-ga --daemonize",
		"start_qemu_guest_agent",
	}
	for _, needle := range required {
		if !strings.Contains(script, needle) {
			t.Fatalf("live ISO QEMU guest agent autostart missing %q", needle)
		}
	}

	depsIdx := strings.Index(script, "install.sh --deps-only")
	if depsIdx < 0 {
		t.Fatal("install.sh --deps-only not found in script")
	}
	if !strings.Contains(script[depsIdx:], "\nstart_qemu_guest_agent\n") {
		t.Fatal("QEMU guest agent must start after dependency installation")
	}
}

func TestInstallerAlpineDepsIncludeQEMUGuestAgent(t *testing.T) {
	data, err := os.ReadFile("../../packaging/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	if !strings.Contains(script, "qemu-guest-agent") {
		t.Fatal("Alpine dependency packages must include qemu-guest-agent")
	}
}
