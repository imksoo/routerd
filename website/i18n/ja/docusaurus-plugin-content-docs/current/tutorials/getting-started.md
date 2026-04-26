---
title: はじめに
---

# はじめに

このチュートリアルでは、WAN と LAN の 2 本のインターフェースを持つホストで、routerd を最小手順で動かすところまでを通します。流れ自体は実機ルータを構築するときと同じで、まず物理構成を確認し、WAN 側で上流と疎通できるようにし、LAN 側にアドレスを振り、routerd をインストールしてデーモンに任せていきます。

routerd は v1alpha1 のソフトウェアです。リモートのルータに向ける前に、ラボ用の VM やコンソール接続のあるホストで一度通して動かしてください。

## 1. routerd をビルドする

```bash
make build
```

これで次の実行ファイルが生成されます。

- `bin/routerd`
- `bin/routerctl`

## 2. WAN と LAN のインターフェースを確認する

まずホスト側の物理構成を確認します。以下では `wan` を上流ネットワーク、`lan` を下流ネットワークとして扱います。実際のカーネル名はホストに合わせて読み替えてください。

```bash
ip link
```

たとえば小さなルータ用 VM では次のような対応になります。

- WAN: `ens18`
- LAN: `ens19`

routerd の設定上では `wan` や `lan` のような安定した名前を使い続けます。`spec.ifname` でその名前と OS のインターフェース名を結び付けるので、後からハードウェアを差し替えても他のリソースを書き換えずに済みます。

## 3. 最初の構成: WAN の DHCPv4

最初に役立つのは `Interface` と `IPv4DHCPAddress` の組み合わせです。WAN を上げて、上流ネットワークに IPv4 アドレスを要求する、という最小の振る舞いを宣言します。

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

リポジトリには同じ形の `examples/basic-dhcp.yaml` があります。ホストの状態を変える前に、検証と予行実行で確認します。

```bash
routerd validate --config examples/basic-dhcp.yaml
routerd reconcile --config examples/basic-dhcp.yaml --once --dry-run
```

予行実行の結果は JSON で返ります。どのリソースが健全か、ホストとどのくらい乖離しているか、routerd が何をしようとしているかを確認できます。

## 4. LAN にアドレスを足す

WAN 側の挙動が見えたら、独立した別の構成として LAN 側を追加します。最小の LAN は `Interface` と `IPv4StaticAddress` だけで足ります。

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

この時点ではまだ、クライアント向けに完成したルータではありません。DHCP サービス、DNS、NAT、IPv6 プレフィックス委譲、DS-Lite、PPPoE、経路ポリシーなどはあとで足していくリソースです。1 機能ずつ別のリソースとして増やしていくと、計画の出力が読みやすいまま保てます。

## 5. ソースインストールの配置で入れる

routerd は /usr/local 配下を標準のインストール先にしています。

```bash
sudo make install
sudo install -m 0644 examples/basic-dhcp.yaml /usr/local/etc/routerd/router.yaml
```

主な既定パス:

- 設定ファイル: /usr/local/etc/routerd/router.yaml
- 本体コマンド: /usr/local/sbin/routerd
- プラグイン: /usr/local/libexec/routerd/plugins
- 実行時ディレクトリ: /run/routerd
- 状態保存ディレクトリ: /var/lib/routerd

## 6. 反映を 1 回試す

デーモンを有効化する前に、必ず 1 回限りの反映で挙動を確認します。

```bash
sudo /usr/local/sbin/routerd reconcile \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

計画が想定どおりであることを確認してから、`--dry-run` を外します。

## 7. デーモンを常駐させる

1 回限りの実行で問題が無ければ systemd ユニットを入れます。

```bash
sudo make install-systemd
sudo systemctl daemon-reload
sudo systemctl enable --now routerd.service
```

`routerd serve` は /run/routerd/ 配下に制御 API のソケットを開き、定期的な反映を続けます。ここから先は `routerctl status` や [制御 API](/ja/docs/reference/control-api-v1alpha1) を通じて状態を確認できます。

## 次に読むもの

- [リソース API リファレンス](/ja/docs/reference/api-v1alpha1): 宣言できるすべての振る舞いの一覧。
- [ルータラボ チュートリアル](/ja/docs/tutorials/router-lab): もう少し実用に近い構成例。
- [リソース所有モデル](/ja/docs/reference/resource-ownership): 既設のルータを routerd の管理下に入れる前に必読。
