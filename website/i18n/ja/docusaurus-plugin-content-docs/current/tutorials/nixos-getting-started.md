---
title: Nix / NixOS から始める
---

# Nix / NixOS から始める

このチュートリアルは Nix を初めて触る運用者向けです。関与の度合いに
応じて、次の 3 段階で進められるように構成しています。

1. **バイナリを試す** — 上流の flake から routerd を一発実行し、ホストの
   設定には触らずに動作を確認する。
2. **ソースで開発する** — `nix develop` で routerd が依存するホスト
   ツールを揃えた開発シェルに入る。
3. **NixOS で本番運用する** — 付属の NixOS モジュールで routerd を
   組み込み、必要なら `router.yaml` から永続ホスト設定も生成する。

NixOS は routerd の Tier 2 プラットフォームです。ビルド、systemd
ユニット、`routerd render nixos` の流れまでは整備済みですが、いくつかの
リソース種別は現状では非永続な実行時判断で動かしています。最新の対応
状況は [対応プラットフォーム](/ja/docs/platforms) にまとまって
います。

## 前提

次のいずれかが必要です。

- 任意の Linux ホスト + [Nix パッケージマネージャ](https://nixos.org/download)
  + Flakes 有効化。
- NixOS ホスト（Flakes を推奨。手順は
  [NixOS Wiki](https://nixos.wiki/wiki/Flakes) を参照）。

非 NixOS で Flakes をまだ有効化していない場合は、`~/.config/nix/nix.conf`
に次を追記しておきます。

```text
experimental-features = nix-command flakes
```

routerd の flake が対応するシステムは `x86_64-linux` と `aarch64-linux`
です。macOS や Windows での routerd 本体動作は対象外としています。

## 1. まずはバイナリを試す

GitHub から直接 routerd を起動できます。flake は Ubuntu のソース
インストールと同じ Go バイナリをビルドします。

```bash
nix run github:imksoo/routerd#routerd -- --help
nix run github:imksoo/routerd#routerctl -- --help
```

その場で実行する代わりに、`result/bin/` 配下にシンボリックリンクで
ビルド成果物を置きたい場合は次を使います。

```bash
nix build github:imksoo/routerd#routerd
./result/bin/routerd --help
```

NixOS の構成に手を入れる前に、自分のカーネルとアーキテクチャでバイナリ
が動くことだけ確認しておく、という用途に向いています。

## 2. ソースで開発する

リポジトリを clone して開発シェルに入ります。シェルには、routerd の
レンダラがホスト側で実行する `iproute2`、`nftables`、`dnsmasq`、
`conntrack-tools`、`ppp` と、調査用の `dnsutils`、`iputils`、`tcpdump`、
`traceroute` に加え、Go と Make が事前に揃っています。

```bash
git clone https://github.com/imksoo/routerd
cd routerd
nix develop
```

シェル内では、ホストに何もインストールせずに通常の Makefile 手順を
実行できます。

```bash
make build
make test
make validate-example
make dry-run-example
```

`bin/routerd` と `bin/routerctl` は作業ディレクトリ内に生成されるだけ
で、システムにはインストールされません。

## 3. NixOS で routerd を動かす

routerd は `contrib/nix/module.nix` に NixOS モジュールを同梱しています。
このモジュールは `routerd` パッケージをインストールし、systemd ユニット
を宣言し、レンダラが起動時に `iproute2`、`nftables`、`dnsmasq`、
`conntrack`、`ppp`、`dnsutils`、`iputils`、`tcpdump`、`traceroute` を
見つけられるようにします。

### 3.1 flake の input に追加する

システム flake（典型的には `/etc/nixos/flake.nix` または手元の dotfiles
flake）に routerd を input として加えます。

```nix
{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  inputs.routerd.url = "github:imksoo/routerd";

  outputs = { self, nixpkgs, routerd, ... }: {
    nixosConfigurations.router = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        ./configuration.nix
        routerd.nixosModules.default
        ({ pkgs, ... }: {
          services.routerd = {
            enable = true;
            package = routerd.packages.${pkgs.system}.routerd;
            configFile = ./router.yaml;
          };
        })
      ];
    };
  };
}
```

`router.yaml` は Ubuntu でのインストールと同じ宣言的設定です。まだ
書けていない場合は、リポジトリの `examples/basic-dhcp.yaml` をひな型
としてコピーし、自分のホストのインターフェース名に合わせてください。

反映は次のコマンドで行います。

```bash
sudo nixos-rebuild switch --flake .#router
```

切り替え後、`routerctl status` で Unix ソケット越しに実行時の状態を
確認できます。

```bash
sudo routerctl status
```

この段階で routerd は systemd ユニットとして常駐し、実行時判断の
反映を続けています。一方で、永続的な NixOS 設定（ホスト名、
ユーザ、SSH、ブートローダーなど）はまだ手書きの `configuration.nix`
側の責任です。

### 3.2 任意: ホスト設定も routerd に任せる

「このマシンはルータ専用」というホストでは、ホスト設定そのものも
`router.yaml` の `NixOSHost` リソースとして書き、routerd に NixOS
モジュールとして生成させることができます。役割分担は次のようになります。

- `router.yaml` を唯一の入力として扱います。`NixOSHost` リソース
  （ホスト名、ブートローダー、ユーザ、SSH、sudo、追加パッケージなど）
  と通常のネットワークリソースを同じファイルに書きます。
- `routerd render nixos` が `router.yaml` を読み、
  `routerd-generated.nix` を出力します。このファイルは routerd が
  所有するため、手で編集してはいけません。
- `configuration.nix` は最小限に保ちます。`hardware-configuration.nix`
  と `routerd-generated.nix` を import するだけで、`router.yaml` に
  載せにくいホスト固有の上書きがある場合だけ追記します。
- 永続状態の反映には `nixos-rebuild switch` を使い、ヘルスチェックや
  経路選択、AFTR 解決、ステータス報告、conntrack 観測などの非永続な
  実行時判断は `routerd serve` が引き続き担当します。
  まだ flake の NixOS モジュールを取り込まない場合は、
  `NixOSHost.spec.routerdService.enabled: true` を設定すると、
  `/usr/local/sbin/routerd serve` を起動するローカルの
  `routerd.service` も生成できます。

動くラボ例は次の 2 ファイルです。

- `examples/nixos-edge.yaml` — `NixOSHost`、`Interface`、
  `IPv4DHCPAddress`、`IPv6DHCPAddress` を含む `router.yaml`。
- `examples/nixos-edge-configuration.nix` — 上記に対応する手書きの
  最小 `configuration.nix`。

レンダリングコマンドは次のとおりです。

```bash
routerd render nixos \
  --config /etc/nixos/router.yaml \
  --out /etc/nixos/routerd-generated.nix
sudo nixos-rebuild switch
```

`routerd render nixos` は生成された Nix ファイルを書き出すだけで、
`nixos-rebuild` の実行も、手書きの `configuration.nix` の編集も
行いません。

### 3.3 モジュールの主なオプション

`services.routerd` でよく使うオプションは次のとおりです。

| オプション | 用途 |
| --- | --- |
| `enable` | ユニットの有効化。 |
| `package` | インストールする routerd の派生。通常は `routerd.packages.${pkgs.system}.routerd`。 |
| `configFile` | Nix ストア外の `router.yaml` のパス。 |
| `configText` | インラインで書く `router.yaml`。Nix ストアに展開されます。`configFile` とは排他。 |
| `socket` | 制御 API の Unix ソケットパス。既定 `/run/routerd/routerd.sock`。 |
| `applyInterval` | 定期反映の間隔（Go の duration 形式）。既定 `60s`。 |
| `extraFlags` | `routerd serve` に追加で渡すコマンドラインフラグ。 |

完全な一覧は `contrib/nix/module.nix` にあります。

`routerd render nixos` だけを使い、flake モジュールをまだ使わない場合は、
同等の設定を `NixOSHost.spec.routerdService` に書きます。この方法は
ラボ環境や `/usr/local/sbin/routerd` に置いたバイナリでの動作確認向け
です。NixOS として長く運用するなら、最終的には flake モジュールに寄せる
方が自然です。

### 3.4 ファイアウォールとの共存

NixOS は既定で `networking.firewall.enable = true` のままです。これは
iptables-nft の独立 chain (`nixos-fw`) として実装されており、routerd が
出力する nftables テーブル (`inet routerd_filter`、`bridge
routerd_l2_filter` など) と **並行** に動作します。routerd は NixOS
firewall を無効化したり置き換えたりはしません。

実用上の影響: routerd の `routerd_filter` で accept ルールを書いても、
パケットは先に `nixos-fw` を通過する必要があります。NixOS firewall が
許可していないポートに着信したリソース (代表例: VXLAN underlay の
UDP/4789、LAN に公開する独自サービス) は、routerd の chain に届く前に
NixOS 側で drop されます。

`configuration.nix` で同じポートを許可してください:

```nix
networking.firewall.allowedUDPPorts = [ 4789 ];   # VXLAN underlay
networking.firewall.allowedTCPPorts = [ 22 ];     # SSH を公開する場合
networking.firewall.trustedInterfaces = [ "br-home" ]; # bridge / LAN
```

routerd 管理の bridge / LAN を全面的に信頼してよい場合は、
`trustedInterfaces` への追加が一番すっきりします。NixOS はその
インターフェイスでは firewall をスキップし、routerd のポリシーが
そのまま効きます。

NixOS firewall での drop を疑うときの確認手順:

```bash
sudo iptables -L nixos-fw -n -v --line-numbers
sudo nft list ruleset                # routerd 側のテーブルだけ
```

覚えておくと便利な症状: `tcpdump` で underlay にパケットが到着して
見えるのに、対応する kernel デバイスの RX カウンタが 0 のまま、
かつ routerd 側 input chain のカウンタも増えない場合は、`nixos-fw`
で drop されている可能性が高いです。

## つまずきやすいところ

- **Flakes が無効。** `nix run github:...` が `experimental-features`
  でエラーになる場合は、前提のとおり Flakes を有効化してください。
- **`routerd-generated.nix` を手で書き換えてしまう。** routerd が
  上書きします。手書き設定は `configuration.nix` か別の import 先に
  置いてください。
- **Ubuntu のソースインストールと NixOS モジュールを同じホストで
  併用する。** どちらか一方に統一してください。NixOS ではモジュール側
  を推奨します。
- **`networking.firewall` が routerd と並行で動いていることを忘れる。**
  3.4 を参照してください。routerd は無効化しません。
- **すべてのリソースが Nix ネイティブで生成されると期待する。** 現状
  のレンダラはホスト設定、依存パッケージ、基本的な systemd-networkd
  の `.network` 宣言までです。それ以外のリソース種別は実行時の
  反映処理を経由します。詳細は [対応プラットフォーム](/ja/docs/platforms)
  にあります。

## 次に読むもの

- [リソース API リファレンス](/ja/docs/reference/api-v1alpha1) の
  `NixOSHost` 項。
- もう少し実用的な `router.yaml` を読みたい場合は
  [ルータラボ チュートリアル](/ja/docs/tutorials/router-lab)。
- NixOS で対応済みの範囲と未対応の項目は
  [対応プラットフォーム](/ja/docs/platforms) を参照。
