---
title: はじめに
---

# はじめに

![インターフェース名を調べ、小さな YAML を validate と dry-run で確認してからサービスを起動する流れ](/img/diagrams/tutorial-getting-started.png)

このページでは、ネットワークを急に変えない最初の手順を試します。
最初の 4 段階では daemon（起動し続けるプログラム）を起動せず、VM のネットワークを
本当に変更しません。

## 0. 安全を確認する

次の条件がそろってから進めます。

- 隔離した Ubuntu Server VM を使っている。
- Proxmox、KVM、VirtualBox などの **VM コンソール**を開ける。
- 変更する WAN / LAN 用 NIC（ネットワークにつなぐ口）とは別に、管理用 NIC または
  コンソールがある。
- WAN / LAN 用の仮想ネットワークは、普段の家・学校・職場のネットワークから分かれている。

routerd は、起動後にインターフェース、アドレス、経路、サービスを変えることが
あります。SSH 接続が切れても直せることを先に確認してください。

まだ routerd を入れていなければ、[インストール](./install.md) を先に行います。

## 1. インターフェース名を調べる

```sh
ip -br link
ip -br address
```

ここでは例として、`ens18` を WAN、`ens19` を LAN にします。
あなたの VM の表示に合わせて必ず読み替えてください。管理用 NIC を `ens20` に
していても、この最初の YAML には入れません。routerd に管理させないからです。

## 2. 小さな YAML を作る

作業用ディレクトリで `first-router.yaml` を作り、次を書きます。

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: first-router
spec:
  resources:
    # 上流側は、最初は外部のネットワーク設定に任せる。
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ens18
        adminUp: true
        managed: false
        owner: external

    # LAN 側だけを routerd が管理する候補にする。
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ens19
        adminUp: true
        managed: true
        owner: routerd
```

この段階ではまだ WAN の DHCP、LAN の IP アドレス、NAT は入れません。
一度にたくさん変えるより、役割を一つずつ足す方が原因を見つけやすくなります。

## 3. daemon の前に検証する

`routerctl validate` は動いている daemon のソケットに接続するコマンドです。
まだサービスを起動していない最初の確認では、`routerd validate` を使います。

```sh
sudo routerd validate --config ./first-router.yaml
```

`config is valid` と表示されれば、YAML の形は通っています。エラーが出たら
サービスを起動せず、ファイル名、インデント、インターフェース名を見直します。

## 4. 使い捨ての場所で dry-run する

次は、適用の流れを試します。`state`、`ledger`、`status` の保存先を明示的に
一時ディレクトリへ向けます。

```sh
LAB_DIR="$(mktemp -d)"
sudo routerd apply --config ./first-router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
sudo sed -n "1,120p" "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

`--dry-run` があるため、routerd は本当のネットワーク変更をしません。
表示された plan と `status.json` で、`ens18` と `ens19` が意図どおりかを
確認します。知らない NIC、知らないファイル、警告が出たら、ここで止めて YAML を
直します。

## 5. 実際の変更は次の手順で行う

この小さな設定はインターフェースの宣言だけで、DHCP、NAT、インターネット共有は
まだ提供しません。サービスは停止したまま、[最初のルーター](./first-router.md)へ
進みます。完成した設定の確認、起動、状態確認、LAN クライアントの通信確認を
そこで順に行います。

## 次に読むもの

- [最初のルーターを立ち上げる](./first-router.md) — DHCPv4 WAN と LAN の IP アドレスを足す
- [適用と生成](../concepts/apply-and-render.md) — `routerd` と `routerctl` の役割を知る
- [NAT44 とファイアウォールの準備](./basic-firewall.md) — NAT と現在のファイアウォール機能の注意
