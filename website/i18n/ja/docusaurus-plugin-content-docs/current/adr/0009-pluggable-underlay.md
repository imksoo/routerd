# ADR 0009: プラガブルオーバーレイアンダーレイ（ipip / gre、次に fou / gue）

![ADR 0009 の図。TunnelInterface、IPIP または GRE 配送、オプションの WireGuard 暗号化アンダーレイ、MTU オーバーヘッド導出、MSS clamp の安全性](/img/diagrams/adr-0009-pluggable-underlay.png)

## ステータス

提案済み。
実験的実装として承認（2026-06-01）。

CloudEdge オーバーレイ/SAM データプレーン（[ADR 0006](../adr/0006-event-federation.md)、
[Selective Address Mobility](../reference/selective-address-mobility)）と
ゾーン非依存の PMTU/MSS clamp（#53/#68）を土台とする。
実験的。

## 背景

CloudEdge オーバーレイ（`OverlayPeer`）は現在、唯一の実装されたアンダーレイとして
**WireGuard** を使用している。
*信頼された*プライベートアンダーレイ（ExpressRoute、DirectConnect、FastConnect、VPC/VNet ピアリング）上では、「WireGuard」の暗号化は冗長であり、
約 80 バイトのオーバーヘッドは純粋なコストになる。
アンダーレイが既に信頼されている場合に、アドレスの配送方法を**変えることなく**、オペレーターがより軽量で低オーバーヘッドな L3 トランスポートを選べるようにしたい。

オーバーレイは既に適切なシームで抽象化されている（コードで確認済み）：

- **配送はアンダーレイ非依存。** `hybrid.RouteTarget(peer)` が
  `OverlayPeer.Underlay.Type` を `(device, gateway)` にマップし、`/32` 配送ルート
  （`RemoteAddressClaim` / `HybridRoute`）がそのデバイスを指す。
  トランスポートの追加は新しい `switch` ケース。
- **MTU / MSS clamp はパラメーター化済み。** `hybrid.EstimateMTU = underlayMTU(interface)
  − overheadFor(type)`。
  ゾーン非依存の clamp は `EstimateMTU` に従う。
  新しいトランスポートはオーバーヘッド値とインターフェース MTU さえあれば、clamp は自動追従する。

唯一の実質的なギャップ：**デバイス作成が WireGuard 固有**（専用の
`WireGuardInterface` Kind + コントローラー）。
新しい L3 トランスポートには、「トンネルデバイスを作成する」同等のリソース + コントローラーが必要。

## 決定

### 新規 Kind `TunnelInterface`（`hybrid.routerd.net/v1alpha1`）

`WireGuardInterface` のミラー：1 つの OS トンネルデバイスの desired state を所有するリソース。
`OverlayPeer.Underlay` は*配送選択*の参照のまま。
`TunnelInterface` は*デバイス desired state* を持つ。
この分離により、`OverlayPeer` のインラインフィールドがピアごとにデバイス仕様を増殖させ、デバイスの所有権/冪等性/削除を曖昧にする問題を回避できる。

Phase 1 のフィールド：

- `mode`: `ipip | gre`。
- `local`、`remote`: アンダーレイ（物理）エンドポイント IP（必須）。
- `address`: オーバーレイの内側アドレス（オプション。それ以外は WireGuard と同様に
  `ipv4-static-address` コントローラーが設定）。
- `mtu`（オプション）、`ttl`（オプション、デフォルト 64）、`key`（GRE のみ。設定時は
  +4 オーバーヘッド）。
- `trustedUnderlay: true`（必須。安全性を参照）。

Phase 2 で同一 Kind を IPIP-over-UDP に拡張：

- `mode`: `fou | gue` は Linux UDP カプセル化（`encap fou` または `encap gue`）付きの
  `ipip` トンネルデバイスを意味する。
- `encapSport`、`encapDport`: ローカル UDP ソース/リッスンポートとピア宛先ポート。
  `fou`/`gue` では両方必須。

`OverlayPeer.Underlay.Type` enum に `ipip`、`gre`、`fou`、`gue` を追加。
`.Interface` が `TunnelInterface` を名前で参照する。

### 新規コントローラー `tunnel`

`TunnelInterface` を reconcile する `framework.FuncController`（Phase 1 では Linux のみ。
他のプラットフォームではチェーンをエラーにするのではなく unsupported ステータスを報告）：

