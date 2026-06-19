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

func TestLiveISOSupportsNoCloudHostname(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"cloudinit_mount_dir=/media/routerd-cloudinit",
		"cloudinit_candidates()",
		"CIDATA cidata",
		"/dev/disk/by-label/CIDATA",
		"cloudinit_user_data()",
		"${cloudinit_mount_dir}/user-data",
		"cloudinit_hostname_value()",
		"s/^[[:space:]]*hostname:[[:space:]]*//p",
		"set_live_hostname()",
		"hostnamectl set-hostname \"${host}\"",
		"apply_cloudinit_hostname()",
		"udevadm settle --timeout=10",
		"set hostname ${host} from NoCloud user-data",
		"apply_cloudinit_hostname || true",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO NoCloud hostname setup missing %q", needle)
		}
	}

	hostnameIdx := strings.Index(script, "apply_cloudinit_hostname || true")
	runtimeIdx := strings.Index(script, "install -d /run/routerd /var/lib/routerd \"${config_dir}\"")
	if hostnameIdx < 0 || runtimeIdx < 0 {
		t.Fatal("missing NoCloud hostname setup or live runtime setup")
	}
	if hostnameIdx > runtimeIdx {
		t.Fatal("NoCloud hostname must be applied before routerd live runtime setup")
	}
}

func TestLiveISOParsesCloudInitConfigURLSuccessAndFailure(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"cloudinit_value()",
		"cloudinit_first_value()",
		"routerd:[[:space:]]*$",
		"config_url config-url configUrl routerd_config_url routerd-config-url",
		"config_sha256 config-sha256 configSha256 routerd_config_sha256 routerd-config-sha256",
		"fetch_url()",
		"curl -fsSL --connect-timeout 30 --max-time 300 --retry 3",
		"restore_cloudinit_config()",
		"fetching routerd config from cloud-init config_url",
		"restored ${config_file} from cloud-init config_url",
		"[ -n \"${user_data}\" ] || { umount \"${cloudinit_mount_dir}\" 2>/dev/null || true; return 1; }",
		"[ -n \"${config_url}\" ] || { umount \"${cloudinit_mount_dir}\" 2>/dev/null || true; return 1; }",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO cloud-init config_url setup missing %q", needle)
		}
	}
}

func TestLiveISORejectsCloudInitConfigSHA256Mismatch(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"verify_sha256()",
		"sha256sum \"${file}\"",
		"cloud-init config_url sha256 mismatch",
		"verify_sha256 \"${tmp}\" \"${config_sha256}\" || { rm -f \"${tmp}\"; return 1; }",
		"if ! restore_config_disk_config && ! restore_cloudinit_configs && ! restore_provider_config; then",
		"cp /usr/local/etc/routerd/router.yaml.sample \"${config_file}\"",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO cloud-init sha256 handling missing %q", needle)
		}
	}
}

func TestLiveISOExtractsCloudInitConfigBundles(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"zstd",
		"install_config_bundle()",
		"*.tar.zst|*.tzst)",
		"tar --use-compress-program=zstd -xf \"${file}\"",
		"*.tar.gz|*.tgz)",
		"tar -xzf \"${file}\"",
		"cloud-init config bundle missing router.yaml",
		"install -m 0600 \"${work}/router.yaml\" \"${config_file}\"",
		"cp -a \"${work}/secrets/.\" \"${config_dir}/secrets/\"",
		"install -m 0600 \"${work}/metadata.json\" \"${config_dir}/metadata.json\"",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO cloud-init bundle handling missing %q", needle)
		}
	}
}

func TestLiveISOConfigDiskTakesPrecedenceOverCloudInit(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"config_mount_dir=/media/routerd-config",
		"config_disk_candidates()",
		"blkid -L ROUTERD_CONFIG",
		"/dev/disk/by-label/ROUTERD_CONFIG",
		"restore_config_disk_config()",
		"restored ${config_file} from ROUTERD_CONFIG media",
		"if ! restore_config_disk_config && ! restore_cloudinit_configs && ! restore_provider_config; then",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO config disk precedence missing %q", needle)
		}
	}

	precedenceIdx := strings.Index(script, "if ! restore_config_disk_config && ! restore_cloudinit_configs && ! restore_provider_config; then")
	sampleIdx := strings.Index(script, "cp /usr/local/etc/routerd/router.yaml.sample \"${config_file}\"")
	if precedenceIdx < 0 || sampleIdx < 0 {
		t.Fatal("missing config precedence chain or sample fallback")
	}
	if precedenceIdx > sampleIdx {
		t.Fatal("config disk and cloud-init restore must run before sample fallback")
	}
}

