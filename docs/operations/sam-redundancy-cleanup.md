---
title: SAM redundancy cleanup runbook
sidebar_label: SAM redundancy cleanup
---

# SAM Redundancy Cleanup Runbook

Use this runbook after a Cloud-SAM dynamic RR/leaf enrollment live test has
already passed and its validation evidence has been archived. The goal is to
remove lab resources without losing enough evidence to prove what existed
before cleanup, what was removed, and what remained afterward.

This procedure is intentionally evidence-first. Do not delete VMs, bridges,
seed ISOs, state directories, or test keys until the archive tarball and
pre-cleanup snapshot exist.

## Scope

The runbook covers a redundant PVE Cloud-SAM test shape:

- two route reflectors;
- two or more enrolled leaves;
- optional test clients attached to lab bridges;
- FOU or other SAM tunnel transports;
- RR-side `BGPDynamicPeer` dynamic BGP admission;
- leaf-side `SAMEnrollmentClient` claim submit and RRSet fetch.

The concrete 2026-06-29 validation used VMIDs `9601-9608`, bridges
`rsam999`, `rsam998`, and `rsamclnt`, and seed ISOs matching
`routerd-samred-*-cidata.iso`. Treat those names as defaults for that evidence
set, not as constants for every future lab.

## Inputs

Set these variables before starting cleanup:

```sh
RUN_ID=routerd-samred-20260629T035652Z
FREEZE_COMMIT=4a6dad6b8786ed01e63381dcf77230467b8a5021
SRC=/tmp/$RUN_ID
DST=/home/imksoo/routerd-labs-archive/evidence/samred-20260629T035652Z

PVE_NODES="pve05 pve06 pve07"
VMIDS="9601 9602 9603 9604 9605 9606 9607 9608"
BRIDGES="rsam999 rsam998 rsamclnt"
ISO_GLOB="/mnt/pve/qnap/template/iso/routerd-samred-*-cidata.iso"
```

For a new run, change `RUN_ID`, `DST`, VMIDs, bridge names, node list, and ISO
glob to match the reviewed topology. Keep the run ID stable across validation,
archive, cleanup, and manifest updates.

## Preconditions

Before cleanup, verify:

- the validation archive tarball exists in `$DST`;
- the validation archive checksum file exists in `$DST`;
- `sha256sum -c` passes for the validation archive;
- the repository manifest records the freeze commit, archive path, checksum,
  and live validation result;
- cleanup has an operator-approved target list for VMs, bridges, seed ISOs,
  temporary keys, and local state directories.

Do not use broad wildcard deletes except for reviewed test-only artifacts such
as the run-specific seed ISO glob.

## Archive Validation Evidence

If the validation archive has not already been frozen, create it first:

```sh
set -euo pipefail

test -d "$SRC"
mkdir -p "$DST"

tar -C /tmp -czf "/tmp/${RUN_ID}.tar.gz" "$RUN_ID"
sha256sum "/tmp/${RUN_ID}.tar.gz" | tee "/tmp/${RUN_ID}.tar.gz.sha256"

cp "/tmp/${RUN_ID}.tar.gz" "$DST/"
cp "/tmp/${RUN_ID}.tar.gz.sha256" "$DST/"

(
  cd "$DST"
  sha256sum -c "${RUN_ID}.tar.gz.sha256"
)
```

Record the checksum result before moving on.

## Pre-Cleanup Snapshot

Capture host-local state before deleting anything:

```sh
set -euo pipefail

mkdir -p "$DST/pre-cleanup"

ip addr > "$DST/pre-cleanup/ip-addr.txt"
ip route > "$DST/pre-cleanup/ip-route.txt"
ip tunnel show > "$DST/pre-cleanup/ip-tunnel-show.txt"
ip fou show > "$DST/pre-cleanup/ip-fou-show.txt" 2>&1 || true
ss -lntup > "$DST/pre-cleanup/ss-lntup.txt"
```

If `routerctl` can still reach the lab instances, also capture routerd state
from each RR and leaf:

```sh
routerctl status > "$DST/pre-cleanup/routerctl-status.txt" 2>&1 || true
routerctl dynamic list -o yaml > "$DST/pre-cleanup/routerctl-dynamic-list.yaml" 2>&1 || true
routerctl dynamic render -o yaml > "$DST/pre-cleanup/routerctl-dynamic-render.yaml" 2>&1 || true
```

For multi-node PVE cleanup, capture per-node inventory before deletion:

```sh
mkdir -p "$DST/pre-cleanup/pve"
for node in $PVE_NODES; do
  ssh "$node" "hostname; qm list; ip -br link; ls -1 $ISO_GLOB 2>/dev/null || true" \
    > "$DST/pre-cleanup/pve/${node}.txt" 2>&1 || true
done
```

## Cleanup Session Log

Run the destructive phase inside a session log:

```sh
mkdir -p "$DST/cleanup"
script -a "$DST/cleanup/cleanup-session.log"
```

Inside the `script` session, remove only reviewed resources:

