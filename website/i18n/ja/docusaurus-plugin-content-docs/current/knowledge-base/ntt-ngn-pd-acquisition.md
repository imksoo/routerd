# NTT NGN HGW 配下での連続稼働中 PD 取得

このページは 2026-04-30 のラボ調査をまとめたものです。
NTT NGN（フレッツ光ネクスト）配下の PR-400NE HGW が長時間連続稼働している状態で、
新規 DHCPv6-PD クライアントが prefix を取りに行ったときに何が起きるかを記録します。

姉妹ページ
[DHCPv6-PD クライアントの実装と選び方](./dhcpv6-pd-clients.md)
がクライアント側の実装選択を扱うのに対し、こちらはラボの実証データから
逆算したサーバ側（HGW）の状態機械を扱います。

同じ環境を再構築する人が同じ躓き方をしないことが目的です。
証拠の段階付けは `docs/design-notes.md` のラベル
（assert / believe / observe / measure / cite）に従います。

## 背景とラボ構成

- HGW: NTT PR-400NE。NTT NGN の native IPv6 IPoE 経路。
- 参照機: 既存 /60 PD lease を持つ NEC IX2215。
- 被試験機: routerd lab VM 3 台（`router01`、`router02`、`router03`）。HGW と IX2215 と同じ LAN セグメント上。
- HGW LAN 側 MAC（lab 値）: `1c:b1:7f:73:76:d8`。
- HGW LAN 側 RA フラグ: `M=0`、`O=1`（`Flags [other stateful]`）。prefix info は HGW 自身の /60 から派生する /64 SLAAC。
- HGW の LAN PD 払い出し表は最大 15 slot（`1/15`、`2/15`、...、`15/15`）。
- HGW はテストセッションの数日前から無停止で稼働。セッション中は再起動していない。

## A. 連続稼働中の HGW では Solicit 経路が信頼できない

- **observe**: routerd lab VM から 34 種類の Solicit variant を送ったが、Advertise/Reply は 1 つも返らなかった。試した variant の例:
    - DUID-LLT / DUID-LL / DUID-EN（NTT NGN 関連の enterprise number 付き）。
    - MAC OUI sweep（Yamaha、NEC、IX 系、Cisco、Allied Telesis、Apple、Intel、Realtek、locally-administered random）。
    - Vendor-Class option（option 16, NEC IX 系文字列）の有無。
    - User-Class option（option 15）の有無。
    - hop-limit を 1 / 64 / 128 / 255 に変える。
    - flow-label を 0 とランダムで切り替える。
    - IA_PD に IA Prefix hint を入れる / 入れない。
    - IA_PD と IA_NA を同梱する / しない。
    - RS→RA→Solicit の順序、間隔 0.5s / 1s / 2s / 5s。
    - Solicit 単発と RFC 8415 §15 の retransmit cadence (IRT/MRT/MRC) の両方。
- **measure**: 同じ瞬間、IX2215 は T1 周期で既存 lease を更新しており、テスト時間中 11/11 すべて成功していた。
  HGW の DHCPv6 サーバ自体は健全である。
- **believe**: HGW は次の 2 経路を独立に開閉していると推定する。
    - **取得経路（acquisition path）**: HGW 再起動直後の数分間だけ開く。
      新規 Solicit を受理し Advertise/Reply を返す。15 slot 表はここで埋まる。
    - **維持経路（maintenance path）**: 常時開いている。
      有効な Server Identifier と IA_PD claim を伴う Renew/Rebind/Confirm/Information-Request/Request だけを受理。
      新規 Solicit は受け付けない。
- **assert**: HGW が長時間稼働している状態では Solicit 成功を前提にできない。
  bootstrap は B 節の Request 経路に倒すか、acquisition window を再オープンするための out-of-band トリガ
  （HGW 再起動）を用意する必要がある。

## B. Request 直接 bootstrap（RFC 8415 §18.2.10.1 INIT-REBOOT 準拠 + 拡張）

ラボでの確認の結果、Solicit が silent drop されている状態でも `Request`（msg-type 3）には HGW が応答することが分かった。
HGW を再起動できない場面では、これが routerd の bootstrap 経路となる。

### B.1 IX2215 での再現

- **measure**: IX2215 で `clear ipv6 dhcp client GigaEthernet0.0` を実行すると、IX2215 は Solicit を出さず
  （INIT-REBOOT 経路）、キャッシュしていた `Server Identifier` と既存 `IA_PD` を載せた Request だけを送出した。
  HGW は Reply を返し、同じ /60 binding が復旧した。
- **observe**: IX2215 のログ上は Advertise の段が無いまま「lease confirmed」相当のイベントが記録される。

### B.2 routerd lab VM での再現

