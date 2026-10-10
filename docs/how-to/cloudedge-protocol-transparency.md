# CloudEdge Protocol Transparency Acceptance

![Diagram showing CloudEdge protocol transparency probes for FTP, NFS, bulk transfer, PMTU, source preservation, and no-NAT evidence](/img/diagrams/how-to-cloudedge-protocol-transparency.png)

This is the no-cloud harness plan for validating that CloudEdge mobility is
transparent to connection-oriented protocols that are sensitive to NAT, helper
ALGs, dynamic ports, and MTU/PMTU behavior. The live run is performed later by a
lab operator; this document and the scripts under `scripts/` only prepare the
contract and evidence shape.

## Goal

For traffic over the logical shared subnet (`10.77.60.0/24` in the demo), prove:

- No NAT: the server sees the client site's mobility `/32` as the peer address.
- The client default gateway is unchanged from the local site.
- FTP active mode and passive mode both transfer data without NAT ALG.
- RPC endpoint discovery through `rpcbind` and an NFSv3 mount/read/write work
  across sites.
- Large transfers complete without PMTU black holes.
- MSS/PMTU evidence records the overlay MTU, route MTU/advmss when available,
  and the configured MSS clamp value.

## Minimal Live Matrix

Run the protocol probe after the normal D3 directed matrix is already green.
Use two representative pairs:

| Pair | Why |
| --- | --- |
| `aws -> azure` | cloud-to-cloud overlay path with cloud provider trapping on both ends |
| `aws -> onprem` | cloud-to-onprem path with on-prem proxy-ARP/VRRP authority |

The scenario catalog encodes this as `d11-protocol-transparency` in
`examples/cloudedge-acceptance-scenarios.json`.

Larger parity runs may add `azure -> oci`, `oci -> aws`, or reverse directions,
but the minimal acceptance should stay small enough to run inside one 4-site lab
window.

## Harness

The wrapper is:

```sh
PROTOCOL_PROBE_RUNNER=scripts/runners/cloudedge-protocol-runner.sh \
  scripts/cloudedge-protocol-probe.sh \
    --pairs aws:azure,aws:onprem \
    --bytes 104857600 \
    --out evidence/protocol-probe.json
```

The full acceptance scenario uses the same wrapper through:

```sh
PROTOCOL_PROBE_RUNNER=scripts/runners/cloudedge-protocol-runner.sh \
MATRIX_RUNNER=scripts/runners/cloudedge-matrix-runner.sh \
scripts/cloudedge-acceptance.sh run \
  --scenario d11-protocol-transparency \
  --out evidence/d11-protocol \
  --commit <routerd-commit>
```

The output is validated by `scripts/cloudedge-protocol-result-schema.json` and is
folded into `result.json` under the `protocols` object.

## Runner Contract

`scripts/runners/cloudedge-protocol-runner.sh` implements
`PROTOCOL_PROBE_RUNNER`. It is intentionally parameterized by environment
variables and contains no provider account IDs, resource IDs, or secrets.

Required per site:

```sh
export CE_AWS_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export AWS_CLIENT_IP=10.77.60.11
export CE_AZURE_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export AZURE_CLIENT_IP=10.77.60.12
export CE_ONPREM_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export ONPREM_CLIENT_IP=10.77.60.10
export SSH_KEY_FILE=<private-key>
export SSH_USER=ubuntu
export CLIENT_SSH_USER=ubuntu
```

Useful protocol variables:

```sh
export CE_PROTOCOL_INSTALL=1
export CE_PROTOCOL_CONFIGURE_SERVICES=1
export CE_PROTOCOL_FTP_PASSIVE_PORTS=40000:40100
export CE_PROTOCOL_BULK_BYTES=104857600
export CE_PROTOCOL_PMTU_SIZE=1300
export CE_PROTOCOL_OVERLAY_IFACE=wg-hybrid
export CE_PROTOCOL_MSS_CLAMP=1340
```

Each operation can be overridden without editing the runner:

```sh
export CE_PROTOCOL_FTP_ACTIVE_COMMAND='...'
export CE_PROTOCOL_NFS_COMMAND='...'
```

The wrapper calls these operations per pair:

| Operation | Assertion |
| --- | --- |
| `setup` | Installs/configures `vsftpd`, `rpcbind`, NFS server/client tools, and `iperf3` when enabled |
| `ftp-active` | FTP `PORT` mode data channel completes |
| `ftp-passive` | FTP passive mode data channel completes |
| `nfs` | NFSv3 mount plus write/read of the requested byte budget completes |
| `rpc` | `rpcinfo -p` finds `rpcbind` and at least one dynamic RPC/NFS port |
| `bulk` | `iperf3 -n <bytes>` completes and records throughput/retransmits |
| `pmtu` | DF ping succeeds and records overlay MTU, route MTU/advmss, MSS clamp |
| `source-preserved` | Server-side SSH sees the client mobility `/32` as peer IP |
| `no-nat` | Same peer-IP check, recorded as an explicit no-NAT assertion |

