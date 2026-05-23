---
title: Legal and redistribution
---

# Legal and Redistribution

routerd itself is distributed under the BSD 3-Clause License. The full license
text is in the repository root as `LICENSE`.

The routerd copyright notice is:

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

This page summarizes the practical redistribution rules for routerd release
archives and the routerd live ISO. It is an operational note, not legal advice.

## routerd binaries

The routerd binaries are built from Go source code in this repository. Before a
release, run:

```sh
make third-party-licenses
```

That command regenerates `THIRD_PARTY_LICENSES.md`. It lists:

- Go modules linked into routerd binaries
- detected license text type
- license file name
- module source URL
- Alpine packages used by the live ISO
- Alpine package license metadata and upstream URL

The current audit path checks Go module license files for GPL, LGPL, and AGPL
text. If such a linked Go module appears, stop the release and review whether
the routerd binary license needs to change or the dependency needs to be
removed.

Source files use SPDX identifiers such as:

```text
SPDX-License-Identifier: BSD-3-Clause
```

Those headers identify the routerd source license. They do not change the
licenses of bundled tools, Alpine packages, Go modules, or other third-party
components listed in `THIRD_PARTY_LICENSES.md`.

## Release archives

Release archives contain:

- routerd binaries
- installer scripts
- systemd or rc.d service templates
- sample configuration
- `share/doc/LICENSE`
- `share/doc/THIRD_PARTY_LICENSES.md`

When redistributing a release archive, keep those files with the archive.

## Live ISO

The live ISO is an aggregate distribution. It combines:

- routerd binaries and scripts
- Alpine Linux base files
- Alpine packages such as dnsmasq, nftables, WireGuard tools, ppp, iproute2,
  chrony, tcpdump, and related utilities

Those Alpine packages keep their own upstream licenses. Some are GPL licensed.
The live ISO is not relicensed as a single GPL work.

The live ISO includes the routerd notices at:

```text
/usr/share/licenses/routerd/LICENSE
/usr/share/licenses/routerd/THIRD_PARTY_LICENSES.txt
```

Source information for Alpine packages is available from Alpine package
repositories, APKBUILD records, and the upstream URLs listed in
`THIRD_PARTY_LICENSES.md`.

## Release checklist

Before publishing a release:

1. Run `make third-party-licenses`.
2. Confirm the Go module copyleft check reports no GPL, LGPL, or AGPL module.
3. Confirm GPL-family licenses only appear in separately distributed Alpine
   packages or other external tools.
4. Run the normal test, schema, example, website, archive, and live ISO checks.
5. Confirm release archives include `share/doc/LICENSE` and
   `share/doc/THIRD_PARTY_LICENSES.md`.
6. Confirm the live ISO includes `/usr/share/licenses/routerd/`.

If the dependency set changes substantially, review this page and the generated
license inventory before tagging the release.
