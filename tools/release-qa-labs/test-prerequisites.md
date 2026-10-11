# Existing test prerequisites

This source-based map covers the existing test entry-point families. It is not a
new gate or proof of live validation. Preparation is split across shell scripts,
OpenTofu userdata, installers, images, CI and selected external qualification
plans. An operator CLI check does not establish a guest CLI dependency.

| Entry points | Execution prerequisites | Definitions |
| --- | --- | --- |
| `make test`, Go package tests, golden/install/liveiso fixtures, schema/wizard targets | Go 1.25/toolchain 1.25.9, `go.mod` dependencies; shell/coreutils/Python for invoked fixtures. Installer fixtures use temporary prefixes/mock commands. | `go.mod`, `Makefile`, `.github/workflows/quality.yaml`, individual tests |
| Cloudedge/release/QA offline tests | Bash, Python 3, jq, coreutils; configuration generation also uses wg. Provider/guest commands are substituted by fixtures. | Existing cloudedge/release Make targets, `tests/e2e/cloudedge`, `tools/release-qa-labs/tests` |
| SAM single/minimal/full/representative, protocol/performance/failover/rejoin | Operator: OpenTofu and locked providers, aws/az/oci, selected account/profile/region permissions, SSH/SCP/known-hosts/public-key access, jq/Python/wg/timeout. PVE: reachable API/SSH, selected VMIDs/bridges/storage/media/template and QGA where required. | QA certification/common drivers, Terraform env/modules, `sam-preflight.sh`, `sam-e2e.sh` |
| AWS SAM guest | Existing cloud-init AWS CLI v2 installer (curl/unzip), SAM preparation fallback, /usr/local/bin; EC2 instance role and private-IP permissions. Inventory and executor invoke aws. | `terraform/modules/aws_leaf/main.tf`, `sam-e2e.sh`, packaged AWS executor/inventory |
| Azure SAM guest | Existing cloud-init Azure CLI installer, az version; guest executor invokes az with managed identity. Inventory tries IMDS/ARM REST first and CLI fallback. REST success does not remove the executor's CLI requirement. AZURE_CONFIG_DIR must be writable in the execution context. | `terraform/modules/azure_leaf/main.tf`, packaged Azure executor, `provider-private-ip-inventory`, generated provider env |
| OCI SAM guest | Existing cloud-init Oracle installer into /opt/oci-cli with /usr/local/bin/oci. Inventory/executor need oci in root service PATH; IMDS identity, instance-principal dynamic group and private-IP/VNIC/subnet permissions. SAM adds no second OCI installer. | `terraform/modules/oci_leaf/main.tf`, packaged OCI executor/inventory, generated provider env |
| Router guests and client traffic cases | Artifact/config/key transfer and hash binding; Linux systemd/root sudo; runtime dependency list in installer. Client functions prepare their selected ping/iperf3/protocol tools. Capture needs tcpdump/privilege; optional sqlite3/GoBGP absence is reported. | `packaging/install.sh`, SAM deploy/client functions, `scripts/runners/*` |
| Linux netns, including standalone cases | Disposable Linux, explicit root/CAP_NET_ADMIN, namespace/veth/VXLAN/bridge and firewall kernel support; iproute2/Python/timeout. Scenario-specific tools/drivers are listed in the netns README. | `tests/netns/lib.sh`, individual scenarios, `tests/netns/README.md` |
| Live ISO/rootfs/static and optional nDPI | Go/build inputs; root/debootstrap/chroot/network package access; full ISO additionally squashfs-tools, GRUB rescue tools/xorriso. QGA/runtime packages are defined by the image list; optional libndpi/CGO uses separate build dependencies. | `scripts/build-live-iso.sh`, installer, Make build/check targets |
| Web console/website/browser/wizard UI | Node/npm lockfiles, package dependencies; Playwright Chromium and OS dependencies for screenshots; Go/schema generators for relevant targets. | package manifests, Make targets, quality workflow |
| FreeBSD cross-build/native VM/VNET/CARP/PPPoE/tunnel/IPsec/WireGuard/Tailscale/lifecycle definitions | Cross-build: Go. Native: QEMU/image/SSH/TAP, root/VNET/kernel modules, declared guest packages; lifecycle adds sqlite3/kea, PPPoE uses mpd5, Tailscale/WireGuard require their tools. Definitions alone do not establish a native run. | FreeBSD workflow, `scripts/ci/freebsd-*.sh`, `scripts/freebsd-*.sh` |
| External PVE/k8s qualification plans | The selected plan's command/helper files, media/binary hashes, guest management, capture/process ownership, HTTP/backend and route/CNI inputs. No cloud CLI or generic unit check supplies these implicitly. | Existing certification/qualification contract and plan, environment-preflight and qualification wrapper |

## Existing dependency verification and failure evidence

AWS/Azure/OCI cloud-init command lists use `set -e` so an installer or version
failure cannot be replaced by a final successful cleanup. Guest preparation saves
cloud-init status and the owned lab's installer output, then verifies the selected
CLI in the root service PATH before deploying routerd. The existing AWS fallback
remains; no Azure/OCI duplicate installer or traffic acceptance condition is added.

Existing route/ownership attempts and warm-up probes retain stdout/stderr, exit,
UTC start/finish and target markers. Journals use sudo. WireGuard collection uses
public handshake/transfer/endpoint/allowed-IP fields, never private-key dump data.
Missing GoBGP is exit 127, not non-JSON input to jq. Diagnostic failure does not
change traffic verdicts.

Run `make cloudedge-e2e-preflight-offline-test` and
`make cloudedge-representative-redundancy-offline-test` for affected fixtures.
These prove failure propagation/evidence behavior, not cloud installation or
connectivity. Upstream installer/package/network/auth results and externally
supplied plans require their own saved execution evidence.
