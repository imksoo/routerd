# ADR 0015: hub/spoke ブートストラップ向け WireGuard peer enrollment

## ステータス

提案 — 2026-06-09。

関連 issue: #377。

## 背景

`WireGuardInterface.spec.peersFrom` は、共有された `SAMNodeSet` から WireGuard
peer を導出できる。すべての router が信頼済み node registry をすでに持っている場合、
静的 peer 記述の重複は大きく減る。

ただし、hub/spoke のブートストラップはこれだけでは完了しない。Route Reflector または
spine 構成では、leaf router が固定 RR/spine endpoint に向けて WireGuard traffic を
開始することが多い。RR/spine 側の kernel は、peer を受け入れる前に各 leaf の public
key、allowed IP、必要に応じて endpoint を知っている必要がある。つまり `pve-rt`
のような leaf を増やすたびに、RR/spine 側の正本を更新する運用負荷が残る。

初回接続の経路に対象の WireGuard tunnel は使えない。WireGuard は未知の peer を
application protocol より前で破棄するため、enrollment は management address、
underlay listener、または事前に確立済みの別 control channel など、別の bootstrap
transport を使う必要がある。

## 決定

hub/spoke 構成向けに、任意有効化の WireGuard peer enrollment flow を追加する。

RR/spine router は、明示設定された non-WireGuard の listen address と port で
enrollment endpoint を公開できる。leaf は node identity と WireGuard peer material
を送信し、RR/spine は local policy と期待 topology に照らして検証した後に peer を
有効化する。

enrollment record には次の情報を含める。

- `nodeRef` と対象 WireGuard interface
- WireGuard `publicKey`
- leaf が安定 endpoint を持つ場合の endpoint または listen port
- 要求する `allowedIPs` または `samEndpoint`
- retry を冪等にし、古い write を検出するための nonce または generation

承認済み registration は、config graph から見えない場当たり的な runtime state ではなく、
dynamic config として保存する。effective config path はその record を通常の WireGuard
peer input に変換する。変換先は生成 `WireGuardPeer` resource、または既存の
`WireGuardInterface.spec.peersFrom` が消費できる entry とする。静的 `WireGuardPeer`
resource は引き続き名前単位で生成 peer を上書きできるため、運用者は緊急 override を
保持できる。

leaf の静的 bootstrap config は小さく保つ。必要なのは自分の private key、RR/spine の
public key と固定 endpoint、enrollment credential である。承認と有効化は RR/spine が
所有する。

## 検証とセキュリティ

enrollment は fail closed とする。設定されたすべての検査に通った場合だけ要求を受け入れる。

- enrollment endpoint はデフォルト無効とし、設定された address だけに bind する。
- bearer token、mTLS client identity、署名付き registration payload などの明示的な方式で認証する。
- 要求された `nodeRef` は policy で許可され、設定されている場合は期待する `SAMNodeSet` に存在する必要がある。
- 要求された `allowedIPs` と `samEndpoint` は node identity と一致し、既存 node と衝突しない。
- public key は一意とする。ただし同じ node が同じ generation を retry している場合は例外とする。
- 再登録、key rotation、却下、失効、有効期限切れは audit/status output で確認できる。
- rate limiting により、無効な registration の連続送信から bootstrap endpoint を守る。

`routerctl` は enrollment state を `Pending`、`Approved`、`Rejected`、`Revoked`、
`Expired` として表示し、有効でない request には検証理由を添える。

## 非目標

- WireGuard cryptokey routing を置き換えない。RR/spine は承認済み leaf ごとに kernel peer を持つ。
- 明示的な policy decision なしに任意の public key を受け入れない。
- 初回 enrollment を対象 WireGuard interface の中で実行しない。
- `SAMNodeSet` 配布を、新しく enroll する peer が必要な tunnel に依存させない。

## 実装計画

1. enrollment API resource 形状、status model、CLI/status output を定義する。WireGuard runtime reconcile とは分離する。
2. RR/spine 側の enrollment storage を dynamic config source として追加し、永続 audit と stale entry cleanup を持たせる。
3. policy と任意の `SAMNodeSet` membership に基づく validation を追加する。
4. static peer override の挙動を保ちながら、承認済み registration を既存の effective WireGuard peer generation path に流す。
5. boot 時に安全に実行できる、冪等な leaf-side submit/retry logic を追加する。
6. 失効と key rotation flow を追加する。

## 影響

承認済み enrollment policy がある構成では、leaf を 1 台追加するたびに RR/spine config に
WireGuard peer block を手書きする必要がなくなる。kernel peer 数と identity validation
の必要性は残るが、運用は各 RR/spine の peer material 編集から、leaf registration の承認
または事前許可へ移る。

この機能により bootstrap boundary も明確になる。topology distribution は `SAMNodeSet` と
`peersFrom` を使い続け、first-contact trust は明示的な non-WireGuard enrollment surface
で扱う。