- **measure**: routerd lab VM（`router01`、`router02`、`router03`）から、
  scapy + raw socket で UDP 547 multicast `ff02::1:2` 宛に Request を合成して送出した。中身は次のとおり。
    - `xid`（24bit ランダム）。
    - Client `DUID-LL`（`0003 0001` + VM の MAC）。
    - Server Identifier `DUID-LL`（`0003 0001` + HGW MAC `1c:b1:7f:73:76:d8`）。
    - 非ゼロ T1/T2 と /60 の IA Prefix hint を持つ `IA_PD`。
    - `Elapsed Time = 0`。
    - NTT 系で実績のある set の `Option Request Option`（ORO）。
    - `reconfigure-accept` option（option 20）。
- **measure**: HGW は VM ごとに Reply を返し、空きスロットから新規 /60 binding を割り当てた。
    - `router01` → `2400:xxxx:xxxx:1220::/60`（slot N/15）。
    - `router03` → `2400:xxxx:xxxx:1240::/60`（slot N+2/15）。
    - `router02` → `2400:xxxx:xxxx:1260::/60`（slot N+4/15）。
  （site prefix の上位 32bit は事業者割当のため公開ドキュメントでは伏せる。/60 境界はラボで観測したまま。）
- **observe**: HGW は 15 slot 表の次の空きを選んだ。hint された prefix が他のクライアントに払い出し済みなら hint を尊重しない。
- **observe**: `router03` が同じ IAID と同じ `1230::/60` claim で 3 回連続で Request を出したところ、
  HGW は `1230` / `1240` / `1250` の 3 つを別 binding として払い出した。
  HGW 側に「IAID 単位で merge」する挙動はない。Request ごとに別 slot を消費する。
- **measure**: 不要 binding は適切な Server Identifier、IA_PD、prefix を載せた multicast `Release` で確実に消去できた。
  HGW を再起動する必要はない。

### B.3 routerd への含意

- **assert**: routerd の NTT WAN プロファイルは次の条件で Request を直接合成する `bootstrap_via_request` を持つ。
    - on-disk の lease state が無い、または、
    - キャッシュ lease が古く Solicit retry が MRC を超えて失敗した。
- **assert**: routerd は前の binding を解放せずに Request retry をして slot を多重消費しないように
  ガードしなければならない。IAID は dedup 用に機能しない。HGW は Request ごとに別 slot を払い出す。

## C. RA からの Server Identifier 導出（過去の DHCPv6 履歴なし）

- **observe**: HGW の `Server Identifier` は `0003 0001` + HGW LAN 側 MAC の `DUID-LL`。
  ラボでは `0003 0001 1c b1 7f 73 76 d8`。
- **observe**: HGW の RA 送信元 link-local は同じ MAC から EUI-64 で派生した
  `fe80::1eb1:7fff:fe73:76d8`。
- **measure**: RA 送信元 LL に modified-EUI-64 逆変換をかけると、MAC が一致するバイト列が復元できた。
  そこに `0003 0001` を前置すると、HGW が DHCPv6 Reply で実際に出している `Server Identifier` と一致した。
- **assert**: routerd の controller は RA 1 通の観測だけで、過去の DHCPv6 ラウンドトリップ無しに
  HGW の `Server Identifier` を計算できる。これにより B 節の Request bootstrap がコールドスタートからでも使える。
- **assert**: リソース仕様で `serverID` を override できる。HGW の振る舞いがデフォルトと違うサイトでは明示的に固定可能。

## D. 維持経路での Renew 受理条件

このネットワークで一番混乱を生むのが HGW の Renew 受理条件である。
セッションでは 3 系統の Renew をバイトレベルで取得し、次の仮説を得た。

### D.1 unicast Renew は常に UseMulticast bounce を返す

- **measure**: HGW のグローバルアドレス宛 UDP 547 に Renew を unicast 送出すると、約 4ms 以内に
  `status-code 5 (UseMulticast)` の Reply が返り、`xid` は echo される。
- **measure**: WIDE `dhcp6c` は同じ `xid` のまま multicast `ff02::1:2` に再送するが、HGW は応答しない。
  推定原因は multicast 経路での xid replay サプレッション。
- **assert**: routerd の controller はこの HGW では unicast Renew を使わない。
  常に新規 xid で multicast Renew する。

### D.2 成功した multicast Renew

IX2215 の lease 更新トランスクリプトから読み取れる成功する Renew の形:

| フィールド | 値 |
| --- | --- |
| `xid` | Renew ごとに新規 |
| `T1` | 7200 |
| `T2` | 12600 |
| `IA_PD` | bound prefix、`pltime`/`vltime` は非ゼロ |
| `reconfigure-accept` | 含まれる |
| `IAID` | 1568088（IX2215 ラボ値） |

### D.3 HGW が silently drop した multicast Renew

| 実装 | xid | T1/T2 | reconfigure-accept | 結果 |
| --- | --- | --- | --- | --- |
| WIDE `dhcp6c`（UseMulticast bounce 後） | reuse | 7200 / 12600 | 無し | silent drop |
| routerd active controller（初期実装） | 新規 | 0 / 0 | 無し | silent drop |

