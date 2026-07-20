# FreeBSD VM115 G3 doctor acceptance — 2026-07-20

Commit `d40cada39bb502df4c53a1b15f88f122b6ef32fe` was exercised in the
isolated VM115 v4 syntax fixture under `/tmp/vm115-stage48-doctor.vlJSpW`.

`routerctl doctor firewall -o json` exited zero and produced native FreeBSD
diagnostics.  The fixture intentionally has no enabled host PF and no
`routerd-firewall-logger` service process.  The report therefore recorded
actionable warnings, rather than a false pass or a Linux command failure:

- `pfctl status`: warn (`exit status 1`) with a PF read-only-access remedy.
- `pfctl rules`: warn (`exit status 1`) with a read-only-rule inspection remedy.
- `routerd-firewall-logger runtime`: warn (`exit status 1`) with an rc.d/pflog
  remediation path.

The doctor command itself returned `0`; its negative fixture state is visible
in the JSON report.  The implementation uses read-only FreeBSD `route`,
`pfctl`, `pgrep`, `procstat`, `netstat`, `ifconfig`, `sysctl`, and `sockstat`
paths, while retaining Linux command paths unchanged.

The same Stage48 guest completed the retained v4 suite and halted. Postflight
is VM115 `stopped`, args absent, exact v4 ISO attachment, `vmbr404
NO-CARRIER`, retained snapshots unchanged, and no stage controller process.
