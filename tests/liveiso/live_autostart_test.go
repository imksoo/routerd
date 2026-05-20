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

func TestLivePersistenceSupportsLabeledConfigImport(t *testing.T) {
	data, err := os.ReadFile("../../scripts/build-live-iso.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	required := []string{
		"config_source_file=/run/routerd/live-config-source",
		"config_checksum_file=/run/routerd/live-config-sha256",
		"blkid -L ROUTERD_CONFIG",
		"/dev/sr*",
		"iso9660|udf",
		"read_only_config_media",
		"$5 == \"part\" || $5 == \"rom\"",
		"select_config_source()",
		"${mount_dir}/${persist_dir_name}/hosts/${host}.yaml",
		"${mount_dir}/${persist_dir_name}/hosts/${mac}.yaml",
		"${mount_dir}/${persist_dir_name}/router.yaml",
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
