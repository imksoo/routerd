# CloudEdge Acceptance Runners

These scripts are live runner implementations for the acceptance probe wrappers:

- `cloudedge-matrix-runner.sh` implements `MATRIX_RUNNER`.
- `cloudedge-failover-runner.sh` implements `FAILOVER_TIMING_RUNNER`.
- `cloudedge-protocol-runner.sh` implements `PROTOCOL_PROBE_RUNNER`.
- `cloudedge-l2-runner.sh` implements `L2_LOOP_RUNNER`.
- `cloudedge-capture-runner.sh` implements the four-point pcap harness for the
  `05-capture` evidence slot.
- `cloudedge-fabric-runner.sh` implements cloud fabric evidence collection for
  the `03-control-plane` CF slot.

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

Four-point packet capture runs use one `TEST_ID` for source endpoint,
router-inside, router-outside-tunnel, and remote endpoint captures. The runner
writes `TEST_ID-node-role-iface.pcap`, per-test JSON, and an aggregate manifest
under `poc-evidence-*/05-capture`:

```sh
scripts/runners/cloudedge-capture-runner.sh start \
  --test-id 20260605-0236-CAP-01 \
  --out poc-evidence-20260605 \
  --source-site aws \
  --remote-site azure \
  --router-provider onprem \
  --target-ip 10.77.60.9 \
  --ports 22,2049

scripts/runners/cloudedge-capture-runner.sh stop \
  --test-id 20260605-0236-CAP-01 \
  --out poc-evidence-20260605 \
  --source-site aws \
  --remote-site azure \
  --router-provider onprem \
  --target-ip 10.77.60.9 \
  --ports 22,2049
```

Use `CE_CAPTURE_<ROLE>_<START|STOP|COPY>_COMMAND` or the generic
`CE_CAPTURE_START_COMMAND`, `CE_CAPTURE_STOP_COMMAND`, and
`CE_CAPTURE_COPY_COMMAND` overrides to fake captures in offline tests or adapt a
lab without editing the shared runner.

Cloud fabric evidence runs collect provider control-plane state and normalize it
with `scripts/cloudedge-cloud-fabric-schema.json`. The runner writes provider
JSON, an aggregate manifest, a Markdown summary, and CF test-record rows under
`poc-evidence-*/03-control-plane`:

```sh
scripts/runners/cloudedge-fabric-runner.sh collect \
  --provider aws \
  --test-id 20260605-0242-CF-01 \
  --out poc-evidence-20260605 \
  --capture-address 10.88.60.9

scripts/runners/cloudedge-fabric-runner.sh collect \
  --provider azure \
  --test-id 20260605-0242-CF-01 \
  --out poc-evidence-20260605 \
  --capture-address 10.77.60.9

scripts/runners/cloudedge-fabric-runner.sh collect \
  --provider oci \
  --test-id 20260605-0242-CF-01 \
  --out poc-evidence-20260605 \
  --capture-address 10.99.60.9
```

Live runs use read-only `aws`, `az`, or `oci` CLI calls. Missing CLI,
authentication, or required target env is recorded as `NOT-RUN` with a reason.
Use `CE_FABRIC_<PROVIDER>_JSON_COMMAND` to feed deterministic fake JSON through
the same normalizer in offline tests.

The scripts support `--help` without credentials. `cloudedge-runners-offline-test.sh`
exercises the contracts with local fake commands and no cloud access.
