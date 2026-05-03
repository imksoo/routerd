---
title: リソースモデル
slug: /concepts/resource-model
sidebar_position: 3
---

# リソースモデル

routerd の YAML ファイルには `Router` というリソースが 1 つあり、その中に
型付きサブリソースが並びます。ここではその構造とスキーマ全体に共通する規約を説明します。

## ルーターファイル

最小のルーター YAML は次の形です。

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: home-router
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ens18
        adminUp: true
        managed: true
```

最上位は常に以下の形です。

| フィールド | 意味 |
|---|---|
| `apiVersion` | `routerd.net/v1alpha1` (routerd の API バージョン) |
| `kind` | `Router` |
| `metadata.name` | このルーターを示すラベル。安定した名前を付けます。 |
| `spec.resources` | ルーターの振る舞いを構成するサブリソースの一覧 |

## サブリソース

`spec.resources` の各要素もまたリソースで、`apiVersion`、`kind`、
`metadata.name`、`spec` を持ちます。`apiVersion` でグループが分かります。

- `net.routerd.net/v1alpha1` — ネットワーク系 (インターフェース、アドレス、DHCP、
  NAT、ファイアウォール、経路ポリシーなど)
- `system.routerd.net/v1alpha1` — ホスト系 (sysctl、ホスト名、NTP クライアント、
  ログ出力先、NixOS ホスト連携)
- `routerd.net/v1alpha1` — routerd 自身の種類 (観測専用の `Inventory` を含む)

種類の全カタログは [API リファレンス](../reference/api-v1alpha1) を参照してください。

## 安定した名前

YAML の中ではリソースを `metadata.name` で参照します。OS のオブジェクト名は使いません。
カーネルのインターフェース名 `ens18` が出てくるのは、`Interface` リソースの
`spec.ifname` 1 か所だけです。それ以外では `wan`、`lan`、`mgmt` のような名前で参照します。
これにより、NIC を差し替えても他のリソースを書き換えずに済みます。

たとえば `DHCPv4Address` は「ens18 を使う」とは書きません。次のように書きます。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Address
  metadata:
    name: wan-dhcpv4
  spec:
    interface: wan
```

ここでの `wan` は `Interface` リソースの `metadata.name` を指しています。

## `spec` と `status` の役割

routerd は Kubernetes と同じ方式でデータを分けています。

- `spec` は望む状態を表します。YAML に書くものはこれです。
- `status` は観測した状態を表します。routerd が SQLite に書きます。
  `routerctl describe` や `routerctl show` で読めます。

たとえば `DHCPv6PrefixDelegation` には次のようなフィールドがあります。

- `spec.interface`、`spec.prefixLength`、`spec.profile`、... — 宣言したもの
- `status.currentPrefix`、`status.lastObservedAt`、`status.duid`、
  `status.iaid`、... — routerd が観測したもの

YAML に `status` は書きません。routerd が記録します。

## プロファイル

種類によっては *プロファイル* で意見の入った既定値をまとめて指定できます。
代表例は `DHCPv6PrefixDelegation.spec.profile` で、`ntt-hgw-lan-pd` のような上流環境を
指定すると、DUID 種別やプレフィックス長などの妥当な既定が引き当てられます。
プロファイルがあると、上流環境を一度で記述でき、個別フィールドを並べ直さずに済みます。

プロファイルは明示的に書いたフィールドを上書きしません。`spec.prefixLength` を
書けば、プロファイルの既定値は当たりません。だから「`profile: ntt-hgw-lan-pd` を指定し、
さらに 1 つだけ細かく調整する」という書き方が予期せぬ副作用なく安全に行えます。

## 次に読むもの

- [apply と render](./apply-and-render) — YAML がホストに届くまで
- [状態と所有](./state-and-ownership) — routerd が記憶するもの
- [API リファレンス](../reference/api-v1alpha1) — すべての種類とフィールド
