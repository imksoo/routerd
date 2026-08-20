---
title: 最初の実験用ルーターを立ち上げる
sidebar_position: 2
---

# 最初の実験用ルーターを立ち上げる

![DHCPv4 の WAN、LAN の DHCP、NAT44、検証、dry-run、live apply、状態確認を示す最初のルーター](/img/diagrams/tutorial-first-router.png)

このチュートリアルでは、隔離した LAN のテスト用クライアントにプライベート IPv4
アドレスとゲートウェイを渡し、上流ネットワークへ IPv4 で出られるようにします。完全な例
[`examples/example-basic-ipv4-nat.yaml`](../../../../../../examples/example-basic-ipv4-nat.yaml)
を使います。

家庭用ルーターへそのまま貼り付ける手順ではなく、実験用の手順です。

:::danger 復旧経路を残す

VM コンソール、シリアルコンソール、または別の管理 NIC を使います。下の `ens18` と
`ens19` は例です。`ip -br link` で自分の NIC 名を確認し、管理接続に使っている NIC を
推測で選ばないでください。例の `192.168.10.0/24` は、上流、VPN、学校、管理ネットワーク
と重なるなら別の未使用範囲に変えます。

:::

## 成功した状態

```text
上流ネットワーク -- WAN -- routerd ホスト -- LAN -- テスト用クライアント
                              DHCP + NAT
```

1. 上流が WAN に DHCP で IPv4 アドレスを渡す。
2. ルーターが LAN のテスト用クライアントに `192.168.10.100` から
   `192.168.10.199` のアドレスを渡す。
3. テスト用クライアントが `192.168.10.1` をゲートウェイとして使う。
4. NAT により、クライアントの IPv4 通信が 1 つの上流アドレスを通って外へ出る。

DHCP、ゲートウェイ、NAT という言葉が初めてなら、先に
[ネットワークの基本](./network-basics.md)を読んでください。

## 1. 完成した実験用ファイルを取得して直す

```bash
# インストール済み release には router.yaml.sample だけがあり、全ての例は含まれません。
# インストール済み routerd と同じ release から例を取得します。
ROUTERD_VERSION="$(sudo routerd version | awk '{print $2}')"
curl --fail --location --output first-router.yaml \
  "https://raw.githubusercontent.com/imksoo/routerd/${ROUTERD_VERSION}/examples/example-basic-ipv4-nat.yaml"
```

同じ release tag の source checkout がすでにある場合は、代わりにその checkout の
`examples/example-basic-ipv4-nat.yaml` をコピーできます。

最初の preview の前に、`first-router.yaml` で次だけを自分の実験環境に合わせます。

- `Interface/wan.spec.ifname`: 実験用 WAN の NIC 名。
- `Interface/lan.spec.ifname`: 隔離した LAN の NIC 名。
- `192.168.10.0/24`: 接続済みのネットワークと重なる場合だけ変更する。
- 公開 DNS サーバー: 実験環境で別のリゾルバーを使う必要がある場合だけ変更する。

このファイルでは WAN を外部管理のままにし、LAN を routerd が管理します。DHCPv4
サーバー、NAT44 ルール、基本的な zone リソースを含みます。

:::caution firewall の範囲

routerd の firewall リソースは機能の基礎であり、安全性の認証ではありません。この例だけを
唯一の防御境界にしてインターネットへ公開しないでください。最初の実験は隔離した仮想
スイッチまたは物理ラボネットワークの内側で行います。

:::

## 2. daemon を起動する前に検証と preview をする

```bash
sudo routerd validate --config first-router.yaml

LAB_DIR="$(mktemp -d)"
sudo routerd apply --config first-router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

この 2 つのコマンドはホストのネットワークを変更しません。検証に失敗したら、live apply を
試すのではなく YAML を直します。dry-run に管理 NIC、経路、生成されるファイルが予想どおりに
表示されていることを確認してください。

## 3. コンソールから適用する

preview が想定どおりの NIC と生成物を示したときだけ、コンソールまたは独立した管理経路から
変更します。

```bash
sudo routerd apply --config first-router.yaml --once
```

実験用ルーターを継続して動かすなら、確認済みのファイルを標準の場所に置いてサービスを
起動します。

```bash
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 first-router.yaml /usr/local/etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
```

## 4. 小さな通信経路を最後まで確認する

ルーター上で確認します。

```bash
sudo routerctl get status
sudo routerctl describe DHCPv4Client/wan-dhcpv4
sudo routerctl describe DHCPv4Server/lan-dhcpv4
sudo routerctl describe NAT44Rule/lan-to-wan
```

隔離した LAN だけにつないだクライアントで DHCP リースを更新し、次を確認します。

```bash
ip route
ping 192.168.10.1
curl -I https://example.com/
```

最後のコマンドが失敗したときは、原因を分けて確認します。最初にクライアントがアドレスと
ゲートウェイを受け取ったか、次に WAN のリースがあるか、最後に DNS を確認します。原因が
不明なまま何度も apply しないでください。

## 次に読むもの

- [基本 IPv4 NAT ゲートウェイ](../config-examples/basic-ipv4-nat.md) — このファイルを
  図と YAML の対応で説明します
- [LAN 側サービス](./lan-side-services.md) — IPv4 が動いてからローカル DNS と IPv6 を
  足します
- [基本 NAT と firewall policy](./basic-firewall.md) — 現在の firewall の範囲と安全な次の手順
