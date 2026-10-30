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
