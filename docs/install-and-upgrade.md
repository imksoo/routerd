---
title: Install and upgrade
---

# Install and upgrade

The routerd 1.5 proposal uses native Debian/Ubuntu `.deb` and FreeBSD `.pkg`
packages on a clean OS. These changes are under development; the older stable
release's archives are not 1.5 packages. No public apt/pkg repository is announced
here. Obtain the package for the intended build, OS and CPU architecture, and
verify its accompanying checksum before installation.

Package-to-package updates are supported by this installation contract. Existing
1.0 configurations, `make install`, manually copied binaries and archive-based
installations are outside it. Prepare a reviewed 1.5 configuration on a clean OS
rather than attempting to adopt those installations.

## Install

For Debian/Ubuntu, pass the downloaded local package to apt so dependencies are
resolved from the configured OS repositories:

```sh
sudo apt install ./routerd_<version>_amd64.deb
```

For FreeBSD 14, use the native package manager:

```sh
sudo pkg -o HANDLE_RC_SCRIPTS=true install ./routerd-<version>.pkg
```

Replace the placeholders with the actual downloaded filename; use arm64 packages
on arm64 hosts. FreeBSD's native ABI names that architecture `aarch64`. Linux
routerd binaries are statically linked with `CGO_ENABLED=0`; OS tools still have
runtime dependencies. The standard FreeBSD package includes the native libnDPI
agent and its actual shared-library/runtime-package dependencies. Package metadata
declares baseline dependencies. Additional
features can require additional OS packages; review the selected configuration's
requirements before applying it.

Fresh installation installs files and service integration without starting the
daemon, enabling routing or applying a sample configuration. The package does not
contain the operator's actual configuration or database.

| Purpose | Debian/Ubuntu | FreeBSD |
| --- | --- | --- |
| Main daemon | `/usr/sbin/routerd` | `/usr/local/sbin/routerd` |
| Configuration | `/etc/routerd/router.yaml` | `/usr/local/etc/routerd/router.yaml` |
| Examples | `/usr/share/doc/routerd/examples` | `/usr/local/share/doc/routerd/examples` |
| Durable state | `/var/lib/routerd` | `/var/db/routerd` |
| Runtime sockets | `/run/routerd` | `/var/run/routerd` |
| Main service | vendor systemd unit | `/usr/local/etc/rc.d/routerd` |

## Prepare configuration and start

Review interface names, addressing, DHCP/DNS, NAT and firewall policy before
starting a router. Keep console access or a separate management path. The large
bundled sample is reference material, not a safe configuration for every network.
Validate your prepared configuration first:

```sh
# Debian/Ubuntu
sudo routerd validate --config /etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
sudo systemctl status routerd.service

# FreeBSD
sudo routerd validate --config /usr/local/etc/routerd/router.yaml
sudo sysrc routerd_enable=YES
sudo service routerd start
sudo service routerd status
```

Starting a configured daemon can change network state. Validate syntax before
startup, then verify actual client traffic and the intended failure/recovery
behavior. `routerctl get status` provides runtime status; successful installation
alone does not prove that a routing configuration works.

## Local control socket access for non-root operators

Run `routerctl` with sudo when using the privileged control socket. If a `routerd`
group exists, the daemon grants that group access to its local sockets; membership
therefore grants control access, not just read-only status. The native package
does not create that group. Review this boundary before creating a group or adding
operators, then start a new login session for changed membership to take effect.
The default status endpoint is `/run/routerd/routerd-status.sock` on Linux and
`/var/run/routerd/routerd-status.sock` on FreeBSD.

## Update a package installation

Keep the operator configuration and durable state in the paths above. Install the
newer local package with the same apt/pkg command used for initial installation.
The package manager tracks distributed binaries and service files; do not replace
those files with `make install` or manual copies.

Debian update hooks remember previously active owned services, stop the old daemon
and helpers before replacement, and restore active services after installation.
Previously inactive Debian services stay inactive. FreeBSD uses pkg's native
`HANDLE_RC_SCRIPTS=true` service policy: enable `routerd_enable=YES` for an
operating router, and use the option in the command above when updating. Native
pkg starts enabled rc.d services after hook cleanup; main rc.d startup then
restores previously active owned helpers. An enabled but previously stopped
FreeBSD service can therefore start during the update. The option applies to that
pkg invocation and does not alter global pkg configuration. Without native service
handling, an update can leave routerd stopped. Starting daemons directly from a
pkg hook is insufficient because pkg reaps hook descendants.
A fresh installation without an enabled service remains inactive. Configuration and database contents are retained. A package update can
interrupt traffic and cause protocol reconvergence; verify independent client
traffic after the update. The package hooks do not implement a database-format
migration contract or guarantee uninterrupted BGP sessions.

If stopping an old service fails, the update's pre-install step must reject
replacement. Inspect the package manager's diagnostics and actual service status;
do not treat a package-manager exit code alone as proof that traffic recovered.
On Debian/Ubuntu, service policy can prevent restarts; resolve that policy before
expecting an active daemon to recover. Do not repeatedly restart a reconverging
protocol daemon to make a status check pass.

## Remove

```sh
# Debian/Ubuntu
sudo apt remove routerd

# FreeBSD
sudo pkg delete routerd
```

Removal stops owned services and removes package files and marked generated
service helpers. Operator configuration and the durable database remain, including
after Debian purge. Removal is not a promise to undo every network effect of a
running configuration: withdraw the desired resources and verify client/kernel
state before removing a router from service. Delete retained configuration/state
only when the operator explicitly intends to discard them.

## Build and verify

Use `make dist-deb ROUTERD_OS=linux` or native FreeBSD
`gmake dist-pkg ROUTERD_OS=freebsd`. Set `GOARCH` and `PACKAGE_VERSION` explicitly
when building release artifacts. FreeBSD packaging requires native `pkg`, Python, a C compiler, `pkgconf`,
`readelf`, `dnsmasq`, `ca_root_nss` and `ndpi`; Linux packaging requires `dpkg-deb` and Python. The
release workflow builds both amd64 and arm64 packages; a workflow definition does
not itself constitute an executed release or runtime qualification for both CPUs.

The reproducible installation/update/removal gates and their pass criteria are in
[Development](./operations/development.md). They run only in disposable OS guests.
They check real package replacement, independent DNS traffic, old process exit,
configuration and SQLite value retention, and removal. They do not certify every
routerd function. Linux native libnDPI uses a matching
[addon package](./operations/ndpi-agent-libndpi.md); do not overwrite
package-owned agent files with an archive.
