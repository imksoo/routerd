# Lab validation host policy - 2026-05-18

This is an internal lab runbook note. Do not copy host-specific details from
this file into public website or product documentation.

## Native nDPI coverage

homert02 is the permanent native nDPI validation host. After any release/apply
run that touches homert02:

- leave `/usr/local/sbin/routerd` on the normal static release archive
- replace `/usr/local/sbin/routerd-ndpi-agent` from the matching
  `routerd-ndpi-agent-libndpi` archive
- verify `routerd-ndpi-agent.service` and `routerd-dpi-classifier.service`
- verify the nDPI agent status endpoint reports `libndpiLoaded=true`

When a validation batch has two hosts with the same OS and architecture, keep
one host on the normal static archive and keep one host on the native nDPI
agent override. For the current Ubuntu/amd64 lab coverage, homert02 is the
native nDPI side.

## RA and DHCPv6-PD coverage

pve01 through pve04 are available for future validation that needs RA or
DHCPv6-PD coverage. The previous RA/DHCPv6-PD blocker on those hosts has been
resolved; prior commit history and conversation logs are the evidence trail.

Include pve01 through pve04 when a change touches delegated IPv6 addresses,
DHCPv6-PD renewal, dnsmasq RA/DHCPv6 rendering, LAN IPv6 service gating, or
downstream reconciliation after prefix changes.
