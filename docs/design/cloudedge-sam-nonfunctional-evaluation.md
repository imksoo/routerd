# CloudEdge SAM 非機能評価 (2026-06)

本書は CloudEdge SAM(Selective Address Mobility)provider fabric を、4 クラウド
(AWS / Azure / OCI / PVE)full topology(2RR + 8 leaf + 8 pseudo-client)で実機検証した
際の **非機能特性**(フェールオーバー収束時間・スループット・レイテンシ・フェールオーバー挙動)を
まとめたもの。機能正当性(56/56 hostname matrix)は別途検証済みで、本書はその副産物として
取得したエビデンスを評価したものである。

## 対象・データ範囲

- routerd: main `48fb4d55`(provider owner coalescing 修正 #610 を含む)。
- 実行: `tests/e2e/sam` harness、full topology、`sam-full-validation.sh`。
- エビデンス:
  - baseline: `sam-full-48fb4d55-20260620T175450Z/full-validation-firewall-fix/baseline`
  - RR failover/rejoin: 同 run の `rr-failover-aws-rr-a` / `rr-failover-aws-rr-b`
  - 性能比較(SAM vs public 直結): 各 scenario の `performance/*/comparison.tsv`

> **データ範囲の注意**: leaf failover/rejoin 8 シナリオはコスト都合で打ち切ったため
> 本書には未収録(別途フルトポロジーで取得予定)。capture(secondary IP)分散の
> 偏りは [issue #613](https://github.com/imksoo/routerd/issues/613) で別管理。
> 性能値は単発スナップショットであり、クラウド側の変動を含む参考値である。

## 1. フェールオーバー / 収束時間

SAM の収束ゲート(全 leaf が期待状態へ到達するまで)の経過秒数:

| イベント | 収束ゲート経過 |
|---|---|
| baseline initial | 14 s |
| RR failover (aws-rr-a 停止後) | 14 s |
| RR rejoin (aws-rr-a 復帰後) | 16 s |
| RR failover (aws-rr-b 停止後) | 15 s |
| RR rejoin (aws-rr-b 復帰後) | 16 s |

- RR(route reflector)の停止・復帰いずれも **約 14〜16 秒**で収束ゲートを通過。
- この値は収束ゲートの通過所要(再収束の上限の目安)であり、データプレーン切替の
  厳密な瞬時値ではない点に留意。
- leaf failover の収束時間は本 run では未取得(データ範囲参照)。

## 2. スループット(SAM overlay vs public 直結)

クロスクラウド pair の TCP/UDP を SAM 経由と public 直結で比較(iperf3、UDP は 10 Mbps 目標)。

| 経路 | SAM TCP | public TCP | SAM/public |
|---|---|---|---|
| aws ↔ azure | 約 740–880 Mbps | 約 1.85–2.30 Gbps | **約 38–40%** |
| aws ↔ oci | 約 466–555 Mbps | 約 736–771 Mbps | 約 62–72% |
| azure → aws | 約 624–770 Mbps | 約 916–921 Mbps | 約 68–84% |
| azure ↔ oci | 約 497–603 Mbps | 約 727–743 Mbps | 約 68–82% |
| oci → aws | 約 270–522 Mbps | 約 733–741 Mbps | 約 37–71% |
| oci → azure | 約 452–524 Mbps | 約 731–736 Mbps | 約 62–71% |

- **TCP は SAM 経由で public 直結の概ね 40〜80%**。overlay encapsulation と leaf hop の
  オーバーヘッドによる。public 直結が最速の経路(aws↔azure、約 2.3 Gbps)ほど相対差が
  大きく出る(SAM 側は概ね 1 Gbps 未満で頭打ち)。
- **UDP は 10 Mbps 目標の範囲で SAM/public ほぼ同等**(両者 ≈ 10 Mbps、loss 0%)。
- **同一クラウド内**の SAM TCP は高速(例: aws-a→aws-b 約 4.9 Gbps)。overlay の
  コストはクロスクラウド経路で顕在化する。

## 3. レイテンシ / パケットロス

- ping loss: 全 pair で **0%**(SAM / public とも)。
- SAM 経路 RTT 例: aws-client-a → oci-client-a で **min/avg/max = 3.59 / 4.04 / 7.42 ms**。
- 経路は client → ローカル leaf → リモート leaf → client の概ね 3 hop(traceroute で確認)。
  overlay による hop 追加分のレイテンシが乗るが、クロスクラウドでは数 ms 程度。

## 4. フェールオーバー挙動(新規フロー vs 既存 TCP)

active leaf 停止時の挙動を区別して評価:

- **新規フロー(停止後に開始)**: 収束後に **PASS**。ping / 64 MiB HTTP curl / SSH hostname
  検証 / tracepath いずれも生存系 leaf 経由で成功。→ フェールオーバーは機能する。
- **既存の長寿命 TCP(停止前から継続)**: active leaf 停止を **またげず中断**(throttled
  HTTP transfer が約 2.4 MiB で rc=124 timeout)。
  → 既存 TCP セッションの failover 越え継続性は未達。[issue #612](https://github.com/imksoo/routerd/issues/612)
  で別管理(非ブロッカー、reset → 再接続セマンティクスの可能性)。

## 5. 既知の制約 / 今後

- **capture(secondary IP)分散の偏り**: 現 config では capture が片 leaf に集中
  (例 aws-leaf-a:leaf-b = 17:1)。distributed capture mode は実装済みだが E2E config で
  未有効化。定常半々 + 障害時全量 failover + no-preempt を満たす改善を
  [issue #613](https://github.com/imksoo/routerd/issues/613) で対応中。
- **leaf failover/rejoin の収束時間**: 本 run 未取得。次回フルトポロジー実機で取得予定。
- 性能値は単発スナップショット。継続的なベンチではない。

## まとめ

- フェールオーバー(RR)収束は約 14〜16 秒、新規フローは確実に切替わる。
- SAM overlay の TCP スループットは public 直結の約 40〜80%(クロスクラウド)、UDP は同等、loss 0%、RTT は数 ms 増。
- 既存長寿命 TCP の failover 越え継続性(#612)、capture 分散の均し(#613)が今後の改善点。
