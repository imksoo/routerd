# FreeBSD VM115 Stage40 syntax smoke

Commit `418a0ce876a5db6abbffbad2cdb8f534f3ea8d4c` completed the isolated
FreeBSD 14 VM115 syntax smoke on 2026-07-20. The runner returned `0` and
recorded `syntax-smoke=ok`.

- routerd SHA-256: `cb0c31168b22feb883bee07b3960af32135f8e009e8a58dd8536a34e05f53e68`
- routerctl SHA-256: `af37832caa63ab51952452d1a25a9c7ead8dfef666b714b0f23d26bec311805f`
- retained ISO SHA-256: `2b84a7692b23743c0bbc7e1a94494bcaec18bde2f4d9f9fab590503ddfccf327`
- evidence tree: `/tmp/vm115-stage40-v4.FBsbzx`

Observed guest checks: validate valid, plan Healthy, FreeBSD render generated six
files, `pfctl -nf` succeeded, dnsmasq reported syntax-check OK, and generated
rc.d scripts passed `sh -n`; `rc.d-routerd` status returned 0 and
`rc.d-routerd_dnsmasq` returned the allowed not-running status 1. Host
postflight was stopped, args absent, exact v4 ide2, vmbr404 NO-CARRIER, and
retained snapshots unchanged.

This proves the native G1 VM syntax-smoke sub-AC only. It does not claim CI or
publication completion, normal service enablement, or G2–G17 parity.
