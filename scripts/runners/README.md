# CloudEdge Acceptance Runners

These scripts are live runner implementations for the acceptance probe wrappers:

- `cloudedge-matrix-runner.sh` implements `MATRIX_RUNNER`.
- `cloudedge-failover-runner.sh` implements `FAILOVER_TIMING_RUNNER`.
- `cloudedge-protocol-runner.sh` implements `PROTOCOL_PROBE_RUNNER`.
- `cloudedge-l2-runner.sh` implements `L2_LOOP_RUNNER`.

They are parameterized by environment variables. They do not contain account IDs,
OCIDs, subscription IDs, instance IDs, interface IDs, or secrets. Lab-specific
values must come from the operator environment, for example:

```sh
export SSH_KEY_FILE=/path/to/key
export SSH_USER=ubuntu
export CLIENT_SSH_USER=ubuntu
export AWS_REGION=ap-northeast-1
export AWS_ROUTER_A_INSTANCE_ID=i-...
export AWS_ROUTER_B_SSH_HOST=...
export AWS_CLIENT_SSH_HOST=10.77.60.11
export AZURE_RESOURCE_GROUP=...
export AZURE_ROUTER_VM_NAME=...
export OCI_ROUTER_INSTANCE_REF=ocid1.instance...
export ONPREM_ROUTER_A_SSH_HOST=...
export ONPREM_ROUTER_B_SSH_HOST=...
```

`cloudedge-failover-runner.sh` intentionally observes routerd convergence and
provider action journal state. It does not call `routerctl action approve` or
`routerctl action execute`; the provider executor must be running under routerd's
normal reconcile/executor path.

For unusual labs, each observation or injection can be overridden with a local
command:

```sh
export CE_AWS_INJECT_COMMAND='aws ec2 stop-instances ...'
export CE_AWS_DETECTION_COMMAND='ssh ... sqlite3 ...'
export CE_AWS_SWITCHOVER_COMMAND='ssh ... sqlite3 ...'
export CE_AWS_RECOVERY_COMMAND='scripts/runners/cloudedge-matrix-runner.sh ping aws 10.77.60.10'
export CE_L2_METRICS_COMMAND='ssh ... collect-l2-metrics'
```

The scripts support `--help` without credentials. `cloudedge-runners-offline-test.sh`
exercises the contracts with local fake commands and no cloud access.