## Forcefrag / MSS Comparison

The normal run should pass with the routerd-derived MSS clamp. If a lab needs to
prove P2-b force fragmentation behavior, run the same D11 pair set twice:

1. `forceFragmentIPv4: false` (default): TCP transfers should pass through MSS
   clamp; oversized DF non-TCP may fail depending on underlay PMTU.
2. `forceFragmentIPv4: true` on the relevant `OverlayPeer` or `TunnelInterface`:
   the same DF probe should pass and `routerd_forcefrag` should appear in
   router evidence.

Do not enable force fragmentation globally. Keep it path-scoped and record the
before/after config digest in the evidence bundle.

## Evidence Review Checklist

For each pair in `protocol-probe.json`:

- `checks.ftpActive`, `ftpPassive`, `nfs`, `rpc`, `bulkTransfer`, `pmtu`,
  `sourceIpPreserved`, and `noNat` are all `pass`.
- `details.sourceIpPreserved.peer_ip` equals the client site's mobility `/32`.
- `details.rpc.dynamic_port` is present and not `111`.
- `details.bulkTransfer.retransmits` is recorded when `iperf3` is available.
- `details.pmtu.overlay_mtu`, `route_mtu` or `route_advmss`, and `mss_clamp`
  are recorded.

The outer `result.json` must include passing assertions:

- `protocol_transparency`
- `ftp_active_passive`
- `nfs_rpc`
- `bulk_transfer_pmtu`
- `protocol_source_ip_preserved`
- `protocol_no_nat`

The SAM E2E harness checks the exact read and written content for FTP, NFS and CIFS. Its failover transfer uses a known 64 MiB payload and requires both the expected size and SHA-256 after download. Before injecting a fault, it confirms that the current transfer job is running with partial data; absent progress stops the attempt as inconclusive without injecting or retrying the fault. Entry, result and failed-attempt evidence are retained. Optional transfer observations and performance values remain separate from the normal post-failover matrix verdict. Deploy copies use a fresh directory, propagate every required copy/setup failure, and compare the installed binaries and configuration with this attempt's payload before restarting services.

L2 qualification needs exactly the before and after observations from one provider and threshold configuration, including the suppression mechanism. A successful before acquisition permits the next orchestration step, while the saved aggregate remains inconclusive until after; after returns the aggregate verdict. Failover durations use a monotonic clock, with UTC recorded separately for journal correlation. Provider VM stop implementations handle `stop-active`; `drain` requires an explicit `CE_<PROVIDER>_DRAIN_INJECT_COMMAND` or `CE_DRAIN_INJECT_COMMAND`, and on-premises stop requires an explicit node-stop command. Stopping keepalived does not establish a node-stop qualification.

## Federation qualification evidence

`scripts/cloudedge-federation-qualification.sh` requires positive cycles and all eight scenarios for release qualification. `--allow-skip` permits development subsets; their results never qualify a release. Each attempt uses a fresh evidence directory and event IDs, keeps command stdout/stderr/exit status and stops at the existing `--duration` budget per worker (including cleanup). A failure, incomplete observation or unconfirmed cleanup stops later faults and cycles without retries; retained fault markers require operator review before another attempt.

TTL refresh compares the current receiver event with the sender's extended `ExpiresAt`; an old delivered row or absent `staleTTL` is insufficient. Subscription failure/recovery uses this attempt's event ID and receiver-side post-injection time. Partition isolation records a healthy A/B baseline, current B delivery during the A fault, and group/peer-scoped SLO violations plus remediation with configured thresholds. Identical thresholds are allowed. Security uses correctly signed malformed bodies and a valid body with a bad signature: expected HTTP 400/401 plus receiver nonacceptance and a current valid delivery are required; transport errors and 5xx prove no rejection.

OTel availability, samples and labels are diagnostics: missing data remains inconclusive, never numeric zero or a product failure. The saved binary SHA-256 and product source commit are separate from the QA repository commit. Unknown or mismatched deployed source identity stops before scenario faults. Exit 1 records a measured assertion failure, 2 invalid input/setup, and 3 incomplete observation/provenance. These changes have offline boundary coverage; they do not establish provider or release qualification.

Development partition subsets that do not select multi-group require no B group. Go on the lab nodes is optional. `CE_BINARY_PROVENANCE_FILE` can point to an existing prepared/release contract: `routerdArtifact.commit` and `execution.candidate_binary_hashes` are bound to the current binary SHA-256. Conflicting runtime commit labels or mismatched hashes remain unconfirmed; a commit argument alone never establishes identity.