- **argv ベースの `ip` 呼び出し**（文字列連結シェルではない）。
  `ip link show` → add/modify/`ip link del` で冪等：
  - `ip link add <dev> type ipip|gre local <L> remote <R> ttl <t> [key <k>]`
  - `fou`/`gue` の場合：`ip fou add port <sport> ipproto 4|gue`、次に
    `ip link add <dev> type ipip local <L> remote <R> ttl <t> encap fou|gue
    encap-sport <sport> encap-dport <dport>`
  - `ip link set <dev> mtu <m> up`
- アドレスは既存の `ipv4-static-address` コントローラーが処理（WireGuard と同様）。
- ステータス: phase、device、mode、local、remote、mtu。

### オーバーヘッド、配送、MTU

- `overheadFor`: `ipip = 20`、`gre = 24`（外側 IPv4 20 + GRE base 4）、`fou = 28`
  （外側 IPv4 + UDP）、`gue = 32`（外側 IPv4 + UDP + 最小 4 バイト GUE ヘッダー）。
  GRE `key` で +4。
- `RouteTarget`: `ipip`、`gre`、`fou`、`gue` → `(device, "")` （`/32` ルートは
  WireGuard と同様にトンネルデバイスを指す）。
- `EstimateMTU` と PMTU/MSS clamp は自動追従する。
  `pathMTUResourceMTU` フォールバックに `TunnelInterface` デフォルトを追加（または `spec.mtu` が反映される）。

### バリデーション

- `OverlayPeer.Underlay.Type` enum += `ipip`、`gre`、`fou`、`gue`。
- `TunnelInterface`: `mode ∈ {ipip, gre, fou, gue}`。
  `local`/`remote` 必須、有効な IP。
  `trustedUnderlay == true` 必須（それ以外は明確なメッセージで拒否）。
  MTU/TTL/key/encap ポートの範囲チェック。

## 安全性（ハードな不変条件）

`ipip`、`gre`、`fou`、`gue` は**暗号化も認証もされない**。
WireGuard とは性質が異なり、既に信頼されたアンダーレイ上でのみ安全である。

- **WireGuard がデフォルトのまま。**
- `TunnelInterface` は **`trustedUnderlay: true`** を設定しない限り拒否される。
  アンダーレイが平文であることのオペレーターによる明示的な確認であり、ドキュメント/doctor の警告だけでは弱すぎる。
  これはバリデーションゲートである。

## フェーズ分割

- **Phase 1**: `TunnelInterface` Kind + `tunnel` コントローラー
  （Linux `ipip`/`gre`）+ `trustedUnderlay` ゲート + `RouteTarget`/オーバーヘッド/MTU +
  バリデーション + ユニット/fixture テスト + サンプル設定。
  テストには**削除順序**の不変条件を含む：`OverlayPeer`/claim の削除で `/32` ルートが落ち、
  `TunnelInterface` の削除でデバイス削除プランが出力される。
  ルートインストールはデバイスが存在しない場合を許容する必要がある。
- **Phase 2（実装済み）**: `fou` / `gue`（IPIP-over-UDP）。
  GRE-over-FOU/GUE は意図的に公開しない。
  inner-mode フィールドまたは複合タイプ文字列が必要になるため。
  `ip fou add` の encap-port セットアップを追加。
  最小ヘッダーオーバーヘッドの仮定を既存の明示的 `mtu` エスケープハッチとともにドキュメントする。
- **Phase 3**: FreeBSD（`gif` for ipip、`gre`）。
  設定/ステータスの表面が異なるため、Linux コントローラーに詰め込まない。
- **Phase 4**: ファイアウォール自動穴あけ（raw `ipip` = IP proto 4、`gre` = IP proto 47、
  `fou`/`gue` = UDP）+ `doctor hybrid` チェック。

## 結論

- オペレーターは信頼されたアンダーレイ向けに軽量なオーバーレイトランスポートを得る。
  配送と MSS clamp は変更なく、新しいオーバーヘッドに自動追従する。
- 暗号化のトレードオフは明示的でゲート付き（`trustedUnderlay: true`）であり、
  信頼されていない経路上で軽量トランスポートが誤って選択されることはない。
- `TunnelInterface` は汎用的なデバイス desired state リソースであり、
  Phase 2-3 で配送/MTU のシームに触れることなく拡張（encap、FreeBSD）できる。
- WireGuard の動作や既存デプロイメントへの変更はない（デフォルト不変）。