func TestLiveISOSupportsProviderIMDSDetection(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"detect_provider()",
		"nocloud_available()",
		"dmi_value()",
		"aws_detect()",
		"azure_detect()",
		"oci_detect()",
		"printf '%s\\n' nocloud",
		"printf '%s\\n' aws",
		"printf '%s\\n' azure",
		"printf '%s\\n' oci",
		"printf '%s\\n' unknown",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO IMDS provider detection missing %q", needle)
		}
	}

	nocloudIdx := strings.Index(script, "if nocloud_available; then")
	awsIdx := strings.Index(script, "elif aws_detect; then")
	azureIdx := strings.Index(script, "elif azure_detect; then")
	ociIdx := strings.Index(script, "elif oci_detect; then")
	if nocloudIdx < 0 || awsIdx < 0 || azureIdx < 0 || ociIdx < 0 {
		t.Fatal("provider detection chain not found")
	}
	if !(nocloudIdx < awsIdx && awsIdx < azureIdx && azureIdx < ociIdx) {
		t.Fatal("provider detection order must be NoCloud, AWS, Azure, OCI")
	}
}

func TestLiveISOSupportsAWSIMDSv2UserData(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"fetch_aws_userdata()",
		"X-aws-ec2-metadata-token-ttl-seconds: 300",
		"http://169.254.169.254/latest/api/token",
		"X-aws-ec2-metadata-token: ${token}",
		"http://169.254.169.254/latest/user-data",
		"--connect-timeout 2 --max-time 5",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO AWS IMDSv2 support missing %q", needle)
		}
	}
}

func TestLiveISOSupportsAzureIMDSUserData(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"fetch_azure_userdata()",
		"7783-7084-3265-9085-8269-3286-77",
		"Metadata: true",
		"http://169.254.169.254/metadata/instance?api-version=2021-02-01",
		"http://169.254.169.254/metadata/instance/compute/userData?api-version=2021-02-01&format=text",
		"base64 -d \"${tmp}\" > \"${dest}\"",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO Azure IMDS support missing %q", needle)
		}
	}
}

func TestLiveISOSupportsOCIIMDSUserData(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"fetch_oci_userdata()",
		"OracleCloud",
		"Authorization: Bearer Oracle",
		"http://169.254.169.254/opc/v2/instance/metadata/",
		"http://169.254.169.254/opc/v2/instance/metadata/user_data",
		"base64 -d \"${tmp}\" > \"${dest}\"",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO OCI IMDS support missing %q", needle)
		}
	}
}

func TestLiveISOUsesIMDSAfterNoCloudForHostnameAndConfig(t *testing.T) {
	script := liveISOScript(t)
	for _, needle := range []string{
		"fetch_provider_userdata()",
		"log \"read cloud-init user-data from ${provider} IMDS\"",
		"set hostname ${host} from IMDS user-data",
		"restore_provider_config()",
		"fetching routerd config from IMDS config_url",
		"restored ${config_file} from IMDS config_url",
		"if ! restore_config_disk_config && ! restore_cloudinit_configs && ! restore_provider_config; then",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("Ubuntu live ISO IMDS restore path missing %q", needle)
		}
	}
	hostnameNoCloudIdx := strings.Index(script, "for candidate in $(cloudinit_candidates")
	hostnameIMDSIdx := strings.Index(script, "if fetch_provider_userdata \"${provider_userdata_file}\"; then")
	if hostnameNoCloudIdx < 0 || hostnameIMDSIdx < 0 {
		t.Fatal("hostname NoCloud or IMDS path not found")
	}
	if hostnameNoCloudIdx > hostnameIMDSIdx {
		t.Fatal("hostname setup must try NoCloud before IMDS")
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
