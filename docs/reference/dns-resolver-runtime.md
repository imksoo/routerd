# DNS resolver runtime contracts

Local DNSZone signing is unavailable: `dnssec.enabled: true` is rejected at
configuration admission, the published schema and the worker runtime boundary.
Unsigned local answers never manufacture the authenticated-data bit. This does
not disable the resolver's separate upstream DNSSEC validation setting.

DNS over QUIC uses message ID zero, sends a request FIN before waiting for the
response, and validates the response ID and terminal FIN. The client-facing DNS
ID is retained. Its local regression uses an authenticated real QUIC peer which
waits for the request FIN; this is separate from independent-provider qualification.

Normalization does not mutate the declaration's shared slices. Negative cache
entry limits and invalid or negative TTLs are rejected rather than silently
removing the cache bounds.

`DNSResolver.spec.requestTimeout` bounds a complete lookup, including all
sequential upstream attempts. It defaults to `30s` and must be a positive Go
duration. `DNSForwarder.spec.queryTimeout` continues to limit each individual
attempt. At most 128 queries enter a worker at once; excess UDP requests are
dropped and excess TCP requests close their connection. The current rejected
count is exposed as `queryRejected`.

DNS responses use a two-second write deadline on the actual TCP connection or
UDP socket. UDP response writes have one nonwaiting socket owner so concurrent
replies cannot extend a blocked write's shared deadline. A busy UDP writer drops
the reply; clients can retry. Transport errors are recorded as `WRITE_FAILED`
when query history is enabled.

Reload retires and joins the old upstream probes and admitted requests before
acknowledging the new table. Removed listeners stop and join their handlers;
unchanged listener addresses retain their sockets. An interrupted retirement
clears the applied digest, so a same-digest retry must restore a live owner.
Stopped pools cannot accept a later query. These local loopback/process contracts
do not establish native resolver deployment or independent-provider behavior.

DHCP-derived DNS records belong to a complete named source such as
`DHCPv4Server/lan`, not just an address or a displayed status. The dnsmasq callback
passes its actual range tags and authoritative expiry (or an explicit infinite
lease) through the existing durable queue and event journal. Startup callbacks
without tags can use a unique current configured allocation scope. Missing or
ambiguous provenance remains a history-only event and cannot publish a live DNS
lease. Renewals are enabled with dnsmasq's `script-on-renewal` option.

A finite lease caps both forward and PTR TTLs at its remaining lifetime. Removal
and hostname withdrawal remove both records. Reload retains the latest event for
each source/address, including removal; reading an unchanged old lease file does
not turn that removed lease into a new observation. These tombstones survive
table reload only; full DNS-process restart does not yet restore named removal
state from the durable DHCP journal. A stale, unexpired file row at cold start
therefore remains an open recovery limitation. Static DNS records and
explicit configured reservations remain separate assignment intent. Sticky host
snapshots require a matching, still-held row from the actual sticky database and
retain its hold deadline; unqualified hostname/address sidecar text cannot grant
an infinite live lease.

Shared dnsmasq files recover only rows attributable to one current allocation.
IPv6 rows use the client DUID in their fifth field, not the IAID in the second.
Missing/malformed required lease files report an error before replacing the
active DNS table. Attribution for dnsmasq constructor pools derived from a live
delegated prefix remains unqualified at this boundary; tagged live IPv6 callbacks
continue to carry their named source. The worker does not infer native PD
ownership from display fields to recover those rows.

Protocol references: the upstream [callback documentation](https://thekelleys.org.uk/dnsmasq/docs/dnsmasq-man.html)
(`--dhcp-script`, `--script-on-renewal`), the maintainer's
[IPv4/IPv6 lease-file description](https://lists.thekelleys.org.uk/pipermail/dnsmasq-discuss/2013q3/007544.html),
and [explicit infinite lease semantics](https://lists.thekelleys.org.uk/pipermail/dnsmasq-discuss/2016q4/011019.html).
