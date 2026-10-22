# DHCP lease synchronization content checks

Lease synchronization adds `--checksum` to the default rsync arguments. A
successful transfer must not leave old contents merely because the source and
destination have equal sizes and modification times. This applies to lease
files, the sticky lease database and its WAL, and prefix-delegation state.
Both ends read matching-size files to compare their contents, so this adds
read I/O. User-supplied rsync options retain their existing ordering after the
defaults; options that disable content comparison can weaken this behavior.

One failure path is a SQLite checkpoint between the database copy and the WAL
copy. The WAL can disappear when the last writer closes. That transfer still
reports `SyncFailed`; the next reconciliation inventories the files again and
copies the checkpointed database using content comparison. Missing WAL is not
ignored or removed from the configured transfer set.

This does not create an atomic SQLite snapshot. The database and WAL still
transfer separately, and arbitrary concurrent writes or a role transition
during transfer require separate consistency analysis. A recovery log confirms
the command result; it does not independently verify every consumer.

Regression tests use the actual generated arguments with only remote transport
and destination replaced by local temporary paths. They reproduce unchanged
size/mtime with different contents, and a real SQLite writer closing before
the WAL transfer. They verify receiver contents and integrity after retry.
No host networking or production service is modified.
