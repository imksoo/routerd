#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-3-Clause
"""Build native packages from the single Makefile distribution inventory.

Staging is unprivileged. Only dpkg-deb/pkg writes the package format; this
program never installs packages or changes the build host's service state.
"""

import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


REPO = Path(__file__).resolve().parent.parent


def stage(payload, root, package_format):
    prefix = root / ("usr" if package_format == "deb" else "usr/local")
    shutil.copytree(payload / "bin", prefix / "sbin")
    shutil.copytree(payload / "libexec", prefix / "libexec")
    shutil.copytree(payload / "share/doc", prefix / "share/doc/routerd")
    examples = prefix / "share/doc/routerd/examples"
    examples.mkdir()
    shutil.copy2(payload / "etc/routerd/router.yaml.sample", examples)
    (prefix / "bin").mkdir()
    (prefix / "bin/routerctl").symlink_to("../sbin/routerctl")
    (prefix / "libexec/routerd/dhcp-event-relay").symlink_to(
        "../../sbin/routerd-dhcp-event-relay"
    )
    if package_format == "deb":
        units = prefix / "lib/systemd/system"
        units.mkdir(parents=True)
        shutil.copy2(REPO / "contrib/systemd/routerd.service", units)
    else:
        rc = prefix / "etc/rc.d"
        rc.mkdir(parents=True)
        shutil.copy2(REPO / "contrib/freebsd/routerd", rc)
        (rc / "routerd").chmod(0o555)
        restore = prefix / "libexec/routerd/package-service-restore"
        restore.write_text((REPO / "packaging/native/freebsd-service.sh").read_text().replace(
            "set -eu\n", "set -eu\nphase=post-start\n", 1))
        restore.chmod(0o555)


def stage_ndpi(payload, root):
    binary = root / "usr/sbin/routerd-ndpi-agent"
    binary.parent.mkdir(parents=True)
    shutil.copy2(payload / "bin/routerd-ndpi-agent", binary)
    shutil.copytree(payload / "share/doc", root / "usr/share/doc/routerd-ndpi-agent-libndpi")


def normalize_modes(root):
    # Directory creation must not inherit a developer's group-writable umask.
    root.chmod(0o755)
    for path in root.rglob("*"):
        if path.is_symlink():
            continue
        if path.is_dir():
            path.chmod(0o755)
        elif path.is_file():
            path.chmod(path.stat().st_mode & 0o755)


def deb(root, output, version, arch, name="routerd"):
    control = root / "DEBIAN"
    control.mkdir()
    control.chmod(0o755)
    dependencies = "systemd, init-system-helpers, iproute2, nftables, dnsmasq-base, ca-certificates"
    if name != "routerd":
        scanner = root.parent / "debian"
        scanner.mkdir()
        (scanner / "control").write_text(
            "Source: routerd\nMaintainer: Kirino Minato <kirino.minato@gmail.com>\n\n"
            f"Package: {name}\nArchitecture: any\nDescription: native nDPI agent\n")
        substvars = subprocess.check_output([
            "dpkg-shlibdeps", "-O", "-e" + str(root / "usr/sbin/routerd-ndpi-agent")
        ], cwd=root.parent, text=True)
        libraries = next((line.removeprefix("shlibs:Depends=") for line in substvars.splitlines()
                          if line.startswith("shlibs:Depends=")), "")
        if "libndpi" not in libraries:
            raise SystemExit("native addon must link to a packaged libndpi runtime")
        dependencies = f"routerd (= {version}), {libraries}"
    suggestions = (
        "Suggests: wireguard-tools, chrony, bind9-dnsutils, tcpdump, cron, jq, ppp, "
        "pppoe, conntrack, iputils-ping, iputils-tracepath, net-tools, kmod, "
        "radvd, strongswan-swanctl, iptables, keepalived, openssh-server\n"
    ) if name == "routerd" else ""
    description = "declarative router daemon" if name == "routerd" else "native libnDPI agent for routerd"
    (control / "control").write_text(
        f"Package: {name}\nVersion: {version}\nArchitecture: {arch}\n"
        "Maintainer: Kirino Minato <kirino.minato@gmail.com>\n"
        "Section: net\nPriority: optional\n"
        f"Installed-Size: {sum(p.stat().st_size for p in root.rglob('*') if p.is_file() and not p.is_symlink()) // 1024 + 1}\n"
        f"Depends: {dependencies}\n"
        f"{suggestions}"
        "Homepage: https://github.com/imksoo/routerd\n"
        f"Description: {description}\n"
        " Clean-OS native installation of routerd and its protocol helpers.\n"
    )
    checksums = []
    for path in sorted(root.rglob("*")):
        if path.is_file() and not path.is_symlink() and control not in path.parents:
            checksums.append(f"{hashlib.md5(path.read_bytes()).hexdigest()}  {path.relative_to(root)}\n")
    (control / "md5sums").write_text("".join(checksums))
    service = (REPO / "packaging/native/debian-service.sh").read_text()
    phases = ("prerm", "postinst", "postrm")
    if name != "routerd":
        service = service[:service.index('case "$phase:${1:-}" in')]
        service += (REPO / "packaging/native/debian-ndpi-service.sh").read_text()
        phases = ("preinst", "prerm", "postinst", "postrm")
    for phase in phases:
        script = control / phase
        script.write_text(service.replace("set -eu\n", f"set -eu\nphase={phase}\n", 1))
        script.chmod(0o755)
    subprocess.run(
        ["dpkg-deb", "--root-owner-group", "--build", str(root),
         str(output / f"{name}_{version}_{arch}.deb")], check=True
    )


