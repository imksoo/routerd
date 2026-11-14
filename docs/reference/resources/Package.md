# Package

[Resource configuration reference](../resources.md#package) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Ensures selected OS packages are installed. The controller selects the first package set whose os exactly matches the running host.

## Conditions and references

- Only present is supported; this resource cannot remove packages. An unmatched host OS produces Pending/NoMatchingOSPackageSet. optional is accepted in each set but is not read by this controller; installation failures still return an error.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: Package
  metadata:
    name: "<name>"
  spec:
    packages:
      -
        [ manager: ("" | "apt" | "dnf" | "pkg") ]
        names:
          - <string>
        [ optional: <boolean> ]
        os: ("ubuntu" | "debian" | "fedora" | "rhel" | "rocky" | "almalinux" | "freebsd")
    [ state: ("" | "present") ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.state` | *Optional* | string; enum=["", "present"] | Desired package state; omitted means present. Other values are rejected. |
| `spec.packages` | **Required** | array | Required nonempty list of OS-specific package sets. No resource references. |
| `spec.packages[].os` | **Required** | string; enum=["ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "freebsd"] | Required OS ID: ubuntu, debian, fedora, rhel, rocky, almalinux or freebsd. Linux reads /etc/os-release ID; there is no ID_LIKE matching. |
| `spec.packages[].manager` | *Optional* | string; enum=["", "apt", "dnf", "pkg"] | Default apt for ubuntu/debian, dnf for fedora/rhel/rocky/almalinux, pkg for freebsd. An explicit manager must match this OS mapping. |
| `spec.packages[].names` | **Required** | array | Required nonempty package-name list. Each name must be nonblank and contain no whitespace or /. No implicit package names are added by this field. |
| `spec.packages[].optional` | *Optional* | boolean | Default false. The field is decoded but currently has no effect on PackageController error handling. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/package.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/package.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `packages`, `dryRun`
