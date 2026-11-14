---
title: Changelog
---

# Changelog

routerd release history. The format follows [Keep a Changelog](https://keepachangelog.com/).
Changes are grouped under Added, Changed, Deprecated, Removed, Fixed, and Security.
Versions, however, do not follow Semantic Versioning; routerd uses date-and-time-based
release versions in `vYYYYMMDD.HHmm` format.
The software is at the v1alpha1 stage; releases may contain breaking changes.

## Unreleased

- Remove the browser configuration wizard and publish plain Markdown documentation with YAML definitions for every resource kind.

## v20261114.0631

### Fixed

- Treat an empty IPv4 address list as an unconfigured IPIP interface so first-boot reconciliation can assign its addresses.
- Use the existing casper `noprompt` option for appliance Live ISO reboots; do not require an interactive media-removal confirmation.

## v20261114.0607

### Changed

- Simplify typed configuration access, SQLite-backed controller stores, status transitions, event consumers, MTU selection, SAM input decoding, and Linux IPIP observation. Remove unused compatibility paths and redundant test-only code.
- Keep PD lease data in one authoritative object status and define its stored fields once, preserving lease identities, deadlines, and previous prefixes.

### Fixed

- Restore the matching applied configuration generation after daemon restart.
- Stop pending EventRule timers when configuration changes remove their controller generation, and remove stale object statuses after successful configuration persistence.

[Release history](https://github.com/imksoo/routerd/releases)
