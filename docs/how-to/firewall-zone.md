# Define firewall zones

Use `FirewallZone` to map interfaces to a policy role.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: wan
  spec:
    role: untrust
    interfaces:
      - Interface/wan
      - DSLiteTunnel/ds-lite

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: lan
  spec:
    role: trust
    interfaces:
      - Interface/lan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: management
  spec:
    role: mgmt
    interfaces:
      - Interface/mgmt
```

`untrust` is for WAN-facing paths. `trust` is for normal LAN segments.
`mgmt` is for the management network. The role matrix supplies the default
behavior, so a minimal home router usually needs zones and no broad policy
rules.
