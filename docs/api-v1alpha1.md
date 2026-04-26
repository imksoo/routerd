# API v1alpha1

routerd uses Kubernetes-like API shapes:

- `apiVersion`
- `kind`
- `metadata.name`
- `spec`
- `status` where applicable

## API Groups

- `routerd.net/v1alpha1` for the top-level `Router` config
- `net.routerd.net/v1alpha1` for network resources
- `system.routerd.net/v1alpha1` for local system resources
- `plugin.routerd.net/v1alpha1` for plugin manifests

## MVP Resources

- `Interface`
- `IPv4StaticAddress`
- `IPv4DHCPAddress`
- `IPv6DHCPAddress`
- `Hostname`
- `Sysctl`

The schema is intentionally small and will be implemented incrementally.

## Interface Ownership

`Interface` resources support `spec.managed`.

- `managed: false` means routerd observes the interface and resolves aliases, but does not change link or address state.
- `managed: true` means routerd may manage the interface after existing OS networking ownership has been reviewed.

When cloud-init or netplan is detected, routerd planning reports `RequiresAdoption` instead of taking over automatically.

## IPv4 Overlap Safety

`IPv4StaticAddress` resources are checked against desired static addresses and observed IPv4 prefixes on other interfaces during planning.

Overlapping prefixes on different interfaces are blocked by default. Intentional overlap for NAT, HA, or lab cases must be explicit:

```yaml
spec:
  interface: lan
  address: 192.168.160.3/24
  allowOverlap: true
  allowOverlapReason: overlapping customer network for NAT lab
```

## Sysctl

`system.routerd.net/v1alpha1` `Sysctl` declares a kernel parameter.

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Sysctl
metadata:
  name: ipv4-forwarding
spec:
  key: net.ipv4.ip_forward
  value: "1"
  runtime: true
  persistent: false
```

`runtime: true` means routerd should manage the running kernel value. `persistent: true` is reserved for OS-specific rendering such as sysctl.d or rc.conf and is not applied yet.
