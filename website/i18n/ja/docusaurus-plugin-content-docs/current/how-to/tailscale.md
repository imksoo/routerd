---
title: Tailscale の exit node と subnet router
---

# Tailscale の exit node と subnet router

![TailscaleNode が tailscaled、auth key file、advertised subnet、exit-node intent、tailnet approval flow を扱う流れ](/img/diagrams/how-to-tailscale.png)

## 想定する構成

`TailscaleNode` は、routerd ホストを tailnet に参加させ、次の経路を広告したい場合に使います。

- exit node（`0.0.0.0/0` と `::/0`）
- 1 個以上の subnet route
- exit node と subnet route の両方

routerd は `tailscaled` を置き換えません。
routerd は `tailscale up` を実行する systemd ユニットを生成し、ノードの広告設定を管理します。
Tailscale のアカウント、制御プレーン、経路承認は Tailscale 側に残します。
routerd はホスト上の宣言設定を管理します。

## 設定の変更

routerd は宣言設定全体を `tailscale up --reset` で適用します。
`advertiseRoutes` を削除するか `advertiseExitNode: false` にすると、
過去の非既定フラグの省略エラーを起こさず、以前の広告を解除します。
省略した設定は Tailscale の既定値に戻るため、無効化を維持する設定は
`acceptDNS: false` などで明示してください。YAML にない手動の
`tailscale up` 設定には依存しないでください。ノードのログアウトや
強制再認証は行いません。経路の承認は引き続き tailnet 管理者が行います。

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

この設定を反映したあと、Tailscale の管理画面で広告された経路を承認します。
承認前は、`tailscale debug prefs` では要求した経路が見えます。
ただし、`tailscale status --self --json` の `Self.AllowedIPs` にはまだ出ないことがあります。

## ファイアウォールゾーンの置き方

`tailscale0` を `Interface` として宣言します。
これにより、状態と Web 管理画面の Interfaces に表示できます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: tailscale
spec:
  ifname: tailscale0
  mtu: 1280
  managed: false
```

`mtu: 1280` を指定すると、派生する TCP MSS clamp が Tailscale 経由の経路を考慮しつつ、
無関係な LAN から WAN への経路まで低い MTU に下げることはありません。

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

この構成では、tailnet のクライアントは `trust -> self` の経路で routerd の Web 管理画面などに到達できます。
一方で、ファイアウォールが `trust -> mgmt` の転送を拒否していれば、tailnet から管理 VLAN 全体へ広く入ることはできません。

tailnet 全体を管理ネットワークとして扱いたい場合だけ、`tailscale0` を `mgmt` に置きます。

## 反映と確認

設定を確認してから routerd を再起動します。

```sh
routerctl validate -f /usr/local/etc/routerd/router.yaml --replace
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
routerctl get status -o json
routerctl get TailscaleNode/edge -o yaml
routerctl vpn tailscale peers
```

`routerctl vpn tailscale peers -o json` は `tailscale status --json` を読み、ピア一覧を routerd の CLI 形式で表示します。Web 管理画面のリソース画面でも、`TailscaleNode` にピアのオンライン状態、relay、last seen、許可された経路を表示します。

Web 管理画面を Tailscale 経由で見たい場合は、ルーターの Tailscale アドレス、または承認済みの経路上のアドレスで確認します。

```sh
curl -f http://100.64.0.1:8080/
```

上のアドレスは例です。
実際のルーターの Tailscale IP に置き換えてください。

## 補足

- `acceptDNS: false` にすると、Tailscale がルーター自身の DNS 設定を置き換えません。routerd の基本方針は LAN の DNS を優先することです。`DNSResolver`、ローカルゾーン、DHCP 由来のレコード、条件付き転送を LAN 側の権威として維持し、MagicDNS にホストのリゾルバーを乗っ取らせません。
- `acceptRoutes: false` にすると、ルーターはほかのノードが広告する経路を取り込みません。
  経路を外へ広告するルーターでは、この設定が自然です。
- routerd は Tailscale ピアのメトリクスとして `routerd.tailscale.peer.count` と `routerd.tailscale.last_handshake.seconds` を出します。運用上のハンドシェイク経過時間としては、Tailscale status の `LastSeen` を使います。
- exit node と subnet route の承認は Tailscale 側で行います。
- auth key は examples や Git 履歴に残さないでください。
  実機では `authKeyFile` を使います。

## 接続の停止と撤去

Linux では `TailscaleNode` の削除、`state: absent` の指定、または `when` 条件が
偽になると、管理対象 unit を停止して `tailscale down` を実行します。
ログアウトや `tailscaled` 自体の停止は行わず、再有効化に使うノードの認証状態を
保持します。停止に失敗した場合は unit と撤去エラーを保持し、reconcile で再試行します。

撤去時は管理対象 unit の消失に加え、`tailscale debug prefs` の
`WantRunning: false` と `tailscale status --json` の `BackendState: Stopped`
を確認してください。routerd のリソース状態が空になっただけでは撤去の証明になりません。

隔離環境での相互運用確認には、独立した tailnet peer とルーター配下の SSH サーバーを
使います。制御プレーンで広告サブネットを承認し、peer で経路の受入れを有効化した後、
`ip route get <サーバーIP>` が `tailscale0` を選ぶことを確認します。
`nc -w 5 <サーバーIP> 22` で SSH バナーを受信できることが合格条件です。
Tailscale ping だけではサブネット転送の証明になりません。
`state: absent` と認証キーなしの再有効化後にも通信を確認し、Tailscale アドレスが
維持されることを検証します。最後にリソースを削除し、前述の停止状態を確認します。
本番経路ではなく、使い捨て可能な隔離ホストで実施してください。
