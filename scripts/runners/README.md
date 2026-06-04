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

Protocol transparency runs use `cloudedge-protocol-runner.sh` through
`PROTOCOL_PROBE_RUNNER`. The default implementation can install/configure a
minimal lab server stack on the client VMs (`vsftpd`, `rpcbind`, NFS, `iperf3`)
and records FTP active/passive, NFS/RPC, bulk transfer, source-IP, no-NAT, and
PMTU/MSS evidence:

```sh
export CE_AWS_CLIENT_SSH_HOST=...
export AWS_CLIENT_IP=10.77.60.11
export CE_AZURE_CLIENT_SSH_HOST=...
export AZURE_CLIENT_IP=10.77.60.12
export CE_ONPREM_CLIENT_SSH_HOST=...
export ONPREM_CLIENT_IP=10.77.60.10
export CE_PROTOCOL_OVERLAY_IFACE=wg-hybrid
export CE_PROTOCOL_MSS_CLAMP=1340

PROTOCOL_PROBE_RUNNER=scripts/runners/cloudedge-protocol-runner.sh \
  scripts/cloudedge-protocol-probe.sh \
    --pairs aws:azure,aws:onprem \
    --bytes 104857600 \
    --out evidence/protocol-probe.json
```

Use `CE_PROTOCOL_<OP>_COMMAND` overrides for lab-specific FTP/NFS/RPC setup
without editing the shared runner.

The scripts support `--help` without credentials. `cloudedge-runners-offline-test.sh`
exercises the contracts with local fake commands and no cloud access.
