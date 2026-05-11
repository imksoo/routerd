# Phase 3.4 A2 firewall semantic audit

Date: 2026-05-11

Scope:
- Linux nftables host: homert02 (Ubuntu).
- FreeBSD pf host: router04.
- Goal: 3-role firewall semantic alignment for untrust / trust / mgmt.

Fixes made:
- The firewall controller now uses pf on FreeBSD instead of always rendering/applying nftables.
- Firewall status records the backend (`nftables` or `pf`) and a generic `rulesetPath`.
- local/router04.yaml had an indentation error that left `--controller-chain-dry-run-firewall=false` outside ExecStart. Fixed and applied.

Linux nftables evidence (homert02):
- routerctl status: Healthy, generation 52, resourceCount 88.
- firewall controller: live.
- nft table `inet routerd_filter` exists with input/forward policy drop.
- established/related and ICMPv6 are accepted.
- lan->wan, mgmt->self, mgmt->lan, mgmt->wan are accepted by role matrix.
- wan->self/lan/management deny chains are present with counters/logging.

FreeBSD pf evidence (router04):
- routerctl status: Healthy, generation 1124, resourceCount 73.
- firewall controller: live.
- `/run/routerd/firewall.pf` exists and passes `pfctl -nf`.
- `pfctl -sr -vv` shows `block drop all`, pass out keep state, ICMPv6 pass, mgmt/self pass, lan->mgmt deny, lan->wan pass, and mgmt forwarding passes.
- pf state table active; current entries observed.

Semantic comparison:
- Both backends enforce default drop for input/forward and allow established/related behavior through stateful rules.
- Both permit trust->self and mgmt->self.
- Both deny untrust->self except explicit internal holes.
- Both deny untrust->trust and untrust->mgmt.
- Both allow trust->untrust and management-originated flows.
- FreeBSD pf has no MAC matching for ClientPolicy; that is handled in A3.

Safety:
- Before enabling router04 pf live mode, a rollback cron entry was installed to run `pfctl -d`; it was removed after SSH and status stayed healthy.
