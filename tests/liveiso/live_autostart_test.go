// SPDX-License-Identifier: BSD-3-Clause

package liveiso_test

import (
	"os"
	"strings"
	"testing"
)

func liveISOScript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../scripts/build-live-iso.sh")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLiveISOUsesUbuntuBase(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"UBUNTU_ISO_URL",
		"ubuntu-24.04",
		"ubuntu_iso_url",
		"ubuntu_iso=",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO script missing %q", needle)
		}
	}
}

func TestLiveISOIncludesRouterdPayload(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"payload_root=\"${iso_root}/routerd\"",
		"rootfs=\"${workdir}/rootfs\"",
		"make build-daemons ROUTERD_OS=linux GOARCH=amd64",
		"install -m 0755 packaging/install.sh",
		"install -m 0755 packaging/uninstall.sh",
		"router.yaml.sample",
		"THIRD_PARTY_LICENSES.txt",
		"/cdrom/routerd/install.sh configure",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO payload script missing %q", needle)
		}
	}
}

func TestLiveISOInstallsUbuntuPackagesIntoSquashFS(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"UBUNTU_LIVE_PACKAGES",
		"unsquashfs -d \"${rootfs}\" \"${squashfs}\"",
		"chroot_run apt-get update",
		"chroot_run apt-get install -y --no-install-recommends \"${ubuntu_package_list[@]}\"",
		"chroot_run apt-get clean",
		"mksquashfs \"${rootfs}\" \"${squashfs}\" -noappend -comp xz",
		"filesystem.size",
		"filesystem.manifest",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO root customization missing %q", needle)
		}
	}
}

func TestLiveISOUsesSystemdFirstBootSetup(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"routerd-live-setup.service",
		"WantedBy=multi-user.target",
		"systemctl enable routerd.service",
		"systemctl enable routerd-dns-resolver@lan-resolver.service",
		"multi-user.target.wants/routerd-live-setup.service",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO systemd setup missing %q", needle)
		}
	}
}

func TestLiveISOBootsUbuntuCasperWithSerialConsole(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"grub-mkrescue",
		"menuentry \"routerd Ubuntu live",
		"linux /casper/vmlinuz",
		"initrd /casper/initrd",
		"console=ttyS0,115200n8",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO boot config missing %q", needle)
		}
	}
}

func TestLiveISOProducesReleaseWorkflowArtifacts(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"out_iso=\"${outdir}/routerd-live-${version}.iso\"",
		"alias_iso=\"${outdir}/routerd-live.iso\"",
		"checksum_file \"${out_iso}\"",
		"checksum_file \"${alias_iso}\"",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO release artifact handling missing %q", needle)
		}
	}
}