### D.4 仮説

- **believe**: HGW は次の **すべて** が同時に満たされる multicast Renew だけを受理する。
    1. xid が新規（このクライアントから過去に使われていない）。
    2. T1 と T2 が非ゼロ。IX2215 の観測値は T1=7200、T2=12600。
    3. `reconfigure-accept` option（20）が含まれる。
- **assert**: ablation で否定されるまで routerd は 3 つすべてを満たした Renew を送る。
  ablation は要素を 1 つずつ落として行い、結果は本ページに追記する。

## E. WIDE `dhcp6c` の HGW 固有の落とし穴

- **observe**: `dhcp6c` はサーバ link-local をキャッシュすると Renew を unicast で先に送る。
  これにより毎回 D.1 の UseMulticast bounce 経路に落ちる。
- **observe**: `dhcp6c` は送信する Solicit/Renew に `reconfigure-accept` を含めない。
  option 20 の受信処理は実装されているが送信処理は無い。
- **observe**: `dhcp6c` の DUID 保存先:
    - FreeBSD: `/var/db/dhcp6c_duid`
    - Ubuntu: `/var/lib/dhcp6/dhcp6c_duid`
- **assert**: `dhcp6c` 設定で `DUID-LL` を強制するときは `hardware-type` を `0x0001`（Ethernet）にする。
  それ以外の値は HGW が暗に求める形を満たさない。

## F. PR-400NE 固有の運用上の注意

- **measure**: HGW の `Reply` パケットは UDP 宛先ポート 546 に届くが、送信元 UDP ポートは ephemeral
  （ラボ観測値: 49153）。`udp port 547` だけの pcap フィルタは Reply の片道を取りこぼす。
  この HGW の DHCPv6 トラブルシューティングでは `udp port 546 or udp port 547` を使うこと。
  rixwwd 公開ノートと整合する。
- **measure**: HGW LAN /60 PD 払い出し表は最大 15 slot。ステータス画面では `1/15`、`2/15`、...、`15/15` と表示される。
  満杯になると新規取得には既存 slot の Release か HGW 再起動が必要。multicast Release で消去できる。
- **observe**: HGW LAN 側の RA は `Flags [other stateful]`（M=0、O=1）で、prefix info は /64 SLAAC。
  stateful な情報（DNS、NTP）が必要なホストは SLAAC と並行して Information-Request DHCPv6 を打つ。
- **observe**: HGW はクライアントの IA Prefix hint が既に他で使われているとき、その hint を尊重せず
  次の空き slot を返す。

## G. Reconfigure key の扱い

- **observe**: 新規取得後の最初の Reply には次の `Authentication` option が含まれる。
    - `protocol = reconfigure`
    - `algorithm = HMAC-MD5`
    - `RDM = mono`
    - `RD` フィールド: 8 バイト
    - `reconfig-key value`: 16 バイト
- **observe**: 同じ binding の Renew Reply には Authentication option が含まれないことがある。
  HGW が新しい鍵を発行するまで前の鍵が有効である。
- **assert**: routerd は最初の Reply で得た Reconfigure key を保持し、
  将来 HGW が当該 binding に対して送ってくる Reconfigure（RFC 8415 §18.2.10）の認証に用いる。

> 注: 実際の `reconfig-key value` は本ドキュメントには載せない。
> HGW ごとに異なるため定数として記録する意味がなく、不要なシークレット露出を避けるため。
> binding ごとの状態として扱うこと。

## H. routerd active controller を持つ理由（セッションの総括）

- **assert**: OS 標準の DHCPv6 client は canonical な Solicit-first の RFC 経路に従うため、
  ここで記録した HGW の steady-state-only 状態から自力では復旧できない。
  NTT NGN 上の本番 routerd は DHCPv6 を能動制御する必要がある。
- **assert**: routerd の controller は必要に応じて Solicit / Request / Renew / Rebind / Release / Confirm /
  Information-Request を発行する。`IPv6PrefixDelegation` リソースの `objects.status._variables` には次を保持する。
    - `lease.serverID`
    - `lease.prefix`
    - `lease.iaid`
    - `lease.t1`、`lease.t2`
    - `lease.pltime`、`lease.vltime`
    - `lease.sourceMAC`
    - `lease.sourceLL`
    - `lease.lastReplyAt`
    - `lease.reconfigureKey`
    - `wanObserved.*`（直近 RA のフィールド、Server-ID 導出スナップショット）
- **assert**: Renew は D 節の仮説に従い、multicast、新規 xid、非ゼロ T1/T2、`reconfigure-accept` option ありで送出する。
- **assert**: Server-ID は既定で WAN インターフェイス上の直近 RA から、modified-EUI-64 逆変換で MAC を復元し、
  `0003 0001` を前置して導出する。リソース仕様で override できる。
