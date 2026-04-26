# routerd

`routerd` is a declarative router resource reconciler written in Go.

The first MVP targets Ubuntu Server and focuses on the smallest reliable core:
- YAML router config
- Kubernetes-like resource shapes
- interface alias resolution
- local trusted resource plugins
- dry-run reconcile
- machine-readable status JSON

The MVP does not implement firewall, NAT, DS-Lite, IPv6 PD, policy routing, remote plugin installation, or full rollback.

## Requirements

- Go 1.22 or newer
- `make`
- `iproute2`
- `jq`

On Ubuntu:

```sh
sudo apt-get update
sudo apt-get install -y golang-go make iproute2 jq
```

## Build

```sh
make build
```

or:

```sh
go build ./cmd/routerd
```

## Test

```sh
make test
```

or:

```sh
go test ./...
```

## Example Commands

```sh
make validate-example
make dry-run-example
```

The current CLI is an initial scaffold. Resource loading, plugin execution, and reconcile behavior will be implemented incrementally.

## Default Paths

- Config: `/etc/routerd/router.yaml`
- Plugin dir: `/usr/lib/routerd/plugins`
- Runtime dir: `/run/routerd`
- State dir: `/var/lib/routerd`
- Status file: `/run/routerd/status.json`
- Lock file: `/run/routerd/routerd.lock`