```sh
set -euo pipefail

for node in $PVE_NODES; do
  for vmid in $VMIDS; do
    ssh "$node" "qm status $vmid >/dev/null 2>&1 && qm stop $vmid --skiplock 1 || true"
    ssh "$node" "qm status $vmid >/dev/null 2>&1 && qm destroy $vmid --purge 1 --destroy-unreferenced-disks 1 || true"
  done
done

for node in $PVE_NODES; do
  for bridge in $BRIDGES; do
    ssh "$node" "ip link show $bridge >/dev/null 2>&1 && ip link set $bridge down || true"
    ssh "$node" "ip link show $bridge >/dev/null 2>&1 && ip link delete $bridge type bridge || true"
  done
done

for node in $PVE_NODES; do
  ssh "$node" "rm -f $ISO_GLOB"
done
```

Exit the session with `exit` or `Ctrl-D`. Keep the resulting
`cleanup-session.log` in the evidence archive.

If the lab created additional local state, delete it only after it has been
listed in the session log. Examples include run-specific state DB copies,
temporary SSH keys, generated cloud-init snippets, and packet captures already
copied into `$DST`.

## Post-Cleanup Snapshot

Capture the same local state after cleanup:

```sh
set -euo pipefail

mkdir -p "$DST/post-cleanup"

ip addr > "$DST/post-cleanup/ip-addr.txt"
ip route > "$DST/post-cleanup/ip-route.txt"
ip tunnel show > "$DST/post-cleanup/ip-tunnel-show.txt"
ip fou show > "$DST/post-cleanup/ip-fou-show.txt" 2>&1 || true
ss -lntup > "$DST/post-cleanup/ss-lntup.txt"
```

Capture per-node post-cleanup inventory:

```sh
mkdir -p "$DST/post-cleanup/pve"
for node in $PVE_NODES; do
  ssh "$node" "hostname; qm list; ip -br link; ls -1 $ISO_GLOB 2>/dev/null || true" \
    > "$DST/post-cleanup/pve/${node}.txt" 2>&1 || true
done
```

Then assert that reviewed resources are absent:

```sh
mkdir -p "$DST/post-cleanup/assertions"
for node in $PVE_NODES; do
  {
    echo "node=$node"
    for vmid in $VMIDS; do
      ssh "$node" "qm status $vmid >/dev/null 2>&1" \
        && echo "FAIL vmid-present $vmid" \
        || echo "PASS vmid-absent $vmid"
    done
    for bridge in $BRIDGES; do
      ssh "$node" "ip link show $bridge >/dev/null 2>&1" \
        && echo "FAIL bridge-present $bridge" \
        || echo "PASS bridge-absent $bridge"
    done
    ssh "$node" "ls -1 $ISO_GLOB 2>/dev/null" \
      && echo "FAIL seed-iso-present" \
      || echo "PASS seed-iso-absent"
  } > "$DST/post-cleanup/assertions/${node}.txt" 2>&1
done
```

Review the assertion files. Any `FAIL` line means the cleanup is incomplete
and the session log must include the follow-up removal.

## Cleanup Evidence Archive

After post-cleanup assertions pass, create a cleanup evidence tarball:

```sh
set -euo pipefail

(
  cd "$DST"
  find pre-cleanup cleanup post-cleanup -type f -print0 \
    | sort -z \
    | xargs -0 sha256sum \
    > CLEANUP-EVIDENCE-SHA256SUMS.txt

  tar -czf "${RUN_ID}-cleanup-evidence.tar.gz" \
    pre-cleanup cleanup post-cleanup CLEANUP-EVIDENCE-SHA256SUMS.txt

  sha256sum "${RUN_ID}-cleanup-evidence.tar.gz" \
    | tee "${RUN_ID}-cleanup-evidence.tar.gz.sha256"

  sha256sum -c "${RUN_ID}-cleanup-evidence.tar.gz.sha256"
  sha256sum -c CLEANUP-EVIDENCE-SHA256SUMS.txt
)
```

The cleanup archive is the durable evidence that the test lifecycle reached:

```text
live validation
-> evidence archive freeze
-> cleanup pre-snapshot
-> cleanup execution log
-> cleanup post-snapshot
-> cleanup checksum verification
```

## Repository Manifest Update

Do not commit the evidence tarballs to the repository. Commit only the manifest
and runbook references:

- validation freeze commit;
- validation archive path and checksum;
- cleanup archive path and checksum;
- `sha256sum -c` results;
- post-cleanup assertions;
- cleanup status `complete`.

For the 2026-06-29 run, this is recorded in
`docs/releases/manifests/v20260629.samred.yaml`.

## Acceptance Checklist

Cleanup is complete when all of these are true:

- validation archive tarball exists outside the repository;
- validation archive checksum verification passes;
- pre-cleanup snapshot exists;
- cleanup session log exists;
- post-cleanup snapshot exists;
- reviewed VMs are absent on every PVE node;
- reviewed bridges are absent on every PVE node;
- reviewed seed ISOs are absent on every PVE node;
- cleanup evidence file manifest verification passes;
- cleanup evidence tarball verification passes;
- repository docs record cleanup status `complete`.
