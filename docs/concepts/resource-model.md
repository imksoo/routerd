---
title: Resource model
slug: /concepts/resource-model
sidebar_position: 3
---

# Resource model

A routerd YAML file declares one `Router` resource that contains a list of
typed sub-resources. This page explains the layout and the conventions
that hold across the schema.

## The router file

A minimal router YAML looks like this:

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: home-router
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ens18
        adminUp: true
        managed: true
```

The top-level resource is always:

| Field | Meaning |
|---|---|
| `apiVersion` | `routerd.net/v1alpha1` (the routerd API version) |
| `kind` | `Router` |
| `metadata.name` | A label for this router. Pick something stable. |
| `spec.resources` | A list of sub-resources that describe the router's behavior. |

## Sub-resources

Each item in `spec.resources` is itself a resource with an `apiVersion`,
`kind`, `metadata.name`, and `spec`. The `apiVersion` indicates which
group the kind belongs to:

- `net.routerd.net/v1alpha1` — networking kinds (interfaces, addresses,
  DHCP, NAT, firewall, route policy, ...).
- `system.routerd.net/v1alpha1` — host-side kinds (sysctl, hostname,
  NTP client, log sink, NixOS host integration).
- `routerd.net/v1alpha1` — routerd's own kinds, including the observed
  `Inventory` resource.

The full kind catalog is in the [API reference](../reference/api-v1alpha1).

## Stable names

Inside the YAML you reference resources by `metadata.name`, not by the
underlying OS object. The kernel interface name `ens18` only appears once
in the `Interface` resource's `spec.ifname` field. Everywhere else, you
say `wan`, `lan`, `mgmt`, etc. This lets you swap NICs without rewriting
unrelated resources.

For example, an `IPv4DHCPAddress` does not say "use ens18". It says:

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4DHCPAddress
  metadata:
    name: wan-dhcp4
  spec:
    interface: wan
```

The `wan` here points to the `Interface` resource by its `metadata.name`.

## What is in `spec` vs `status`

routerd splits the data the way Kubernetes does:

- `spec` describes the desired state. You write this in YAML.
- `status` describes the observed state. routerd writes this to its
  SQLite database. You read it through `routerctl describe` and
  `routerctl show`.

For example, an `IPv6PrefixDelegation` has:

- `spec.interface`, `spec.prefixLength`, `spec.profile`, ... — what you
  declared.
- `status.currentPrefix`, `status.lastObservedAt`, `status.duid`,
  `status.iaid`, ... — what routerd saw on the wire.

The YAML never carries `status`. routerd populates it.

## Profiles

Some kinds use *profiles* to bundle a set of opinionated defaults. The
clearest case is `IPv6PrefixDelegation.spec.profile`, which selects a
known upstream environment (e.g. `ntt-hgw-lan-pd`) and pulls in the right
defaults for DUID type, prefix length, and other knobs. Profiles let
operators describe the upstream once instead of restating individual
fields.

Profiles never override an explicit field. If `spec.prefixLength` is set,
the profile-default does not apply. This makes it safe to say
`profile: ntt-hgw-lan-pd` and still tweak one knob without surprises.

## Where to go next

- [Apply and render](./apply-and-render) — how the YAML reaches the host.
- [State and ownership](./state-and-ownership) — how routerd remembers
  what it did.
- [API reference](../reference/api-v1alpha1) — every kind and field.
