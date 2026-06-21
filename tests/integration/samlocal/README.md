# SAM Local Integration Harness

This package is the first local integration slice for issue #622. It is
intentionally small:

- start and stop N isolated OS child processes under `go test`
- provide a fake provider API for secondary-IP assign/unassign
- model provider rejection contracts that unit tests usually miss
- exercise capture distribution, failover takeover, no-preempt rejoin, and forced rebalance

The first process test uses the Go test binary as a helper process. The harness
shape is meant to be replaced by real `routerd` / `routerd-bgp` binaries in the
next slice, while keeping the same lifecycle and bounded wait patterns.

Current fake provider coverage:

- AWS-style `move not allowed` rejection
- Azure `PrivateIPAddressIsAllocated` rejection
- OCI `PrivateIpAlreadyAssigned` rejection
- secondary-IP limit rejection
- idempotent same-NIC assign/unassign

Next steps:

1. Build and start per-node `routerd-bgp` processes with isolated runtime dirs.
2. Add an RR + leaf topology builder and BGP convergence polling.
3. Feed realistic SAM inventory/action state into real routerd processes.
4. Replace direct fake-provider calls with executor-plugin calls against this API.
