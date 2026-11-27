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

## v20261127.1753

- Remove the unused scheduled-apply option, duplicate DNS resolver and RA observer side state/event histories, the DHCP relay direct socket/API path, the legacy in-process healthcheck runner, and the separate and built-in DPI classifiers. Keep daemon live status, durable DHCP callback recovery, standalone healthcheck units, and direct ndpi-agent classification.

## v20261127.1548

- The DNS resolver treats unexpected listener exits and failures to send responses as errors, records events outside the cache lock, and logs upstream healthy or down events only when the state changes.

## v20261126.1233

- DNS keeps configured DHCP reservation names and reverse records after lease removal.

## v20261119.1122

- Cached DNS answers return the remaining TTL. BGP reads local IPv4 addresses in one dump. Traffic history keeps active connections open even outside the detail sample. The main Debian package uses xz compression.

## v20261118.1332

- Honor dynamic BGP peer conditions, resolve resource kinds and log paths consistently, and open live state read-only for diagnostics. Simplify event history, scheduled observation and package builds. Build the Web Console with esbuild.

## v20261117.0407

- Unify main-unit ownership in the Debian package; replace client process supervision with service-manager operations, fix generated capabilities, start the DHCP fingerprint watcher with dnsmasq, and group routerd services in `routerd.slice`. Remove `render systemd-service`; main-daemon Telemetry updates require a restart. Preserve drop-ins and persistent data on removal.

## v20261117.0008

- Fix Debian package upgrades and reinstalls failing when systemd retains a non-existent routerd helper unit.

## v20261116.2323

- Unify Linux installation and upgrades on Debian packages, preserving existing configuration and managing service shutdown and startup through package installation.

## v20261115.1721

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
