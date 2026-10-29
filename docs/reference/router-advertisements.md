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

Normal unsolicited scheduling is randomized, with at least three seconds between
multicast sends and a maximum of sixteen seconds (shorter for a short router
lifetime). Solicitation replies share the owner and its pacing. Final withdrawal
retains exact prefix and DNS records until transmission is acknowledged. A
recorded VXLAN parent must retain its actual cookie, scope and interface index;
declaration removal cannot redirect withdrawal onto a foreign link.

Local wire/owner tests cover finite deadlines, previous-prefix withdrawal,
M/O/MTU/router lifetime, solicitation scheduling, source failures and stop/join.
Independent clients and native Linux/FreeBSD forwarding are not qualified here.
The current ndp receive API does not expose fragmented-RS provenance required
for complete RFC 6980 handling; that receive boundary remains an implementation
item. Custom DHCPv6 `SelfAddressPolicy` live resolution also remains open.
Some FreeBSD 14.3 ifconfig builds report zero for an individual infinite lifetime;
the owner retains uncertainty instead of guessing infinity from that output.
That native lifetime observation still requires closure and target qualification.
