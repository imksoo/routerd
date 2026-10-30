# Router advertisements in routerd 1.5

The routerd RA service owns ICMPv6 advertisements and Router Solicitation replies.
dnsmasq supplies DHCP and no longer emits RA. `DHCPv6Server.mode: ra-only` does
not start a DHCPv6 listener. Other DHCPv6 modes retain their DHCP service without
creating a second RA source.

An explicit `IPv6RouterAdvertisement` controls its interface's complete RA
header and selected prefixes; an implicit DHCPv6 advertisement is used only
where no explicit one exists. Conflicting interface-wide flags withdraw that
interface's previously owned advertisements and report an error. Unrelated
interfaces continue.

`routerLifetime` is separate from prefix preferred/valid lifetime. Its default is
1800 seconds; valid configured values are `0` or `4`–`9000` seconds. Zero removes
the default-router claim while prefix advertisement may continue. Preferred and
valid prefix limits accept seconds or `infinite`; preferred cannot exceed valid.
Static prefixes default to 12 hours. Delegated prefixes inherit their genuine
absolute upstream deadlines and are never extended to that static default.

Each transmission recomputes the remaining preferred/valid lifetime from the
canonical PD lease and acknowledged LAN native receipt. Current and still-valid
previous prefixes can coexist; previous prefixes are deprecated with preferred
lifetime zero. Native addresses unrelated to that ownership are not advertised.
The service preserves independent Managed/Other flags, MTU/path-MTU selection,
router preference and DNS option lifetimes.

A newly added delegated address may be tentative while the kernel performs DAD.
The RA result remains Pending and sends no positive delegated prefix or default
router claim until the LAN address owner reports readiness. This normal wait
does not fail configuration apply. Failed DAD and unknown native ownership still
report errors and cannot authorize an advertisement.

Normal unsolicited scheduling is randomized, with at least three seconds between
multicast sends and a maximum of sixteen seconds (shorter for a short router
lifetime). Solicitation replies share the owner and its pacing. Final withdrawal
retains exact prefix and DNS records until transmission is acknowledged. A
recorded VXLAN parent must retain its actual cookie, scope and interface index;
declaration removal cannot redirect withdrawal onto a foreign link.

The solicitation receiver binds the wildcard IPv6 address and joins the selected
interface's all-routers group. A receiver bound to its transmit link-local
address can exclude multicast solicitations on FreeBSD. Transmissions still use
an explicitly selected IPv6 link-local source and interface index. Portable source-selection tests and the explicit actual-socket solicitation
gate pass on isolated Linux and FreeBSD 14.3. This does not qualify fragmented-RS
provenance or all native client paths.

Local wire/owner tests cover finite deadlines, previous-prefix withdrawal,
M/O/MTU/router lifetime, solicitation scheduling, source failures and stop/join.
The isolated Linux Kea/SLAAC client gate now covers delegated-prefix forwarding,
renewal, service restart and expiry; see the reproducible
[VM procedure](../operations/development.md#linux-pdra-lifecycle-vm-gate).
FreeBSD client forwarding remains a separate unqualified gate.
The current ndp receive API does not expose fragmented-RS provenance required
for complete RFC 6980 handling; that receive boundary remains an implementation
item. The receive API returns ICMPv6 payload/control data, without the IPv6
fragment header or reassembly provenance; a decoded solicitation is not proof
that its original packet was unfragmented.

DHCPv6 `dnsSource: self` can select a custom IPv6 `SelfAddressPolicy` for its
implicit advertisement. Candidates are tried in declaration order. Static
candidates use the configured address; interface candidates read current usable,
non-temporary global/ULA addresses and their native valid lifetimes; delegated
candidates require a current canonical PD association and an acknowledged native
address receipt for the exact derived address. A different delegated interface
also requires a current matching interface index, kernel scope and virtual-parent
cookie. An unresolved DAD/native ownership result cannot authorize self DNS.
A candidate's absence or expiry permits the next candidate; a failed required
observation stops that transmission instead of treating the failed read as absence.

Each selected DNS address is advertised for at most its own remaining validity
and the advertisement's DNS lifetime. Selection changes and exhausted candidates
withdraw the previous DNS address without withdrawing unrelated usable prefixes.
If no candidate is available the DHCPv6 RA-only status reports `SelfDNSNotReady`.
Without a custom policy the delegated self address uses this same source/receipt
path. Interface ordinals retain the existing one-based lexical address ordering;
`matchSuffix` retains the existing selector's nonzero-byte matching semantics.
This controls the RA service; dnsmasq's separately rendered DHCP DNS selection is
not qualified by these RA wire tests.

Some FreeBSD 14.3 ifconfig builds report zero for an individual infinite lifetime;
the owner retains uncertainty instead of guessing infinity from that output.
That native lifetime observation still requires closure and target qualification.

The passive RA observer retains recent diagnostic history for ten minutes and
at most 64 router identities. It bounds each retained observation before placing
it in the status response; excessive options are omitted from diagnostics without
rejecting an otherwise usable advertisement. Status reports expiry, eviction and
truncation counters. This history is not forwarding or prefix authority.
