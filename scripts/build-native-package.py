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


def deb(root, output, version, arch):
    control = root / "DEBIAN"
    control.mkdir()
    (control / "control").write_text(
        f"Package: routerd\nVersion: {version}\nArchitecture: {arch}\n"
        "Maintainer: Kirino Minato <kirino.minato@gmail.com>\n"
        "Section: net\nPriority: optional\n"
        f"Installed-Size: {sum(p.stat().st_size for p in root.rglob('*') if p.is_file() and not p.is_symlink()) // 1024 + 1}\n"
        "Depends: systemd, init-system-helpers, iproute2, nftables, dnsmasq-base, ca-certificates\n"
        "Suggests: wireguard-tools, chrony, bind9-dnsutils, tcpdump, cron, jq, ppp, "
        "pppoe, conntrack, iputils-ping, iputils-tracepath, net-tools, kmod, "
        "radvd, strongswan-swanctl, iptables, keepalived, openssh-server\n"
        "Homepage: https://github.com/imksoo/routerd\n"
        "Description: declarative router daemon\n"
        " Clean-OS native installation of routerd and its protocol helpers.\n"
    )
    checksums = []
    for path in sorted(root.rglob("*")):
        if path.is_file() and not path.is_symlink() and control not in path.parents:
            checksums.append(f"{hashlib.md5(path.read_bytes()).hexdigest()}  {path.relative_to(root)}\n")
    (control / "md5sums").write_text("".join(checksums))
    service = (REPO / "packaging/native/debian-service.sh").read_text()
    for phase in ("prerm", "postinst", "postrm"):
        script = control / phase
        script.write_text(service.replace("set -eu\n", f"set -eu\nphase={phase}\n", 1))
        script.chmod(0o755)
    subprocess.run(
        ["dpkg-deb", "--root-owner-group", "--build", str(root),
         str(output / f"routerd_{version}_{arch}.deb")], check=True
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
    manifest = {
        "name": "routerd", "version": version, "origin": "net/routerd",
        "comment": "Declarative router daemon", "desc": "Clean-OS native routerd installation",
        "maintainer": "kirino.minato@gmail.com", "www": "https://github.com/imksoo/routerd",
        "prefix": "/usr/local", "abi": f"FreeBSD:14:{'aarch64' if arch == 'arm64' else arch}",
        "licenselogic": "single", "licenses": ["BSD3CLAUSE"],
        "flatsize": size, "files": files,
    }
    metadata = root.parent / "+MANIFEST"
    metadata.write_text(json.dumps(manifest))
    subprocess.run(["pkg", "create", "-r", str(root), "-M", str(metadata),
                    "-o", str(output)], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--format", choices=("deb", "pkg"), required=True)
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
    for relative in ("bin/routerd", "bin/routerctl", "bin/routerd-dhcp-event-relay",
                     "share/doc/LICENSE", "etc/routerd/router.yaml.sample"):
        if not (args.payload / relative).is_file():
            parser.error(f"missing payload file {relative}")
    args.output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="routerd-native-package-") as temporary:
        root = Path(temporary) / "root"
        stage(args.payload, root, args.format)
        # The native writer can leave a truncated archive on failure. Publish
        # only after success, with a rename on the destination filesystem.
        with tempfile.TemporaryDirectory(prefix=".routerd-build-", dir=args.output) as pending:
            pending = Path(pending).resolve()
            if args.format == "deb":
                deb(root, pending, args.version, args.arch)
            else:
                pkg(root, pending, args.version, args.arch)
            for artifact in pending.iterdir():
                artifact.replace(args.output / artifact.name)


if __name__ == "__main__":
    main()
