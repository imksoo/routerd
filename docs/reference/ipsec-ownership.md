# IPsec ownership in routerd 1.5

The runtime IPsec controller uses one scoped VICI session and a durable receipt
at `<SysconfDir>/swanctl/routerd-ipsec.json`, protected by
`.routerd-ipsec.lock`. The receipt format is version 1. A connection's native
name is `routerd-<IPsecConnection resource name>`; its native name and input hash
are distinct from observed IKE/child-SA state.

The controller records pending intent before loading or unloading its connections
and shared keys. It verifies native inventories before acknowledging the current
set. Failed publication or VICI observation retains pending ownership for retry.
An existing connection with the desired name but no matching receipt is foreign
and is not adopted or unloaded. Configuration removal uses this same owner.
Both the serving runtime and one-shot apply use it; planning and dry-run do not
load native IPsec state.

PEM/DER certificates and supported private-key encodings are loaded through scoped
VICI credential commands. PKCS#12 automatic loading is unsupported; extract the
certificate and private key into the configured PEM/DER files before use. Public
certificates and content-addressed private keys can be shared with other charon
users, so routerd does not globally unload or clear these native objects.

This revision does not read old aggregate ownership or generated `.conf` files as
authority. At cutover, stop the earlier owner and resolve its existing native
connections before starting the new runtime with rewritten configuration. Moving
an old database aside does not withdraw a live connection. Keep the new receipt
until its actual owner's withdrawal is acknowledged.

Local checks cover the actual scoped VICI controller with a test-owned TCP VICI
endpoint, failure/retry and startup wiring. Linux/FreeBSD strongSwan peer
negotiation, tunnel traffic and native credential behavior require isolated target
qualification; configuration loading alone does not establish an IPsec tunnel.
