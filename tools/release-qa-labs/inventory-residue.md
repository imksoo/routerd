# Inventory residue and terminated instance records

The seven required inventory scope names are unchanged. For the AWS and OCI
tagged-resource scopes, `count` is actionable run-owned residue, not the raw
number of entries in a tagging/search index. Those indexes can retain records
for terminated instances. A zero inventory remains a cleanup result, not an
E2E or fault-test pass.

`inventory-driver.sh` saves provider responses and invokes the read-only
`inventory_resources.py` classifier. It does not delete resources, remove tags,
repair cloud state, or accept a missing instance as proof of termination.

## Required corroboration

- AWS: every tagged EC2 instance ARN is resolved by an exact-ID
  `describe-instances` query. The ID, ARN region/account, reservation owner and
  exact run tag must agree. Only the known `terminated` state is excluded.
  Absence from the separate active-instance query is not sufficient.
- OCI: each search result of type `Instance` must have the same identifier,
  compartment, exact run tag and known lifecycle state in the complete compute
  instance list. Only a matching `TERMINATED` pair is excluded.
- Stopped, stopping and all other known nonterminal instances still count.
  Non-instance and unknown resource types are never exempted by a lifecycle
  label. Live instances found by the provider's instance query but absent from
  its tagging/search index also count.
- Missing corroboration, mismatched identities or states, duplicate records,
  unknown instance lifecycle states, malformed responses, incomplete pagination
  and provider-command errors fail closed. A later retry must obtain fresh
  authoritative evidence; `NotFound` is not a zero-count shortcut.

## Retained evidence

`aws-resource-counts.json` and `oci-resource-counts.json` separately record
`rawTaggedCount`, `confirmedTerminatedCount`, `activeInstanceCount` and `count`.
The terminated count refers only to corroborated tagged/search records; it is
not a count of every historical instance in the account. `count` can exceed
`rawTaggedCount` when the independent instance query finds additional live
residue.

The AWS exact-ID response is saved as `aws-tagged-instance-states.json` alongside
the original active and tagged responses. OCI retains its compute response in
`oci-instances.raw.json`; the existing CLI compatibility rule normalizes only a
successful, strictly empty stdout stream to `{"data":[]}` for classification.
Whitespace or malformed JSON is not normalized.

Each OCI search attempt keeps its raw pages in a separate directory beneath
`oci-tagged-resource-pages/`. Aggregation uses only that attempt's explicit page
list. A retry with fewer pages cannot incorporate stale pages, and earlier raw
pages remain available. Transport stderr is retained beside the corresponding
provider response. Treat these files as private evidence; do not publish raw
account, resource, tag or infrastructure identifiers.

The fixture tests cover corroborated tombstones, stopped instances, missing or
conflicting observations, later-page residue, partial/error responses and a
two-page to one-page retry. These local tests do not establish live-cloud
cleanup or qualification success. Deploy the driver and its classifier together
through a newly reviewed and pinned run; do not modify a sealed active run.
