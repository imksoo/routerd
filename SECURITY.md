# Security Policy

## Supported versions

routerd is pre-release software. Security fixes are provided for the latest
release only.

Please upgrade to the latest release before reporting a vulnerability that may
already be fixed.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability.

Report security issues by email:

```text
kirino.minato@gmail.com
```

Include:

- affected component
- impact
- reproduction steps
- relevant configuration snippets
- logs or packet captures with secrets removed

## Scope

Security-sensitive areas include:

- firewall and NAT rendering
- route and tunnel selection
- DHCP, DNS, NTP, and PPPoE daemons
- Web Console API exposure
- installer and live ISO persistence
- plugin loading

routerd does not currently provide a remote plugin registry. Any proposal to
add remote plugin installation requires a separate security design review.
