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

func TestLiveISOUsesDebootstrapUbuntuBase(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"UBUNTU_SUITE",
		"ubuntu_suite=${UBUNTU_SUITE:-noble}",
		"UBUNTU_MIRROR",
		"debootstrap --variant=minbase",
		"\"${ubuntu_suite}\" \"${rootfs}\" \"${ubuntu_mirror}\"",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("debootstrap Ubuntu live ISO script missing %q", needle)
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
		"UBUNTU_BASE_PACKAGES",
		"UBUNTU_LIVE_PACKAGES",
		"chroot_run apt-get update",
		"linux-image-generic systemd-sysv dbus sudo casper initramfs-tools",
		"chroot_run apt-get install -y --no-install-recommends \"${ubuntu_base_package_list[@]}\" \"${ubuntu_package_list[@]}\"",
		"chroot_run apt-get clean",
		"mksquashfs \"${rootfs}\" \"${iso_root}/casper/filesystem.squashfs\" -noappend -comp xz",
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
		"install -m 0644 \"${kernel_image}\" \"${iso_root}/casper/vmlinuz\"",
		"install -m 0644 \"${initrd_image}\" \"${iso_root}/casper/initrd\"",
		"linux /casper/vmlinuz",
		"boot=casper",
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