- **assert**: `routerd dhcp6 renew` および `routerd dhcp6 release` の CLI
  サブコマンドは診断・復旧用ツールであり、運用上の lease refresh ツール
  ではない。HGW は自然な T1 境界で送られる Renew のみを受理し、T1 前に
  発火された ad-hoc な active Renew は silent drop される（D 節参照）。
  steady-state の lease 更新は OS の DHCPv6 client（`dhcp6c`）が自身の
  T1 タイマーで担う。routerd は Reply を記録し lease 状態を可視化するが、
  HGW が受理する以上の頻度に refresh を前倒ししようとはしない。

### H.1 T1 サイクルと grace window

- **measure**: この HGW の Reply 値は `T1=7200`（2 時間）、`T2=12600`
  （3.5 時間）、`pltime=vltime=14400`（4 時間）。
- これらの値の下では、T1 境界での Renew 成功は Reply 受信の瞬間から
  全タイマーを reset する。次の Renew opportunity はそこからさらに 2 時間後
  （元割当て基準で T0+4h、最新 Reply 基準で T0'+2h）。
- **assert**: Renew opportunity は厳密に 2 時間ごと 1 回であり、routerd
  はその間隔を正当に前倒しできない。自然な T1 境界より前に発火された
  `dhcp6 renew` は HGW に silent drop される。
- **measure**: HGW が T1 境界の Renew を最初に無視した瞬間から、`vltime`
  が切れるまで operator には約 2 時間の grace がある（T1 で Renew → silent
  drop → T2=3.5h で Rebind 試行 → T0+4h で vltime expire）。この 2 時間の
  grace window が、routerd の検出・operator 通知のサイズを規定する時間予算
  である。
- **observe**: この 2 時間の grace window が、Renew 1 回の取りこぼしが
  まだ復旧可能である理由でもある。vltime 切れの前のいずれかの時点で HGW
  を再起動すれば maintenance path が回復し、次の T1 境界の Renew は成功する。

## I. 運用ランブック（要約）

routerd VM が lease を失い、HGW は数時間以上稼働している場合:

1. WAN インターフェイスで RA が見えていることを確認する
   （`tcpdump -i <wan> 'icmp6 and ip6[40] == 134'`）。
2. HGW MAC が想定どおりか確認する
   （`ip -6 neigh show fe80::1eb1:7fff:fe73:76d8 dev <wan>` — LL は実観測値で置き換える）。
3. 期待される `Server Identifier` を計算する: `0003 0001` + HGW MAC。
4. bootstrap-via-Request か HGW 再起動かを選ぶ。
   - bootstrap-via-Request が routerd の既定動作。
   - HGW 側に状態破壊が見えるなら、より安全な fallback として HGW 再起動。
5. HGW に古い binding が残っていれば、それぞれに multicast `Release` を送る。
   IAID ベースで dedup されると思わないこと。
6. Reply 後、steady state で T1 の Renew が成功するかを検証する。失敗したら
   バイト列を取得して D 節の仮説と突き合わせる。

## J. 調査の経緯: 落とし穴、行き詰まり、RFC との乖離

ここでは「結果」だけでは伝わらない、誤解・行き止まり・RFC 8415 を厳格に読んだ
ときの解釈と HGW の実挙動の乖離を残す。同じ NTT フレッツ環境で後続の人が同じ
思考の罠にハマらないことが目的である。各小節は次の 3 層を明示的に分けて書く:
(a) 当時我々がそう思っていたこと、(b) RFC 8415 の文言、(c) HGW の実挙動。
各小節の最後に短い *教訓* を 1 行で添える。

### J.1 「Solicit canonical path」のトンネルビジョン

- **当時の認識**: RFC 8415 §18.2.1 の「fresh client は Solicit から始める」
  という canonical path に思考が固定された。prior lease を持たない client は
  他の入口がなく、HGW を反応させるには Solicit の形を当てるしかない、と
  思い込んだ。
- **試したこと（全敗）**: content axis のみで 34 種類の Solicit variant を
  試した。DUID-LL / DUID-LLT / DUID-EN、IAID（0、1、MAC-tail、IX 由来）、
  hop limit（1、8、64、128、255）、flow label（0、ランダム、最大）、
  Vendor-Class option 16（NEC / Buffalo / Yamaha enterprise number）、
  User-Class option 15、空きスロットを狙った IA Prefix hint、
  RS→RA→Solicit の順序実験、MAC OUI sweep。すべて HGW から silent drop
  された。隣の IX2215 は同じ瞬間も既存 lease を更新できていた（A 節）。
- **盲点**: *message-type 軸* を動かす発想がなかった。RFC §18.2.4
  INIT-REBOOT は「prior valid lease state を持つ client のための path」と
  記述されており、これを字面通り「prior lease がない我々には使えない」
  と読み、Advertise を経由しない Request 直接送出は試行対象から外れていた。
