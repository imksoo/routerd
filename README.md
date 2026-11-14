# routerd

routerd applies declarative YAML network configuration to a router host.

[English](docs/intro.md) · [日本語](docs/ja/intro.md) · [繁體中文](docs/zh-Hant/intro.md) · [简体中文](docs/zh-Hans/intro.md)

## Install and configure

Read [installation](docs/install-and-upgrade.md), choose a complete
[YAML example](examples), and adapt interface names, addresses, secrets and peers.
Keep console access when changing a running router.

```sh
routerd validate --config router.yaml
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
routerctl apply -f router.yaml --replace
routerctl get status
```

`routerctl` connects to an existing daemon. Use `--replace` only with the complete
router configuration, not a partial resource update.

## Configuration reference

[Resource fields and YAML](docs/reference/resources.md) ·
[DS-Lite](docs/config-examples/dslite-home.md) ·
[LocalServiceRedirect](docs/config-examples/local-dns-redirect.md) ·
[JSON Schema](schemas/routerd-config-v1alpha1.schema.json)

YAML is the configuration source; acquired DHCP/PD/BGP state is not static YAML.
See [configuration philosophy](docs/concepts/design-philosophy.md) and
[operation](docs/operations/reconcile.md).

[Releases](https://github.com/imksoo/routerd/releases/latest) ·
[Changelog](docs/releases/changelog.md) ·
[License](LICENSE) · [Third-party licenses](THIRD_PARTY_LICENSES.md)

Development rules are in [AGENTS.md](AGENTS.md). After changing the configuration
schema or canonical examples, run `make generate-resource-docs` in the development
Pod to refresh the four-language resource reference.
