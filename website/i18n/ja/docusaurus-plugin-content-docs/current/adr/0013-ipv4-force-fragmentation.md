# ADR 0013: 信頼されたオーバーレイパスでの IPv4 強制フラグメンテーション

![ADR 0013 の図。通常の MTU 導出と MSS clamping から、非 TCP DF ブラックホールリスク、明示的な trusted-overlay の routerd_forcefrag 処理まで](/img/diagrams/adr-0013-ipv4-force-fragmentation.png)

## ステータス

プレリリース実装として承認済み。

## 背景

routerd は既にトンネルとフォワーディングの意図からパス MTU 処理を導出している。
通常の緩和策は TCP MSS clamping：Linux 上では `routerd_mss` が、導出された低 MTU
フォワーディングパスの TCP SYN MSS を、ファイアウォールゾーンを必要とせずに書き換える。

MSS clamping は非 TCP トラフィックには効果がない。DF ビットが設定された、サイズ超過の
UDP、QUIC、ICMP、その他の IPv4 パケットは、信頼されたオーバーレイやアンダーレイの
実効 MTU が低い場合に、PMTUD フィードバックがブロックされたり無視されたりすると
ブラックホールになりうる。

DF のクリアは一般的なインターネットのデフォルトではない。送信側の明示的なパス MTU 選好に
反し、フォワーディングコストの高いフラグメントを生成してドロップされやすくする可能性がある。
したがってこの機能は明示的、パススコープ、デフォルトオフでなければならない。

## 決定

オーバーレイパス MTU の意図に明示的な IPv4 強制フラグメントオプションを追加する：

- `OverlayPeer.spec.pathMTU.forceFragmentIPv4`
- `TunnelInterface.spec.pathMTU.forceFragmentIPv4`

この機能は、routerd が転送パスと実効 MTU を導出できる信頼された routerd
オーバーレイデバイスでのみサポートされる：`wireguard`、`ipip`、`gre`、`fou`、`gue`。
強制フラグメンテーション有効時に `route`、`tailscale`、`ipsec`、その他の
アンダーレイタイプのバリデーションは拒否する。

Linux では、routerd は専用の nftables テーブルをレンダリングする：

```text
table ip routerd_forcefrag {
  chain forward {
    type filter hook forward priority mangle; policy accept;
    iifname <capture> oifname <tunnel> ip length > <path-mtu> ip frag-off 0x4000 ip frag-off set 0
  }
}
```

マッチは IPv4 のみで、導出された転送パスにスコープされる。現在フラグメント化されていない
DF パケットのうちサイズ超過のもののみ DF をクリアする。その後カーネルが通常のインターフェース
MTU に従って egress デバイスでフラグメントする。

TCP MSS clamping は引き続き TCP の主要な緩和策。強制フラグメンテーションは、
明示的に信頼されたパス上の非 TCP または不適切にサイズ設定されたトラフィックに対する
キャッチオール。

## 代替案

- **Route MTU lock。** routerd が所有するルートにはより標準的だが、
  BGP インポートのモビリティパスを含む全ルートオリジンをクリーンにカバーしない。
  ポリシーがルートライター間に分散する。
- **iptables。** 既存のストックターゲットは、DF クリアのクロスパス表現で
  nftables よりクリーンなものを提供しない。
- **最初のフェーズで FreeBSD pf。** pf には `scrub ... no-df` があるが、
  routerd のライブ SAM/オーバーレイデータプレーンは Linux ファースト。
  FreeBSD サポートはパリティを暗黙に装うのではなく、後のフェーズに残す。

## 結論

- デフォルト動作は変更なし。
- Linux は `routerd_mss` の隣に 2 つ目のルーター所有パス MTU nftables テーブル
  `routerd_forcefrag` を得る。
- オペレーターはオーバーレイパスまたはトンネルインターフェースごとにオプトインが必要。
- フラグメンテーションはスループットを低下させ、パケットロス感度を高める可能性がある。
  ドキュメントでは、信頼されたオーバーレイの PMTU ブラックホール対処の最終手段として
  説明すべき。