- **RFC 8415 の文言**: §18.2.4 では INIT-REBOOT の Request は prior の
  `Server Identifier` と prior の `IA_PD` 内容を載せる。サーバは binding が
  まだ存在するか検証し、応答する（あるいは `NoBinding` で reject する）
  ことを期待する。
- **HGW の実挙動**: PR-400NE は *prior valid lease を持たない* client が
  送ってきた Request でも、(i) Server Identifier が HGW の `DUID-LL` と
  一致し、(ii) 任意の prefix の IA_PD claim を持っていれば受理する。
  HGW は claim が過去の binding に対応するかを strict には検証せず、空き
  slot を割当てて Reply に載せて返す。これが B 節を成立させている lenient
  な逸脱である。
- **教訓**: サーバが silent drop してきたら、content 軸を brute-force する
  前に *message-type 軸* を全 exhaust すること。サーバの state machine は
  遷移ごとに別ロジックの関門を持つことが多く、いま跳ね返されている関門は
  送っているメッセージが開けようとしている関門ではないかもしれない。

### J.2 unicast vs multicast Renew の混同

- **当時の認識**: WIDE `dhcp6c` の Renew が HGW に通らなかったとき、最初は
  「HGW は multicast Renew を一切受け付けない」と結論した。これが誤りで、
  1 セッション分の調査時間を消費した。
- **観測**: `dhcp6c` はサーバ link-local をキャッシュしていると、まず HGW の
  グローバルアドレス宛に unicast で Renew を出す。HGW は約 4ms 以内に
  `status-code 5 (UseMulticast)` の Reply を `xid` を echo して返す。
  `dhcp6c` は *同じ `xid` のまま* multicast `ff02::1:2` に再送するが、HGW は
  応答しない。並行して動いていた IX2215 は最初から fresh xid で multicast
  Renew を出しており、同じ時間帯に 11/11 で成功していた。
- **RFC 8415 の文言**: §18.2.10.1 は `UseMulticast` 受信後、後続のメッセージは
  multicast で送るべきと規定する。同じ xid を transport を変えて再利用する
  ことを RFC は明示的に禁止していない（gray area）。
- **HGW の実挙動**: PR-400NE は xid 単位で transport をまたいで dedup して
  おり、reply 済 transaction と同じ xid の multicast 再送を duplicate として
  silent drop する。state machine 的には妥当だが、RFC が文字通り要求して
  いる挙動ではない。
- **誤読の中身**: client 側の 2 通の出力を「Renew の試行」とまとめて見て
  いたために、wire 上の xid を分離して追えていなかった。capture を xid で
  分けた瞬間、unicast→multicast の二度撃ちであることが見えた。
- **教訓**: client 側 trace と server 側 trace は分けて取り、時間順ではなく
  `xid` を主キーにして読む。「retransmit」という語は、まさに DHCPv6 実装が
  自分で作りがちなバグの種類を覆い隠してしまう圧縮表現である。

### J.3 「T1/T2 を待つ」アンチパターン

これは事後的に整理した教訓ではなく、セッション中に user から直接指摘された。

- **当時の戦略**: binding 取得後の Renew は T1=7200s の自然タイマーまで
  待って観測した。Renew が失敗したら再取得し、また T1 まで待って次の
  Renew を観測した。能動制御の手段が無いまま、自然サイクルを何周も消費した。
- **user の指摘（要約）**: 「PD 取得後の Renew も T1/T2 になるまで放置 →
  更新できずに失敗、を何度も繰り返している。Solicit も Renew も任意の
  タイミングで自分で発火できるまで方法を確立しろ」（user 発言の趣旨）。
- **RFC 8415 の文言**: §18.2.6 は Renew を「T1 経過後」に送ると規定する。
  これは client timer の規定であって、operator が能動的に Renew を発火する
  ことを禁止する文言ではない。debug 文脈では当然 T1 を待たずに送ってよい。
- **本来やるべきだったこと**: scapy + raw socket で必要な message type を
  すべて即時に発火できる手段を最初に作り、OS client の自然サイクルは
  「実運用挙動の検証」に温存する。
- **教訓**: protocol debug ではシステム上で最も遅い natural timer に反復
  速度を支配させない。能動制御を先に組み、passive サイクルは後で確認する。

### J.4 「fresh client は Solicit を使わなければならない」という RFC strict reading

J.1 の概念的裏面を、RFC 角度から書く。

- **strict RFC reading**: 8415 §18.2.1 は prior state を持たない client の
  四段（Solicit→Advertise→Request）を記述する。§18.2.4 INIT-REBOOT は
  prior の有効な `Server Identifier` を持っている client が、既に持って
  いる binding を確認・更新するときの path として記述される。