def pkg(root, output, version, arch):
    files = {}
    size = 0
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            files["/" + str(path.relative_to(root))] = {
                "uname": "root", "gname": "wheel", "perm": "0755"
            }
        elif path.is_file():
            files["/" + str(path.relative_to(root))] = {
                "uname": "root", "gname": "wheel", "perm": f"{path.stat().st_mode & 0o777:04o}"
            }
            size += path.stat().st_size
    # Record dependency versions/origins from the native build environment,
    # exactly as a normal ports build does; never fabricate package metadata.
    installed = subprocess.check_output(["pkg", "query", "-a", "%n\t%v\t%o"], text=True)
    dependencies = {}
    for line in installed.splitlines():
        name, dep_version, origin = line.split("\t")
        if name in ("dnsmasq", "ca_root_nss", "ndpi"):
            dependencies[name] = {"version": dep_version, "origin": origin}
    if set(dependencies) != {"dnsmasq", "ca_root_nss", "ndpi"}:
        raise SystemExit("native pkg build requires installed dnsmasq, ca_root_nss and ndpi build dependencies")
    dynamic = subprocess.check_output([
        "readelf", "-d", str(root / "usr/local/sbin/routerd-ndpi-agent")
    ], text=True)
    libraries = sorted({name for name in re.findall(r"\(NEEDED\).*?\[([^\]]+)\]", dynamic)
                        if name.startswith("libndpi.so.")})
    if not libraries:
        raise SystemExit("FreeBSD package must contain the native libndpi-linked agent ELF")
    service = (REPO / "packaging/native/freebsd-service.sh").read_text()
    scripts = {phase: service.replace("set -eu\n", f"set -eu\nphase={phase}\n", 1)
               for phase in ("pre-install", "post-install", "pre-deinstall", "post-deinstall")}
    manifest = {
        "name": "routerd", "version": version, "origin": "net/routerd",
        "comment": "Declarative router daemon", "desc": "Clean-OS native routerd installation",
        "maintainer": "kirino.minato@gmail.com", "www": "https://github.com/imksoo/routerd",
        "prefix": "/usr/local", "abi": f"FreeBSD:14:{'aarch64' if arch == 'arm64' else arch}",
        "licenselogic": "single", "licenses": ["BSD3CLAUSE"],
        "flatsize": size, "files": files, "deps": dependencies, "scripts": scripts,
        "shlibs_required": libraries,
    }
    metadata = root.parent / "+MANIFEST"
    metadata.write_text(json.dumps(manifest))
    subprocess.run(["pkg", "create", "-r", str(root), "-M", str(metadata),
                    "-o", str(output)], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--format", choices=("deb", "pkg"), required=True)
    parser.add_argument("--component", choices=("routerd", "routerd-ndpi-agent-libndpi"), default="routerd")
    parser.add_argument("--payload", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--arch", choices=("amd64", "arm64"), required=True)
    args = parser.parse_args()
    os_name = "linux" if args.format == "deb" else "freebsd"
    expected = f"{os_name}-{args.arch}"
    target = (args.payload / "share/doc/TARGET").read_text().strip()
    if target != expected:
        parser.error(f"payload target {target!r} does not match {expected!r}")
    if not re.fullmatch(r"[0-9][A-Za-z0-9.+~_-]*", args.version):
        parser.error("invalid native package version")
    required = ("bin/routerd", "bin/routerctl", "bin/routerd-dhcp-event-relay",
                "share/doc/LICENSE", "etc/routerd/router.yaml.sample")
    if args.format == "pkg":
        required += ("bin/routerd-ndpi-agent",)
    if args.component != "routerd":
        if args.format != "deb":
            parser.error("native libnDPI addon is Linux Debian-only")
        required = ("bin/routerd-ndpi-agent", "share/doc/LICENSE")
    for relative in required:
        if not (args.payload / relative).is_file():
            parser.error(f"missing payload file {relative}")
    args.output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="routerd-native-package-") as temporary:
        root = Path(temporary) / "root"
        if args.component == "routerd":
            stage(args.payload, root, args.format)
        else:
            stage_ndpi(args.payload, root)
        normalize_modes(root)
        # The native writer can leave a truncated archive on failure. Publish
        # only after success, with a rename on the destination filesystem.
        with tempfile.TemporaryDirectory(prefix=".routerd-build-", dir=args.output) as pending:
            pending = Path(pending).resolve()
            if args.format == "deb":
                deb(root, pending, args.version, args.arch, args.component)
            else:
                pkg(root, pending, args.version, args.arch)
            for artifact in pending.iterdir():
                artifact.replace(args.output / artifact.name)


if __name__ == "__main__":
    main()
