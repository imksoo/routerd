# DHCP lease synchronization consistency

Lease synchronization adds `--checksum` to the default rsync arguments. A
successful transfer must not leave old contents merely because source and
destination have equal sizes and modification times. Both ends compare file
contents, adding read I/O. User-supplied options that disable comparison can
weaken this behavior.

For DHCPv4 sticky leases, copying the live SQLite database followed by its WAL
is unsafe: a checkpoint can truncate the WAL between transfers, leaving an
incomplete replica even when both commands succeed. The controller instead
opens the source read-only and uses SQLite `VACUUM INTO` to create one consistent,
standalone snapshot including committed WAL updates. All targets receive that
snapshot at the original database path. Neither WAL nor SHM is copied separately.

The temporary directory is private (0700), the snapshot is 0600, and the attempt
removes it after success or failure. Snapshot work respects cancellation and a
bounded command deadline. Failure publishes `SnapshotFailed`, never `Synced`.
A transfer failure still publishes `SyncFailed`. Neither creates another
persistent evidence directory.

This ensures source snapshot consistency; it does not fence concurrent receiver
writers, atomically group the dnsmasq lease file with the sticky database, or
qualify HA failover. A standby should not write the replicated database while
receiving it. Those role/consumer combinations require native qualification.
DHCPv6 lease files and delegated-prefix JSON retain their existing transfer paths.

Run `go test ./pkg/controller/chain -run 'Test(LeaseSync|DHCPv4ServerLeaseSync)' -count=1`
with rsync installed. Tests retain generated options while replacing only SSH
transport and destinations with local temporary paths. They verify equal-size /
equal-mtime updates, a real checkpoint truncating the WAL after DB transfer,
writer close during transfer, receiver lease contents/integrity, original
receiver paths, shared snapshot across targets, cleanup and visible failure.
The checkpoint test fails on the previous implementation with `Synced` but no
committed lease on the receiver. No host networking is modified.

SQLite documents the [consistent snapshot semantics of VACUUM INTO](https://www.sqlite.org/lang_vacuum.html#vacuum_with_an_into_clause).