- **routerd lab VM の状況**: on-disk に prior lease state が無く、HGW は
  我々が試したすべての Solicit 形を silent drop した（A 節）。
- **最終的にやったこと**: RA 観測（C 節）から導出した Server Identifier と、
  任意の /60 を載せた IA_PD claim を持つ Request を scapy で合成して送った。
  HGW は空き slot を割当てて返した。
- **これが RFC 逸脱と言える理由**: RFC 観点では、claimed binding を実際には
  持っていない client から来た INIT-REBOOT Request には `NoBinding` ステータスを
  返すのが筋。HGW はそれを *新規取得要求* として扱い、15 slot 表から空きを
  払い出している。これが HGW の Solicit 関門が閉じている時間帯でも
  bootstrap-via-Request（B 節）を成立させている逸脱である。
- **routerd にとっての意味**: H 節の active controller は*意図的に* この
  lenient な逸脱に乗る。長時間稼働中の HGW から再起動なしで新規 /60 を
  取得できる唯一の手段だから。`docs/design-notes.md` Section 5.2 が
  hybrid recovery path として明文化しているのはこの理由である。
  これはバグ利用ではなく設計である。
- **教訓**: RFC の path 名を「state X の client が送るもの」として記憶しない。
  「この message type をこの内容で送ると、サーバが何を返すか」で記憶し、
  RFC の strict client-state precondition は *RFC の期待値* と理解し、必ずしも
  *実装の関門* と一致するとは限らないと心得る。

### J.5 RFC 準拠と HGW 実挙動の対比

| 動作 | RFC 8415 の規定 | HGW (PR-400NE) の実挙動 | routerd の対応 |
| --- | --- | --- | --- |
| 通常稼働中の新規 Solicit | サーバは応答すべき（Advertise/Reply） | 長時間稼働 HGW では多くが silent drop | 先に Solicit、N 回失敗で Request にフォールバック |
| INIT-REBOOT Request（prior lease あり） | サーバは binding を確認/再発行（§18.2.4） | 即応答（Reply） | 通常経路 |
| INIT-REBOOT 風 Request（prior lease なし） | サーバは `NoBinding` を返すべき（§18.2.4） | **空き slot を新規割当して Reply** | 意図的に利用（recovery path） |
| unicast Renew | サーバが unicast 許可した場合のみ可（§18.2.10） | 常に応答（`UseMulticast` か Reply） | 使わない（multicast 直送） |
| multicast Renew（T1/T2=0、`reconfigure-accept` 無し） | サーバは応答すべき（§18.2.6） | silent drop（観測） | 出さない |
| multicast Renew（非ゼロ T1/T2 + `reconfigure-accept`） | サーバは応答すべき | 応答 | 標準の送出形 |
| Reply の送信元 UDP ポート | 547（RFC 8415 §7） | 49153（ephemeral、観測） | client 側はソースポートを問わず受理 |
| transport 切替時の同 xid 再送（unicast→multicast） | 明示的禁止なし | duplicate として drop | multicast 再送は必ず新規 xid |

### J.6 公開資料に書かれていない注意点

NTT の公開資料にも RFC にも書かれていない、見落としがちな点をまとめる。

- **HGW の acquisition window はバースト的**: Solicit を受理する窓は HGW
  再起動直後の数分間に集中して開いているように見える。長時間稼働 HGW は
  fresh Solicit を何時間も silent drop し続ける。公開仕様には書かれていない。
  本セッションからの推定。
- **Reply の送信元 UDP ポートは 547 ではなく ephemeral**: ラボ観測値 49153。
  `udp port 547` だけの pcap フィルタは Reply の片道を取りこぼす（F 節）。
- **WIDE `dhcp6c` の unicast→同 xid multicast ループ**: OSS 実装の挙動で
  あって RFC が要求するものではない。`dhcp6c` で本 HGW の Renew を
  デバッグする人は、両 transport の capture を取らない限り「Renew が失敗
  している」としか見えない（J.2）。
- **`reconfigure-accept` 無し かつ T1/T2=0 の Renew は multicast でも
  silent drop される** — D.4 の仮説。まだ要素ごとの ablation はしていない。
  再現する人は 3 つの前提条件を 1 つずつ落として検証し、結果を D 節に追記
  すること。
- **Server-ID は RA 1 通から計算可能**: RA 送信元 link-local に modified-EUI-64
  逆変換をかけて MAC を復元し、`0003 0001` を前置する。これにより過去の
  DHCPv6 履歴ゼロでも Request bootstrap がコールドスタートから可能になる
  （C 節）。

## K. HGW DHCPv6 サーバの hung-up 状態

ここに記録するのは、A 節「長時間稼働 HGW が新規 Solicit を silent drop する」
状態とは別の故障モードである。2026-04-30 のラボセッションで観測された。
このモードでは HGW DHCPv6 サーバが Solicit だけでなく *すべての* DHCPv6
client message に応答しなくなる。同セグメントの IX2215 も同時間帯に Renew
失敗していたことが、A 節と K 節の判別点になる。

