# Provider Contract Harness

The provider contract harness is an intermediate test layer between pure planner
unit tests and live AWS/Azure/OCI/PVE qualification.

It models provider-side state, not guest OS state:

- secondary IP ownership is keyed by provider target references such as ENI,
  NIC, or VNIC identifiers;
- route-table steering is keyed by route table and next-hop references;
- forwarding is a provider capability flag on a target reference;
- inventory snapshots report provider observations with snapshot generations and
  completion timestamps.

The harness intentionally does not model secondary IPs as `ip addr` entries in a
Linux namespace. That boundary is the main difference from the retired netns
approach: provider API state and guest dataplane state stay separate.

Current coverage:

- normal `assign-secondary-ip` fails non-destructively when another target holds
  the address;
- explicit seize requires `allowReassignment=true`;
- explicit seize can be fenced with `expectedHolderRef`;
- route-table route assignment records next-hop steering;
- forwarding enablement records provider forwarding state.

This layer complements, but does not replace, live full-topology qualification.
Use it to test provider action and observation contracts without cloud
credentials, then reserve AWS/Azure/OCI/PVE runs for release qualification and
changes that alter real provider executors or dataplane behavior.
