---
title: 設計メモ
---

# 設計メモ

![Diagram showing routerd design notes covering daemon contracts, DHCPv6-PD ownership, honest LAN advertisement, DS-Lite AFTR resolution, event coordination, reusable building blocks, and OpenRC rendering](/img/diagrams/design-notes.png)

このドキュメントは、routerd で残すべき設計判断の記録です。
過去の試行錯誤の時系列ログではなく、現在のコードが従っている原則と、今後の変更で守るべき指針だけを残します。

## 1. デーモン契約

状態を持つ処理は、専用デーモンが担当します。
ツール側が一様に扱えるよう、すべてのデーモンは同じインターフェースを公開します。

- Unix domain socket 上の HTTP+JSON API
- `/v1/status`
- `/v1/healthz`
- `/v1/events`
- `/v1/commands/reload`
- `/v1/commands/renew`
- `/v1/commands/stop`
- state または lease ファイル
- `events.jsonl` (append-only)

この契約は、`routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck` で共通です。

## 2. DHCPv6-PD

DHCPv6-PD は `routerd-dhcpv6-client` が所有します。OS 付属クライアント向けに設定を生成する経路は、もうありません。

通常の家庭用ゲートウェイ環境では、標準の solicit / advertise / request / renew と、lease 永続化、T1 renew で十分です。
壊れた環境を回避するための過剰な再送は、現在の方針では使いません。

## 3. 誠実な LAN 広告

DHCPv6-PD が `Bound` でない場合、routerd は LAN へ古い IPv6 情報を出しません。
これは RA、DHCPv6 サーバー、AAAA レコード、prefix から導出した LAN アドレスのすべてに当てはまります。
「壊れた状態を、壊れたまま正直に見せる」方針です。届かない prefix を配り続けることはしません。

## 4. DS-Lite

一部のアクセス網では、DHCPv6 の information-request で AFTR オプションが返りません。
そのため `DSLiteTunnel` は、`aftrFQDN` または `aftrIPv6` の静的指定を、フォールバックではなく正規の経路として扱います。

AFTR の FQDN は、公衆 DNS で解けないことが多いです。AFTR ドメイン用の `DNSForwarder` と、DHCPv6 の information status から事業者網内のリゾルバーを読み取る `DNSUpstream.addressFrom` で転送してください。

## 5. イベント連携

routerd はプロセス内バスを持ちます。controller はイベントを受けて、影響を受けるリソースだけを再評価します。

高位の連携には次の Kind を使います。

- `EgressRoutePolicy`
- `EventRule`
- `DerivedEvent`
- `HealthCheck`

`EventRule` は、イベント列を入力にして別のイベント列を生成します。
`DerivedEvent` は、観測した状態から asserted / retracted の仮想イベントを合成します。

## 6. Tier S 構成要素

WireGuard、Tailscale、IPsec、VRF、VXLAN は、Tier S（SOHO / branch）の構成要素です。
WireGuard と VXLAN-over-WireGuard は、対応 OS 間での相互運用を確認済みです。
`TailscaleNode` は exit node と subnet router の広告を扱います。
VPN をすべて抽象的な 1 種類の形に押し込まないためです。

抽象的な `VPNTunnel` リソースは作りません。
WireGuard、Tailscale、IPsec、将来の SoftEther 連携は、それぞれ別の Kind として追加します。
それぞれの状態機械が大きく異なり、
多態的な 1 つの Kind にまとめると意味を失うためです。

## 7. OpenRC サービスレンダリング

Alpine は systemd ではなく OpenRC を使います。
OpenRC 対応は、まず applier ではなく renderer として始めます。
`routerd render alpine --out-dir` は、レビュー可能な init script と関連設定を書き出し、routerd が OpenRC の状態を変更する前に、導入済みホストの挙動を確認できるようにします。

最初に対応する OpenRC の範囲は狭く保ちます。

- 明示的な `generated service artifacts` resource から OpenRC script への変換
- `routerd-healthcheck` script の自動生成
- DHCP または RA resource が dnsmasq を必要とする場合の、managed dnsmasq script の自動生成
- DHCPv4 / DHCPv6 client、firewall logger、PPPoE、Tailscale の script の自動生成
- DNS リゾルバ script。ただし resolver の runtime config を controller loop の外で materialize できるまでは enable / start しません

これは互換性の袋小路を避けるためです。
API の形は当面 `generated service artifacts` のままですが、OpenRC に写すのは init script の意味を明確に持つ field に限ります。具体的には `ExecStart`、`ExecStartPre`、environment、working directory、user/group、runtime/state/log directory です。
systemd sandboxing、networkd、resolved、timesyncd の意味は、OpenRC 上で模倣しません。

適用時の有効化処理は `HasOpenRC` で分岐します。
スクリプトは内容またはモードが変わる場合だけ書き込み、`rc-update show default` で登録状態を確認してから追加・削除を行い、`rc-service <name> status` を見てから start / restart / stop を実行します。
systemd 側と同じく、目標状態とファイルが変わっていない場合は、サービスマネージャのコマンドを重複実行しません。

次の実装段階は、Alpine の導入済みホスト向けスモークテストハーネスを通常の VM ジョブに昇格させることです。

## 8. 残課題

- 状態を持つ firewall の本番運用。`FirewallRule` は ICMP type 一致、
  複数 port、nftables rate limit、送信元ごとの connection limit を扱えます。
  今後は基本的な expression の網羅ではなく、rule grouping と上位 policy の
  使いやすさを改善します。
- LAN 向けの DoH 代理。
- BGP ピアのサブ秒障害検出のための BFD。
- YAML を編集せずに `routerctl` 経由で IngressService バックエンドのドレインモードを運用者が制御する機能。
- VRRP の `advertInterval`、`preempt`、`preemptDelay` チューニングの本番環境向け設定例の充実。
- `IngressService`、ローカルサービスリダイレクト、routerd 管理デーモン間のリッスンポート衝突の検証。
- デュアルスタック Kubernetes クラスター向けの IPv6 BGP と VRRPv3。
- Tier C に向けた、OSPF などの動的ルーティングの統合。
- 高可用性（リーダー選出、耐障害のコントロールプレーン）。
- 本番向けの観測性（OpenTelemetry コレクターとリモートログシンク）。
- 家庭用回線で routerd を唯一の WAN ルーターとして長時間運用する検証。
