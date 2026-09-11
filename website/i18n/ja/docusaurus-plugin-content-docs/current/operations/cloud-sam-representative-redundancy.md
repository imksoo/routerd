---
title: Cloud SAM 代表冗長化検証
---

# Cloud SAM 代表冗長化検証

`representative-redundancy` は、費用枠を定めた全トポロジーの Cloud SAM
切り替え検証プロファイルです。baseline のみを検証する
[`full-topology-minimal`](cloud-sam-full-topology-minimal.md) と、網羅的な開発用の
[`sam-full-validation.sh`](https://github.com/imksoo/routerd/blob/main/tests/e2e/cloudedge/scripts/sam-full-validation.sh)
とは別の契約です。

代表となる RR-A の切り替えに加え、AWS・Azure・OCI・PVE それぞれの edge-A を
1 台ずつ停止・復帰させます。従来の「RR のみ」という範囲ではありません。
B 側を停止・復帰させる試験は、選択された範囲に含みません。

監査対象のソース契約はこのプロファイルを選択しますが、現在のツリーに対する
実環境検証は未実施です。offline test の実行は、実行ホスト、PVE/cloud 操作、
`routerd`、DHCP、IPv6 RA、DHCPv6、BGP、SSH 通信の許可にはなりません。

実行役はリモート（既定の `execution.hostPolicy: approved-remote`）に加え、
明示的に承認したローカルマシンにも置けます。ローカル実行には
`execution.hostPolicy: local-supervised`、`execution.requireRemote: false`、
そのマシンの `socket.getfqdn()` の結果と完全一致する `execution.host` が必要です。
同じ tracked systemd supervisor、staging 証明、ソース・artifact 検査、予算、
cleanup を使い、対話シェルを lifecycle の所有者にはしません。変わるのは実行役の
場所だけで、AWS・Azure・OCI・PVE のトポロジーと E2E 検証範囲は変わりません。
実行役自身では、設定検証の sandbox を含めて `routerd` を起動しません。
必要な実行準備は [release-QA runbook](https://github.com/imksoo/routerd/blob/main/tools/release-qa-labs/README.md)
を参照してください。

baseline inventory 前に、選択した Azure account に必要な認証情報だけを run 専用の
0700/0600 secret store と、初回の書込み可能な `runtime/provider-state/azure`
へ準備します。service principal には、対象の `service_principal_entries.json`
entry が必要です。Azure profile や MSAL cache だけでは足りません。
cached `az account show` ではなく、service user として認証付き read-only API が
成功することを確認します。OCI CLI も operator home の権限を広げずに同じ user
から実行できる必要があります。provider ZIP の hash を RC の lock file と照合し、
展開済みの `linux_amd64` mirror と、監査した RC に一致する unit を配置します。
launcher は後で sealed snapshot から Azure の作業領域を再構築します。

PVE token 作成結果の `info.privsep` は `0` または `"0"` を意味することを確認してから、
保護した token input へ保存します。parse 失敗を理由に作成を繰り返してはいけません。
既存の広範な `PVEAdmin` ACL は run-ID 限定でも least-privilege PASS でもありません。
run のために ACL を広げず、限定した lab 操作の承認を本番環境の変更許可と混同しません。

資源 inventory の全 7 scope zero は、`MUTATING` 前と cleanup 後の境界で確認します。
PVE から cloud certification への移行時は、認証済みの PVE guest・template・
capture bridge・state を保持します。これらは後続の cloud/qualification に必要です。
cloud の saved plan には managed create と宣言済み data read の閉じた guard を適用し、
phase 間の全 scope zero や新しい cloud-only inventory gate は要求しません。
この資源 inventory の baseline は、後述する通信の baseline とは別です。

cloud plan では managed resource 43 件に加え、OCI 4 ノードに対応する
宣言済み `oci_core_vnic_attachments` の data read が正確に 4 件必要です。
guard は planned resources と changes の両方で正確な address・件数・`mode: data` を
照合し、change action は正確に `["read"]` を要求します。欠落・重複・余分な entry や
不正な mode/action は拒否し、4 件すべて欠落した plan も通しません。
これらの読取は課金 VM や予算枠を追加しません。PVE plan に data read は認めず、
PVE の `prior_state` 保持を理由とする plan guard の緩和も不要です。

## 検証する内容

トポロジーはルーター 10 台、クライアント 8 台に固定します。

- `pve-rr-a` と `pve-rr-b` は PVE 上の route reflector であり、AWS の VM ではありません。
- AWS・Azure・OCI・PVE の各環境に leaf 2 台、クライアント 2 台を配置します。
- RR の VM は別々の PVE ホストに配置し、PVE ホストの SSH FQDN も分離します。
  QGA で発見したゲスト管理アドレスは、PVE 内の WireGuard bootstrap のみに使います。
- PVE leaf/RR の管理・underlay bridge は、leaf 専用の capture bridge と分離します。
  RR の VM に capture NIC はありません。

開始前の PVE certification audit は、各 RR の宣言済み PVE ホストから
`qm config` を読み、NIC が 1 個だけであること、固定された underlay bridge に
接続していること、leaf capture bridge に接続していないことを確認します。
Terraform output だけを分離の証拠にはしません。

署名された release contract では、`safety.pveManagementControlPlane: none`、
`safety.pveTLS: pinned-ca`、`pve.managementAddressSource: qga-dhcp` も必須です。
PVE API の信頼には実行ごとに固定した cluster CA だけを使い、TLS 検証の省略や
実行ホストの trust store 変更は行いません。ゲスト管理アドレスは既存の PVE
underlay DHCP から受け取り、PVE apply 後、設定生成前に全ゲストを QGA で発見します。
generator は管理 DHCP リソースを出力せず、harness は DHCPv4/DHCPv6、
IPv6RAAddress、IPv6 router-advertisement を含む PVE router 設定を、
`routerd` の配備前に拒否します。共有の管理 L2 は DHCP/DHCPv6/RA 試験網にしません。

`topology_scale != full`、PVE RR の fault domain が `host-redundant` でない場合、
同一ホスト上の RR ペアは拒否します。同一ホストの cost smoke は、ホスト分離を
伴う検証とは別物です。共有 `SAMNodeSet` は PVE WireGuard endpoint を公開しません。
PVE 設定だけが QGA 由来のローカル bootstrap peer を持ち、cloud 向け通信は
PVE から開始します。cloud peer は handshake から endpoint を学習します。

### PVE ゲストの NIC とイメージの前提

この PVE template プロファイルでは、6 ゲスト共通の管理 NIC は `eth0`、
leaf の capture は `ens19`、client の capture は `eth1` です。
PVE/cloud-init は `ipconfigN` がある NIC を MAC に対応付けて `ethN` へ変更します。
管理 NIC には DHCP 設定があり、client の capture には IP 初期化設定があります。
leaf の capture には MobilityPool がアドレスを所有するため初期 IP を設定しません。
certification driver は管理・leaf capture の名前を QGA へ明示し、qualification driver は
`PVE_MANAGEMENT_INTERFACE`、`PVE_CAPTURE_INTERFACE`、`PVE_CLIENT_CAPTURE_INTERFACE`
で同じ対応を設定生成・client 設定へ渡します。汎用スクリプト単独実行の既定値は変えません。

QGA の厳密な IP・capture MAC 検証は維持します。アドレス発見が失敗したら、
DHCP 不調と判断する前に保存済み QGA の NIC 一覧を確認してください。
元イメージにはゲスト用 `qemu-guest-agent` の導入も必要です。
PVE 側でデバイスを有効にしても、ゲストのパッケージ導入・daemon 起動は行われません。
派生イメージの準備と起動情報の clean は環境認証段階で行い、新規 clone で再認証します。
PVE が渡すゲスト別 IP 設定を無視したり、qualification 中に NIC 名を変更したりして、
古い前提に合わせる運用はしません。

順序は次の一方向です。

1. 全 leaf router を配備します。
2. `pve-rr-a` を配備し、service/status socket と実際の BGP membership を待ちます。
   B の配備前に A が参加した証拠を残します。
3. `pve-rr-b` を配備し、同じ membership 確認後、ペアの準備完了を記録します。
4. control/dataplane と provider の gate、全 56 directed client hostname flow、
   全 42 cloud-origin ingress flow による完全な baseline を実行します。
5. `pve-rr-a` を停止します。B の BGP membership、稼働中の全 leaf の
   control/ownership・provider gate を確認し、AWS → Azure → OCI → PVE → AWS の
   hostname canary 4 経路を実行します。
6. `pve-rr-a` を復帰させ、同じ gate と canary を再確認します。
7. `aws-leaf-a`、`azure-leaf-a`、`oci-leaf-a`、`pve-leaf-a` の順に、
   独立した harness 呼び出しを 1 回ずつ行います。各回は対象の A だけを停止し、
   同じ環境の B を含む残存ルーターを確認して A を復帰させます。
   その復帰確認を終えるまで、次の環境には進みません。停止後・復帰後の両方で、
   稼働中の全 leaf の control/dataplane・provider/ownership、RR membership の
   gate と、全 56 client flow・全 42 cloud-origin ingress flow を必須にします。

最初の RR 呼び出しは、完全な baseline 1 回と、各 RR 切り替え後の canary 4 経路を
維持します。後続の edge 呼び出しは配備と初回 baseline を繰り返しませんが、
edge の停止後・復帰後には両方の完全な traffic matrix を繰り返します。
RR 用 canary で edge E2E の証拠を代用しません。必須証拠の欠落・重複・失敗・
skip は PASS ではありません。

ここでの「停止」は、ゲストの `routerd.service` と、別 service がある場合の
`routerd-bgp.service` の停止です。復帰ではそれらを開始します。
VM の電源断、物理ホスト障害、転送中のアプリケーション接続が無停止で維持される
ことの証明ではありません。
RR・edge の両方で、traffic/membership の確認に加え、停止成功と inactive、
開始成功と active を明示的に確認した証拠が必要です。matrix の成功だけでは、
要求した service 切り替えが実際に発生した証明にはなりません。

### A/B の検証範囲と限界 {#ab-coverage-boundary}

このプロファイルでは、既定の PVE `single-router` ownership gate だけを許可します。
CARP は対象外です。generator の任意 CARP モードは primary/secondary に異なる
優先度を設定するため、対称とは扱えません。

既定トポロジーの静的ソースレビューでは、同一役割 A/B の OS/image 選択、
capture/provider mode、timer は共通の生成経路でした。ただし、これは
テンプレートの比較であり、配備済み状態の一致を証明するものではありません。

- RR は別ホストに配置され、underlay bridge/VLAN も個別に指定できます。
  bootstrap では A が B より先に参加します。
- PVE leaf A/B は同じホストと capture bridge を共有します。
  service 単位の停止試験からホスト障害への冗長性は主張できません。
- identity、アドレス、鍵、provider resource identity、観測された ownership、
  bootstrap 結果は各ノード固有です。共通でも未固定の image/CLI 選択から、
  インストール済みバージョンの一致は保証できません。
- PVE の run1 verification/control annotation は既定で異なりますが、
  現在の generator はその値で `gratuitousARPOnSeize` を設定していません。
- 既存の generator fixture は cloud 全 A/B ペアと任意 CARP モードを網羅しません。

A 側の PASS は、観測した方向の停止・復帰と、その際の B の参加を示すだけです。
B 自身の停止・復帰と逆方向の切り替えは未検証です。正規化した設定が似ている
ことを理由に PASS 扱いにはしません。priority、OS、policy、provider/NIC 動作、
timer、bootstrap、fault domain が変わる場合は、改めて範囲をレビューします。

このプロファイルはリソースを作成・破棄しません。作成、後始末、網羅的な
zero inventory の所有者は、永続的な release-QA supervisor だけです。

## コマンドの形

ソース監査と、承認済みの新規 release-QA contract を前提とします。
対話シェルではなく、supervisor から次の形で呼び出します。

```sh
PVE_MANAGEMENT_INTERFACE=eth0 \
PVE_CAPTURE_INTERFACE=ens19 \
PVE_CLIENT_CAPTURE_INTERFACE=eth1 \
tests/e2e/cloudedge/scripts/sam-representative-redundancy.sh \
  --tofu-output /var/lib/routerd-release-qa/<run-id>/runtime/tofu-output-full.json \
  --artifact /var/lib/routerd-release-qa/<run-id>/runtime/routerd-<version>-linux-amd64.tar.gz \
  --tfvars /var/lib/routerd-release-qa/<run-id>/runtime/terraform.tfvars \
  --ssh-key /var/lib/routerd-release-qa/<run-id>/runtime/secrets/guest_ssh \
  --pve-ssh-key /var/lib/routerd-release-qa/<run-id>/runtime/secrets/pve_ssh \
  --pve-known-hosts /var/lib/routerd-release-qa/<run-id>/runtime/pinned/pve-known_hosts \
  --evidence-root /var/lib/routerd-release-qa/<run-id>/runtime/evidence/qualification/representative-redundancy \
  --max-runtime-seconds 5400
```

### 承認済みの予算 {#approved-budget}

承認されたソース上の予算方針は、従来の qualification 32 分・mutation 55 分の枠を
置き換えます。topology、故障 scenario、証拠 gate、cleanup 要件は変更しません。

| 境界 | 承認値 |
| --- | --- |
| Provision/certification | 最大 18 分（1080 秒） |
| Qualification wrapper 全体 | 最大 90 分（5400 秒） |
| Supervisor reserve の最低値 | 5 分以上（300 秒） |
| Mutation TTL | 最大 115 分（6900 秒） |
| Cleanup/inventory の枠 | 10＋5 分を 2 回、変更なし |
| Cleanup を含む有料リソースの計画上の枠 | 最大 145 分（8700 秒） |
| 費用方針の見積／実行許可判定の上限 | US$1.55／US$1.60 |

最小配分は `18 + 90 + 5 = 113` 分で、115 分の TTL 内に 2 分の余裕があります。
cleanup を含む計画上の枠は `115 + 2 × (10 + 5) = 145` 分で、qualification を
延長する時間ではありません。費用の数値は方針上の見積であり、現在の provider 料金の
見積書でも、実際の請求額の上限でもありません。復旧が見積を超えても、正式な
cleanup と zero inventory の確認は必須です。

`5400` 秒は wrapper 全体の上限かつ contract の qualification budget です。
5 回の harness 呼び出しそれぞれに与える枠ではありません。後続の呼び出しには
残り時間だけを渡し、証拠の検証にも同じ予算を使います。拡張した全シーケンスの
実環境での完走時間は引き続き未計測です。予算の拡大や offline PASS は、実環境 PASS
や費用・所要時間の保証ではありません。ソース変更の承認だけでは実環境の実行許可に
ならず、新規の承認済み contract、canonical source の受入条件、必須 precheck が
引き続き必要です。push 済みや実環境検証済みであることも意味しません。
timeout は失敗とし、supervisor の
quiesce、cleanup、網羅的な zero inventory へ移ります。有料枠の延長や
必須 matrix の削減を許可するものではありません。
qualification driver も独立した外側の deadline を適用します。wrapper や出力
pipeline が停止して進まなくなった場合も実行枠を延ばさず失敗とし、supervisor が
mutation process group を quiesce してから cleanup に進みます。

`--tofu-output` は raw OpenTofu output ではなく、QGA で補完した PVE certification
output です。全 PVE router に QGA 由来の `management_ip`、
`pve_management_source: qga-dhcp`、QGA で検証した `ssh_host_keys`、
`ssh_host_key_source: qga` が必要です。certification driver は発見したアドレスと
鍵を mode 0600 の known-hosts artifact に結び付けます。`sam-e2e` もその pin を
使い、共有管理網を host-key scan しません。

最初の RR 呼び出しは次の scenario flag を維持します。

```text
--staged-rr-pair pve-rr-a pve-rr-b
--failover-node pve-rr-a
--rejoin-after-failover
--transition-canary
--skip-legacy-protocols
--skip-load-balance-report
--success-evidence-minimal
```

後続の各 edge 呼び出しでは、対象環境を 1 つ選び、最初に生成した設定を再利用して、
証拠のディレクトリを分けます。

```text
--staged-rr-pair pve-rr-a pve-rr-b
--failover-node <site>-leaf-a
--rejoin-after-failover
--skip-deploy
--reuse-deployed-topology
--skip-initial-validation
--configs-dir <initial-RR-evidence>/config-gen/configs
--full-cloud-ingress
--skip-legacy-protocols
--skip-load-balance-report
--success-evidence-minimal
```

staged-RR flag は edge 試験でも RR membership 確認を維持します。
`--skip-deploy` により RR の再配備はしません。edge には
`--transition-canary` を渡しません。`--skip-initial-validation` は完了済みの
初回 baseline だけを省略し、停止後・復帰後の検証や必須 control/provider gate は
省略しません。`--success-evidence-minimal` が省略するのは成功時の任意診断 snapshot
だけで、gate やどちらの traffic matrix も省略しません。

`--reuse-deployed-topology` には `--skip-deploy` と `--configs-dir` の両方が必須です。
PVE dataplane の設定、guest sandbox での config validation、client の hostname・
SSH-key・route 設定を省略します。read-only preflight と生成設定の safety audit は
必須のままです。後続 scenario の間に準備処理による修復や再設定を挟まず、
配備済みのトポロジーで指定の service 停止・復帰を検証します。

`--destroy-cmd`、performance、legacy-protocol、B 側故障の flag は渡しません。

最初の証拠は `<evidence-root>/representative-redundancy` に保持します。
edge の証拠は同じ階層の `edge-aws-leaf-a`、`edge-azure-leaf-a`、
`edge-oci-leaf-a`、`edge-pve-leaf-a` に分離します。profile result は各 scenario を
個別に記録し、B 側の同等性は未証明のままにします。集約結果で個別の失敗や
欠落を隠してはいけません。

## Offline ソース検証

fake harness は、正確な引数契約、PVE RR の host fault domain と capture bridge の
分離、A/B 順の BGP membership 証拠、最初の完全 baseline と RR canary、
独立した edge-A 4 試験の停止後・復帰後 matrix 契約を確認します。
不完全な証拠、予定外の B 側 scenario、共有予算の枯渇は negative case で拒否します。
実 endpoint や daemon は使わず、実環境の failover 成功を証明するものではありません。

```sh
make cloudedge-representative-redundancy-offline-test
make cloudedge-pve-bridge-audit-offline-test
shellcheck -x tests/e2e/cloudedge/scripts/sam-e2e.sh \
  tests/e2e/cloudedge/scripts/sam-representative-redundancy.sh \
  tests/e2e/cloudedge/scripts/sam-representative-redundancy-offline-test.sh
```
