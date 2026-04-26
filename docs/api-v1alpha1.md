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
- `plugin.routerd.net/v1alpha1` for plugin manifests

## MVP Resources

- `Interface`
- `IPv4StaticAddress`
- `IPv4DHCPAddress`

The schema is intentionally small and will be implemented incrementally.
