# routerd

`routerd` は、宣言的なルーター用リソースを読み取り、実ホストの状態へ反映する Go 製の reconciler です。

現在の主な対象は Ubuntu Server です。インストール先は `/usr/local` 配下を基本にし、将来の dpkg や FreeBSD ports でも包みやすい形にしています。

## 現在できること

- YAML によるルーター設定
- Kubernetes 風の `apiVersion` / `kind` / `metadata.name` / `spec`
- インターフェース alias 解決
- IPv4/IPv6 アドレス計画
- systemd-networkd drop-in による IPv6 Prefix Delegation
- dnsmasq による DHCPv4、DHCPv6/RA、DNS forward/cache
- runtime sysctl 管理
- nftables による IPv4 Source NAT
- nftables mark + `ip rule` による IPv4 policy routing
- DS-Lite ipip6 tunnel、複数 tunnel のhash分散
- status JSON
- dry-run / plan
- ローカル trusted plugin の土台

まだ限定的なもの:

- firewall policy
- remote plugin install
- full rollback
- 常駐 daemon loop

## 必要なもの

- Go 1.24 以上
- `make`
- `iproute2`
- `jq`
- `dnsmasq`
- `nftables`
- `conntrack`

Ubuntu 例:

```sh
sudo apt-get update
sudo apt-get install -y golang-go make iproute2 jq dnsmasq nftables conntrack
```

`conntrack` は、複数 DS-Lite tunnel の policy routing や flow mark の診断にも使います。

## ビルド

```sh
make build
```

生成物は `bin/routerd` です。

schema を Go の型から再生成する場合:

```sh
make generate-schema
make check-schema
```

## インストール

ローカル source install:

```sh
sudo make install
```

remote test host へ tarball install:

```sh
make remote-install REMOTE_HOST=user@router.example
```

remote host の依存確認:

```sh
make check-remote-deps REMOTE_HOST=user@router.example
```

config のみ remote host へ入れる:

```sh
make remote-install-config REMOTE_HOST=user@router.example CONFIG=path/to/router.yaml
```

## よく使うコマンド

```sh
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
routerd reconcile --config examples/router-lab.yaml --once --dry-run
```

実反映:

```sh
sudo routerd reconcile --config /usr/local/etc/routerd/router.yaml --once
```

remote router で実行する前に、`plan` と `dry-run` を確認してください。cloud-init や既存 netplan が管理しているインターフェースを不用意に奪わないよう、routerd は adoption が必要な状態を検出して計画に出します。

## 既定パス

- Config: `/usr/local/etc/routerd/router.yaml`
- Plugin dir: `/usr/local/libexec/routerd/plugins`
- Binary: `/usr/local/sbin/routerd`

Linux:

- Runtime dir: `/run/routerd`
- State dir: `/var/lib/routerd`
- Status file: `/run/routerd/status.json`
- Lock file: `/run/routerd/routerd.lock`

FreeBSD:

- Runtime dir: `/var/run/routerd`
- State dir: `/var/db/routerd`
- Status file: `/var/run/routerd/status.json`
- Lock file: `/var/run/routerd/routerd.lock`

## ドキュメント

- [API v1alpha1](docs/api-v1alpha1.md)
- [Plugin protocol](docs/plugin-protocol.md)
- [API v1alpha1 日本語](docs/api-v1alpha1.ja.md)
- [Plugin protocol 日本語](docs/plugin-protocol.ja.md)
