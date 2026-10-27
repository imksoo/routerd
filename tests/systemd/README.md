# Isolated systemd checks

Run `python3 tests/systemd/conntrackd-stock-mask.py` as an ordinary user with a
working user systemd manager. It creates uniquely named `sleep` services only in
that user's unit directories; do not run it with sudo. It does not start
conntrackd/routerd, change network state, or prove production entry selection,
HA behavior, or forwarding.

The check first demonstrates that restarting an unprotected stock fixture stops
its conflicting managed fixture. After masking the stock fixture, it requires an
explicit mask rejection and the same active managed PID. Each command has a
15-second watchdog, not a performance acceptance threshold. No failed trial is
automatically repeated.

The script prints the retained `result.json` path before starting. Use
`--evidence-dir /path/to/new-directory` to choose it. Records include source and
dependency hashes, unit contents, timestamps, command exits/output, and cleanup
results. Exit 0 requires both product evidence and successful cleanup; exit 1 is
a direct product violation with successful cleanup; exit 2 is an observation or
cleanup problem. Inspect the original evidence before another trial. If stopping
a fixture cannot be confirmed, its unit files remain for manual recovery; only
this invocation's uniquely named units may be cleaned up. Remove detailed
evidence after review/merge according to the repository retention policy.
