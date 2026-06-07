---
title: パブリック DNS をローカルリゾルバへリダイレクト
sidebar_position: 80
---

# パブリック DNS をローカルリゾルバへリダイレクト

![LAN の plaintext DNS が IPAddressSet で一致し local DNSResolver へ redirect される構成](/img/diagrams/config-example-local-dns-redirect.png)

LAN クライアントが有名なパブリックリゾルバへ平文 DNS を直接送ろうとしたときに、
TCP/UDP の port 53 だけをルーターのローカルリゾルバへリダイレクトする例です。
DoH や DoT の port には手を加えません。

完全な YAML は `examples/example-local-dns-redirect.yaml` にあります。

## 構成図

```mermaid
flowchart LR
  client["[1] LAN client"]
  lan["[2] lan"]
  router["[3] routerd DNS リゾルバ<br/>192.168.50.1:53"]
  public["[4] public DNS names<br/>dns.google / one.one.one.one"]
  upstream["[5] actual upstream DNS"]

  client -- "8.8.8.8 or 1.1.1.1:53" --> lan --> router
  router --> upstream
  public -. "IPAddressSet FQDN refresh" .-> router
```

## 図の対応表

| 番号 | 意味 | 主な resource |
| --- | --- | --- |
| [1] | パブリック DNS へ直接問い合わせようとするクライアント。 | external client |
| [2] | prerouting のリダイレクトルールが一致する LAN インターフェース。 | `LocalServiceRedirect/lan-local-services.spec.interface` |
| [3] | リダイレクトされた port 53 のトラフィックを受けるローカルリゾルバ。 | `DNSResolver/lan-resolver` |
| [4] | nftables の set に展開される完全一致の FQDN。 | `IPAddressSet/public-dns` |
| [5] | ローカルリゾルバが実際に使う上流リゾルバ。 | `DNSForwarder`, `DNSUpstream` |

## 要点

```yaml
# [4] public DNS の exact name を IPAddressSet に解決する。
- apiVersion: net.routerd.net/v1alpha1
  kind: IPAddressSet
  metadata:
    name: public-dns
  spec:
    names:
      - dns.google
      - one.one.one.one
    refreshInterval: 10m

# [2] -> [3] 平文 DNS port 53 だけ local resolver に redirect する。
- apiVersion: firewall.routerd.net/v1alpha1
  kind: LocalServiceRedirect
  metadata:
    name: lan-local-services
  spec:
    interface: lan
    rules:
      - name: public-dns
        protocols: [tcp, udp]
        destinationSetRef: IPAddressSet/public-dns
        destinationPort: 53
        redirectPort: 53
```

`IPAddressSet.spec.names` は完全一致の DNS 名です。
`dns.google` はサブドメインを含みません。必要な宛先名はすべて明示的に列挙します。

## 確認

```bash
routerctl validate --config examples/example-local-dns-redirect.yaml
routerctl apply --config examples/example-local-dns-redirect.yaml --dry-run
routerctl describe IPAddressSet/public-dns
nft list table ip routerd_nat
```

LAN クライアントからは次のように確認できます。

```bash
dig @8.8.8.8 router.home.example
dig @1.1.1.1 router.home.example
```
