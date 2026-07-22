# FreeBSD KernelModule persistence

FreeBSD derives `KernelModule` resources from the router configuration.  A
derived module remains runtime-loaded through `kldstat` and `kldload`; the
persistent part is a separate boot-time concern.

On FreeBSD 14.3, routerd writes one owned loader drop-in per derived resource
under `/boot/loader.conf.d`, for example
`90-routerd-router-runtime.conf`.  The loader processes `.conf` files in that
directory during boot.  The owned file contains a stable marker followed by
`<module>_load="YES"` settings.  This keeps routerd out of
`/boot/loader.conf`, `/boot/loader.conf.local`, and administrator-owned
drop-ins.

The controller writes a changed owned file through a same-directory temporary
file, syncs it, and renames it atomically.  It refuses symlinks, non-regular
files, or a same-name file without the routerd marker.  On removal it scans
only `90-routerd-*.conf`, removes only regular files carrying that marker, and
preserves all other loader configuration.  Runtime reconciliation never
unloads a module already present before routerd runs.

The native acceptance remains responsible for proving absent-to-configured,
reboot loading, an idempotent second reconcile, removal of only the owned
drop-in, and cleanup.  No user-facing `KernelModule` resource is reintroduced:
the existing legacy-kind rejection remains intact.
