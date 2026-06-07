---
title: Glossary
sidebar_label: Glossary
sidebar_position: 1
---

# Glossary

![Diagram mapping routerd glossary terms across declarative resources, runtime evidence, host artifacts, and networking behavior](/img/diagrams/concept-glossary.png)

Key terms used throughout the routerd documentation.

## Networking terms

| Term | Meaning |
| --- | --- |
| interface | A network interface on the host (physical NIC, VLAN, tunnel, etc.). |
| route / routing | A forwarding entry and the act of selecting it. |
| gateway | The next-hop router used to leave a network. |
| NAT | Network address translation. |
| NAPT | Dynamic many-to-one translation (port-overloaded NAT). |
| firewall | routerd's zone-based stateful filtering feature. |
| filter / rule | An individual allow or deny rule. |
| prefix delegation (PD) | DHCPv6 prefix delegation; a delegated IPv6 prefix the router redistributes to the LAN. |
| upstream | The provider side of DNS or routing. |
| egress / ingress | Outbound (egress) and inbound (ingress) traffic directions. |

## Declarative model terms

| Term | Meaning |
| --- | --- |
| declarative | Describing the desired state rather than the steps to reach it. |
| resource | A typed object in the router configuration. |
| Kind | The type of a resource. |
| spec | The desired state of a resource. |
| status | The observed, actual state of a resource. |
| apply | Bringing the host toward the desired state (`routerctl apply`). |
| reconcile / reconcile loop | The process that drives the actual state toward the desired state. |
| controller | The component that reconciles a class of resources. |
| render | Building host artifacts (config files, units) from resources. |
| daemon | A long-lived helper process managed by routerd. |
| generation | The SQLite-backed configuration generation number. |
| owned / ownership | The record of which artifacts routerd manages. |
| bootstrap | Preparing a host so routerd can run on it. |
| appliance | A fixed-function deployment of routerd. |
| Tier (H/S/C/E) | Feature stage labels used as proper nouns. |

## Other notations

- **Web Console** is routerd's read-only web UI. `WebConsole` (the Kind that enables it) is a code identifier and is kept verbatim.