### K.1 症状

- **observe**: HGW は受信したすべての DHCPv6 client message を silent drop
  する。Solicit / Request / Renew / Rebind / Release / Confirm /
  Information-Request すべて。新規取得しようとしている client も、有効な
  既存 binding を持つ client（IX2215、routerd lab VM）も、同様に Reply を
  引き出せない。
- **observe**: L2 / NDP 平面は健全。HGW link-local との NS/NA 交換は通り、
  HGW は同じ flag・同じ prefix の RA を LAN セグメントに出し続ける。
- **observe**: HGW LAN PD 払い出し表に既にある binding はテーブルに残る。
  ステータス画面では同じ `N/15` slot が見え続けるが、各 slot の lease
  カウンタは単調減少のみ。Reply が HGW から出ないため Renew で更新されない。
- **measure**: 当該時間帯の IX2215 は、A 節の measurement にあった
  「T1 ごとの Renew 11/11 成功」状態から、「T1 ごとの Renew で Reply 無し」
  状態に切り替わっていた。これにより、これは routerd 側のパケット形の問題
  ではなく HGW 側のサーバ停止であることが確定した。

### K.2 routerd による検出

- **assert**: active controller（H 節）は、Renew または Rebind を wire 上に
  出した時刻と、対応する Reply 受信時刻の wall-clock 差を測る。D 節で
  記録した形の Renew を出してから N 秒（既定値 `30 s`）以上 Reply が
  観測できなければ、controller は当該 binding を `HGW DHCPv6 hung
  suspected` 状態に flag する。
- **assert**: routerd は当該 `IPv6PrefixDelegation/<name>` の
  `objects.status._variables.hgwHungSuspectedAt` に suspicion 時刻を
  記録する。`routerctl describe ipv6pd/<name>` は
  `WARNING: HGW DHCPv6 hung suspected since <timestamp>` を表示し、
  残 `lease.vltime` も併記して operator が緊急度を判断できるようにする。
- **assert**: Renew Reply（または cached lease を更新する任意の Reply）が
  入った時点で suspicion flag を解除する。回復は同じ `routerctl describe`
  画面から見える。

### K.3 公開報告

同形の症状は複数の上流機器ファミリで公開ラボノートに報告されている。
routerd はこれらを「外部要因の既知故障モード」として、本ラボ単独事象では
ないことの裏付けとして扱う。

