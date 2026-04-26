---
title: Changelog
---

# Changelog

routerd is currently pre-release software. This changelog records meaningful
changes as the resource model takes shape.

## Unreleased

- Added a Docusaurus documentation site scaffold for `routerd.net`.
- Added a Docusaurus website configured for Cloudflare Pages at `routerd.net`.
- Added `NTPClient` for static `systemd-timesyncd` configuration.
- Added explicit dnsmasq `listenInterfaces` allow-listing.
- Scoped dnsmasq DNS bind addresses to router self addresses.
- Added remote syslog configuration support through `LogSink`.
- Added default route policy support for active `IPv4PolicyRouteSet` candidates.
- Added PPPoE interface rendering and systemd unit management.

## 0.1.0 Planning Baseline

- Initial resource model for interfaces, static IPv4, DHCP stubs, plugins,
  dry-run, status JSON, and systemd service layout.
