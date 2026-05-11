# Phase 3.5 FreeBSD parity smoke

Date: 2026-05-11

Scope: reboot router04, confirm FreeBSD pf/routerd convergence, run short traffic smoke, and add a small firewall semantic smoke script.

## Reboot convergence

router04:

```text
routerd before reboot: v20260511.1240
status before reboot: phase=Healthy generation=1129 resourceCount=74
reboot to management SSH + routerctl Healthy: 46 seconds
status after reboot: phase=Healthy generation=1129 resourceCount=74
```

pf after reboot:

```text
pf enabled
current states observed: 148 before traffic sample
rules include:
  scrub in on vtnet1 proto tcp all max-mss 1414
  scrub out on gif41/gif42/gif43/gif44/ppp-flets proto tcp all max-mss 1414
  block drop all
  pass quick on lo0
  pass out quick all
  pass quick inet6 proto ipv6-icmp
  routerd ClientPolicy labels
  routerd LAN/MGMT/WAN matrix labels
```

## Regression found

After reboot, routerd reported `EgressRoutePolicy/ipv4-default` as selected:

```text
selectedCandidate=ds-lite-a selectedDevice=gif41
```

However the kernel IPv4 default route was missing. The status store still held an old `IPv4Route/default` error from the early boot window when `gif41` did not exist:

```text
route -n change default -interface gif41
route: interface 'gif41' does not exist
```

The route controller skipped reapplying unchanged routes because desired status had not changed. This is wrong for volatile kernel routes after reboot.

## Fix

`IPv4RouteController` now reapplies host routes every reconcile cycle. It still records `changed=false` when the desired route status is unchanged, so event churn remains controlled while volatile kernel state is restored.

Router04 validation with the fixed FreeBSD binary:

```text
sudo route -n delete default
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
sudo service routerd restart
routerctl status: phase=Healthy generation=1133 resourceCount=74
default route: default link#10 US gif41
curl -4 https://www.google.com/generate_204: 204 local=192.0.0.2
```

## Firewall smoke

Added `scripts/firewall-parity-smoke.sh`. The script checks the semantic anchors common to Linux nftables and FreeBSD pf:

- default drop
- established/related accept
- loopback/self safety
- IPv6 ICMP allowance
- routerd role/client-policy labels

It was run against live router04 pf output and a minimal nftables fixture:

```text
ok linux
ok freebsd
```

## Caveat

The script is a semantic smoke test, not a full formal equivalence checker. It is intentionally small so it can run in CI without mutating host firewall state.
