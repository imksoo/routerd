# Add firewall exceptions

Use `FirewallRule` only for exceptions that the role matrix does not cover.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: allow-admin-ssh
  spec:
    fromZone: management
    toZone: self
    protocol: tcp
    port: 22
    action: accept
```

`fromZone` and `toZone` refer to `FirewallZone` names. `toZone: self` means the
router host itself.

Rules are evaluated before the implicit role matrix. routerd-generated internal
openings are also evaluated before user rules. This keeps DHCP, DNS, DS-Lite,
and other managed services alive when the firewall is enabled.

Use the local simulator before applying a new rule:

```sh
routerctl firewall test from=wan to=self proto=tcp dport=22
routerctl describe firewall
```
