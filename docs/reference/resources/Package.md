# Package

[Resource configuration reference](../resources.md#package) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Ensures selected OS packages are installed. The controller selects the first package set whose os exactly matches the running host.

## Conditions and references

- Only present is supported; this resource cannot remove packages. An unmatched host OS produces Pending/NoMatchingOSPackageSet. optional is accepted in each set but is not read by this controller; installation failures still return an error.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Package
  metadata:
    name: "<name>"
  spec:
    packages: # Required; array
      - # object
        manager: "" # optional; string; enum=["", "apt", "dnf", "pkg"]
        names: # Required; array
          - "<string>" # string
        optional: true # optional; boolean
        os: "ubuntu" # Required; string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"]
    state: "" # optional; string; enum=["", "present"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.state` | Desired package state; omitted means present. Other values are rejected. |
| `spec.packages` | Required nonempty list of OS-specific package sets. No resource references. |
| `spec.packages[].os` | Required OS ID: ubuntu, debian, fedora, rhel, rocky, almalinux or freebsd. Linux reads /etc/os-release ID; there is no ID_LIKE matching. |
| `spec.packages[].manager` | Default apt for ubuntu/debian, dnf for fedora/rhel/rocky/almalinux, pkg for freebsd. An explicit manager must match this OS mapping. |
| `spec.packages[].names` | Required nonempty package-name list. Each name must be nonblank and contain no whitespace or /. No implicit package names are added by this field. |
| `spec.packages[].optional` | Default false. The field is decoded but currently has no effect on PackageController error handling. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/package.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/package.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)
