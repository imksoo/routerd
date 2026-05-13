---
title: Tailscale の exit node と subnet router
---

# Tailscale の exit node と subnet router

## 想定する構成

`TailscaleNode` は、routerd ホストを tailnet に参加させ、次の経路を広告したい場合に使います。

- exit node (`0.0.0.0/0` と `::/0`)
- 1 個以上の subnet route
- exit node と subnet route の両方

routerd は `tailscaled` を置き換えません。
routerd は `tailscale up` を実行する systemd ユニットを生成し、ノードの広告設定を管理します。
Tailscale のアカウント、制御プレーン、経路承認は Tailscale 側に残します。
routerd はホスト上の宣言設定を管理します。

## tailscale を導入する

依存パッケージを `Package` で宣言します。
これにより、必要なパッケージが YAML から分かります。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Package
metadata:
  name: router-service-dependencies
spec:
  packages:
    - os: ubuntu
      manager: apt
      names:
        - tailscale
        - tailscale-archive-keyring
    - os: nixos
      manager: nix
      names:
        - tailscale
    - os: freebsd
      manager: pkg
      names:
        - tailscale
      optional: true
```

Ubuntu では、`Package` が `tailscale` を導入する前に Tailscale の apt リポジトリが必要です。
リポジトリの追加は、通常の初期構築手順で済ませてください。

## 秘密値を Git に残さない

本番設定では `authKeyEnv` と `authKeyFile` を推奨します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: TailscaleNode
metadata:
  name: edge
spec:
  hostname: edge
  advertiseExitNode: true
  advertiseRoutes:
    - 10.0.0.0/8
    - 172.16.0.0/12
    - 192.168.0.0/16
  acceptDNS: false
  acceptRoutes: false
  authKeyEnv: TS_AUTHKEY
  authKeyFile: /usr/local/etc/routerd/secrets/tailscale.env
```

環境ファイルは routerd の YAML の外に置きます。

```sh
sudo install -d -m 0700 /usr/local/etc/routerd/secrets
sudo sh -c 'printf "%s\n" "TS_AUTHKEY=REDACTED" > /usr/local/etc/routerd/secrets/tailscale.env'
sudo chmod 0600 /usr/local/etc/routerd/secrets/tailscale.env
```

既にログイン済みのノードでは、`authKey`、`authKeyEnv`、`authKeyFile` を省略できます。
その場合、routerd は秘密値を systemd ユニットに埋め込まず、広告設定だけを再適用します。

Tailscale は既定で UDP/41641 を使います。
routerd は `TailscaleNode` がある場合にこのポートを予約済みとして扱います。
`WireGuardInterface` が同じポートを使う設定は検証で拒否します。

## プライベートアドレスをまとめて広告する

ルーターを自宅や拠点ネットワークへの入口にする場合は、RFC 1918 のプライベートアドレス全体を広告できます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: TailscaleNode
metadata:
  name: edge
spec:
  hostname: edge
  advertiseExitNode: true
  advertiseRoutes:
    - 10.0.0.0/8
    - 172.16.0.0/12
    - 192.168.0.0/16
  acceptDNS: false
  acceptRoutes: false
```

この設定を反映した後、Tailscale の管理画面で広告された経路を承認します。
承認前は、`tailscale debug prefs` では要求した経路が見えます。
ただし、`tailscale status --self --json` の `Self.AllowedIPs` にはまだ出ないことがあります。

## ファイアウォールゾーンの置き方

`tailscale0` を `Interface` として宣言します。
これにより、状態と Web Console の Interfaces に表示できます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: tailscale
spec:
  ifname: tailscale0
  managed: false
```

家庭ルーターでは、`tailscale0` は `mgmt` ではなく `trust` ゾーンに置くのが自然です。

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: FirewallZone
metadata:
  name: lan
spec:
  role: trust
  interfaces:
    - Interface/lan
    - Interface/tailscale

---

apiVersion: firewall.routerd.net/v1alpha1
kind: FirewallZone
metadata:
  name: management
spec:
  role: mgmt
  interfaces:
    - Interface/mgmt
```

この構成では、tailnet クライアントは `trust -> self` の経路で routerd Web Console などに到達できます。
一方で、ファイアウォールが `trust -> mgmt` 転送を拒否していれば、tailnet から管理 VLAN 全体へ広く入れることはありません。

tailnet 全体を管理ネットワークとして扱いたい場合だけ、`tailscale0` を `mgmt` に置きます。

## 反映と確認

設定を確認してから routerd を再起動します。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
systemctl restart routerd.service
```

生成された systemd ユニットを確認します。

```sh
systemctl cat routerd-tailscale-edge.service
```

Tailscale 側の状態を確認します。

```sh
tailscale status --self --json | jq '.BackendState, .Self.AllowedIPs'
tailscale debug prefs | jq '.AdvertiseRoutes'
```

routerd 側の状態を確認します。

```sh
routerctl status --json
routerctl get TailscaleNode/edge -o yaml
routerctl tailscale peers
```

`routerctl tailscale peers -o json` は `tailscale status --json` を読み、peer 一覧を routerd の CLI 形式で表示します。Web Console の Resources でも `TailscaleNode` に peer の online 状態、relay、last seen、allowed route を表示します。

Web Console を Tailscale 経由で見たい場合は、ルーターの Tailscale アドレス、または承認済みの経路上のアドレスで確認します。

```sh
curl -f http://100.64.0.1:8080/
```

上のアドレスは例です。
実際のルーターの Tailscale IP に置き換えてください。

## 補足

- `acceptDNS: false` にすると、Tailscale がルーター自身の DNS 設定を置き換えません。routerd の基本方針は LAN DNS first です。`DNSResolver`、local zone、DHCP 由来レコード、conditional forwarding を LAN 側の権威として維持し、MagicDNS にホスト resolver を乗っ取らせません。
- `acceptRoutes: false` にすると、ルーターは他ノードの広告経路を取り込みません。
  経路を外へ広告するルーターでは、この設定が自然です。
- routerd は Tailscale peer の metric として `routerd.tailscale.peer.count` と `routerd.tailscale.last_handshake.seconds` を出します。Tailscale status の `LastSeen` を運用上の handshake age として使います。
- exit node と subnet route の承認は Tailscale 側で行います。
- auth key は examples や Git 履歴に残さないでください。
  実機では `authKeyFile` を使います。
