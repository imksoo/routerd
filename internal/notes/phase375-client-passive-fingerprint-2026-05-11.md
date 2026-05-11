# Phase 3.7.5 client passive fingerprint validation

Date: 2026-05-11

Scope:
- Add passive client fingerprint fields to Web Console client entries.
- Infer OS family and device class from hostname/vendor hints, DNS query patterns, multicast/traffic hints, and DPI-enriched flow/log fields.
- Group privacy IPv6 addresses into an existing client row when the passive fingerprint uniquely matches the client fingerprint.

Implementation notes:
- The Web Console API now exposes inferredOSFamily, inferredDeviceClass, fingerprintConfidence, and fingerprintSignals on client rows.
- Summary client fingerprinting uses the latest 24h of DNS queries while keeping the summary DNS table itself at the shorter display window.
- DHCP hostname/vendor/clientID signals are applied to the concrete DHCP client row, not to an IP-level passive fingerprint. This avoids leaking stale DHCP lease identity across IP reuse.
- Device class is selected from signals associated with the selected OS family first. This keeps mixed traffic such as iPhone + Microsoft/Office DNS from changing the class to Windows computer.
- DHCP option-order/PRL fingerprints are not available in the current dnsmasq lease-file data model. Adding them requires packet capture or richer dnsmasq event relay payloads in a later phase.

Local verification:
- go test ./pkg/webconsole
- go test ./...
- npm run typecheck
- npm run build
- make check-schema validate-example
- make build-daemons check-linux-static
- make build-daemons-freebsd

homert02 validation:
- Deployed static linux routerd/routerctl to homert02 and restarted routerd.service.
- routerd.service: active
- routerctl status: Healthy, generation 53, resourceCount 89
- /api/v1/clients showed inferred families for live clients:
  - Android: Pixel-10, motorola-edge-40, Google devices, DNS googleapis/gstatic patterns.
  - Apple: iPhone, iPad, MacBookAir, iCloud/Apple DNS patterns.
  - Windows: WIN-GCAK5B48NM0, T415-639, NetBIOS multicast and Microsoft/update DNS patterns.
- Privacy IPv6 addresses are present in the same client rows as their DHCP/neighbor identity where MAC or unique fingerprint correlation is available.