- [rabbit51 blog (2022-03-05, PR-600MI)](https://blog.goo.ne.jp/rabbit5151/e/eb1a5be280f92d550c343e17e893007f)
  — cite: 「DHCPv6 が hung up して委譲済みの prefix の renew に応答しなくなり、
  prefix の valid lifetime (=14400) が切れて IPv6 通信が出来なくなる」。
  当該事例では HGW が自動再起動して回復した報告。
- [HackMD on Yamaha RTX1200/1210 with AsahiNet (2021)](https://hackmd.io/@jkofK/BJJHe7wmu)
  — cite: DHCPv6 リースが定期的に途絶え、`status rebind` 状態になる挙動。
  HGW DHCPv6 サーバが Renew/Rebind に応答しなくなる症状と整合する。
- [azutake blog (2023, Cisco IOS-XE on FLET'S Cross)](https://azutake.hatenablog.jp/entry/2023/12/07/113048)
  — cite: 上流側で Solicit が無視される事象。client 側ではなく HGW
  ファームウェア経路に原因を見ている。

### K.4 回復

- **measure**: 2026-04-30 のラボでは、HGW を手動で再起動すると DHCPv6
  サーバが直ちに回復した。再起動後は acquisition path（A 節の「再起動直後
  数分」窓）と maintenance path（D 節の Renew 受理）の両方が通常動作した。
- **measure (2026-04-30 11:16:50 UTC)**: 再起動後、`router03` から routerd
  active Request を送出すると、回復した HGW で受理され、既存 `1240::/60`
  binding を満期近い lifetime に refresh できた。
- **measure (2026-04-30 11:20:44 UTC)**: 11:16:50 の Request 成功からわずか
  約 4 分後、同じ `router03` から同じ HGW に対して送出した active CLI の
  Renew、Release、続けての Request はいずれも DHCPv6 Reply を受け取れな
  かった。HGW のテーブルは更新されず、binding も解放されなかった。これは
  HGW がそれ以外は正常（NDP・RA・既存 binding の lease カウンタは影響を
  受けない）であっても、再起動直後の acceptance window が数分のうちに閉じ
  得ることを示す。K.5 の運用ランブックは、最初の成功交換の後も window が
  開き続けていると仮定してはならない。
- **observe**: HGW が operator 介入なしに自力回復するかは未確認。rabbit51
  報告は auto-reboot 回復を述べているが、本ラボでは auto-recovery 経路は
  未検証。確実に動いた回復手段は手動再起動のみ。
- **observe**: HGW 再起動は宅内ネットワークを 1〜2 分停止させる。WAN
  再接続、RA 再送、DHCPv6 サーバ準備の順序は通常の起動シーケンスに従う。

### K.5 運用ランブック

`routerctl describe ipv6pd/<name>` に K.2 の警告が出たとき:

1. 同セグメントの IX2215（または他の Renew のみ動作する client）で確認し、
   suspicion が実体を伴うかを判定する。IX2215 も次の T1 で Renew 失敗
   するなら HGW は K 節状態であり、A 節状態ではない。
2. `routerctl describe` で残 `lease.vltime` を読む。
3. `vltime` が短い（ラボ閾値: 1 時間未満）ときは即座に HGW を再起動する。
   lease が切れると routerd が提供している全 LAN で IPv6 が止まる。
4. `vltime` が十分（数時間以上）あるときは、影響の少ない時間帯に HGW
   再起動を計画する。`routerctl describe` を継続監視し、自然回復で
   suspicion が解けるかも観察する（auto-recovery は保証されないが報告は
   ある）。
5. 再起動後、routerd の次の Renew で Reply を受け取り、
   `hgwHungSuspectedAt` が解除されることを確認する。
6. **注意**: HGW を再起動した直後で post-reboot acceptance window 中で
   あっても、その window は数分以内に閉じ得る（K.4 の 11:20 UTC measure）。
   最初の成功交換の後の active CLI 利用は best-effort と扱い、正規の運用
   経路は OS DHCPv6 client が自然な T1 境界で行う refresh に委ねる。

### K.6 routerd が自動修復できない理由

- **assert**: routerd は DHCPv6 client であり LAN 側のサービスである。HGW
  への制御 plane を持たない。HGW にリセットを送ったり、HGW の DHCPv6
  デーモンを再起動したり、内部復旧ルーチンを発火させたりはできない。
- **observe**: K 節状態では HGW は routerd の active Request、Renew、
  Rebind、Release、Confirm、Information-Request を等しく無視する。HGW を
  動かせる DHCPv6 層メッセージは存在しない。
- **assert**: 復旧には人間の操作が必要 — HGW web UI の再起動ボタン、
  または物理的な電源 OFF/ON。この故障モードでの routerd の責務は、検出、
  残 lease の正確な可視化、operator（および設定があれば家族など宅内の
  関係者）への通知に限られる。

## L. FreeBSD dhcpcd 試験記録（2026-04-30）

この節は、FreeBSD の `dhcpcd` を NTT プロファイルの既定値にせず、
明示的なラボ経路として残す理由を記録する。

- **measure**: FreeBSD ラボ VM で既存の `dhcpcd` DUID ファイルを削除すると、
  `dhcpcd 10.3.1` は NTT NGN プロファイルが必要とする DUID-LL ではなく
  DUID-LLT を生成した。
- **measure**: DUID ファイルを手動で DUID-LL（`0003 0001` と WAN MAC）に
  強制しても、同じ試験窓では PR-400NE から Advertise/Reply は返らなかった。
- **measure**: WAN 側仮想 NIC の MAC だけを変更して同じ試験を繰り返しても、
  結果は変わらなかった。少なくともこの観測では「特定 MAC が嫌われている」
  という説明は弱い。
- **assert**: FreeBSD の NTT プロファイル既定値は、現時点では KAME/WIDE
  `dhcp6c` のままとする。FreeBSD `dhcpcd` はこのプロファイルでは既知の
  問題がある組み合わせとして扱い、routerd は拒否ではなく警告を出す。これにより、
  将来の再試験は検証規則を変えずに実施できる。

## 参考リンク

- RFC 8415 — Dynamic Host Configuration Protocol for IPv6 (DHCPv6)
    - §7 Server / Relay / Client message format
    - §15 Reliability of Client-Initiated Message Exchanges (retransmission)
    - §18.2.1 Solicit
    - §18.2.4 Request
    - §18.2.5 Confirm
    - §18.2.6 Renew
    - §18.2.10 Reconfigure
    - §18.2.10.1 INIT-REBOOT considerations
- systemd issue #16356 — DHCPv6 Renew が IA Prefix lifetime をリセットしない問題
- OpenWrt issue #13454 — NTT フレッツ クロス配下で odhcp6c が 8 時間ごとに切断する症例
- rixwwd 公開ノート — PR-400NE の Reply 送信元 UDP ポートが ephemeral
- sorah 公開ノート — NTT HGW DHCPv6 サーバの DUID-LL 構造
- routerd `docs/design-notes.md` Section 5.2 — active DHCPv6 controller 経路
- routerd `docs/knowledge-base/dhcpv6-pd-clients.md` — クライアント側選択
