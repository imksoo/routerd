---
title: はじめに
---

# はじめに

このチュートリアルでは、WAN インターフェースと LAN インターフェースを持つホストで routerd の最小手順を通します。まず WAN 側で DHCPv4 アドレスを受け取り、次に LAN 側へ静的アドレスを振り、その後にインストールと収束処理へ進みます。

routerd はまだ v1alpha1 のソフトウェアです。実ルーターへ適用する前に、ラボ VM やコンソール接続のあるホストで試してください。

## 1. routerd をビルドする

```bash
make build
```

生成される実行ファイル:

- `bin/routerd`
- `bin/routerctl`

## 2. WAN と LAN のインターフェースを確認する

まずマシンの物理的な形を確認します。以下の例では `wan` が上流ネットワーク、`lan` が下流クライアントネットワークです。実際のインターフェース名はホストに合わせて置き換えます。

```bash
ip link
```

例えば小さなルーター VM では次のような対応になります。

- WAN: `ens18`
- LAN: `ens19`

routerd の設定では `wan` や `lan` のような安定したリソース名を使い、`spec.ifname` で OS 上の実インターフェースに対応させます。

## 3. 最初の構成要素: WAN DHCPv4

最初に役立つリソースの組み合わせは `Interface` と `IPv4DHCPAddress` です。WAN インターフェースを有効にして、上流ネットワークから IPv4 アドレスを受け取ります。

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: first-router

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

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4DHCPAddress
      metadata:
        name: wan-dhcp4
      spec:
        interface: wan
        client: dhclient
        required: true
```

リポジトリには同じ形の `examples/basic-dhcp.yaml` があります。ネットワーク状態を変更する前に、検証と予行実行で確認します。

```bash
routerd validate --config examples/basic-dhcp.yaml
routerd reconcile --config examples/basic-dhcp.yaml --once --dry-run
```

予行実行の出力は JSON 形式の状態です。どのリソースが正常か、差分があるか、routerd が何をしようとしているかを確認できます。

## 4. LAN アドレスを足す

WAN 側の動きが見えたら、次の構成要素として LAN インターフェースを足します。最小の LAN 側は `Interface` と `IPv4StaticAddress` で表現できます。

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ens19
        adminUp: true
        managed: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: lan-ipv4
      spec:
        interface: lan
        address: 192.168.160.3/24
        exclusive: true
```

この時点では、まだクライアント向けの完全なルーターではありません。DHCP サービス、DNS、NAT、IPv6 PD、DS-Lite、PPPoE、経路ポリシーは後続のリソースです。小さく分けて足していくと、計画を読みやすくなります。

## 5. ソースインストール用の配置で入れる

routerd は `/usr/local` 配下を標準のインストール先としています。

```bash
sudo make install
sudo install -m 0644 examples/basic-dhcp.yaml /usr/local/etc/routerd/router.yaml
```

主な標準パス:

- 設定: `/usr/local/etc/routerd/router.yaml`
- 実行ファイル: `/usr/local/sbin/routerd`
- プラグイン: `/usr/local/libexec/routerd/plugins`
- Runtime: `/run/routerd`
- State: `/var/lib/routerd`

## 6. 一度だけ収束処理を実行する

デーモンを有効にする前に、必ず一度だけ実行するモードで確認します。

```bash
sudo /usr/local/sbin/routerd reconcile \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

計画が期待通りになってから `--dry-run` を外します。

## 7. デーモンを有効にする

一度だけの実行が問題なければ systemd ユニットを入れます。

```bash
sudo make install-systemd
sudo systemctl daemon-reload
sudo systemctl enable --now routerd.service
```

`routerd serve` は `/run/routerd/` 配下にローカル制御 API ソケットを開き、定期的な収束処理を実行します。

## 次に読むもの

- [リソース API リファレンス](/ja/docs/reference/api-v1alpha1)
- [ルーターラボのチュートリアル](/ja/docs/tutorials/router-lab)
- [制御 API](/ja/docs/reference/control-api-v1alpha1)
