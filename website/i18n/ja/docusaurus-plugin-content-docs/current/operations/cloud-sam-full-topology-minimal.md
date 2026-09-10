---
title: Cloud SAM 全トポロジー baseline
---

# Cloud SAM 全トポロジー baseline

`full-topology-minimal` は、有料の最終
[`representative-redundancy`](cloud-sam-representative-redundancy.md) プロファイルと
共通の、全トポロジー baseline を 1 回実行するためのプロファイルです。
網羅的な開発・failover 試験である
[`sam-full-validation.sh`](https://github.com/imksoo/routerd/blob/main/tests/e2e/cloudedge/scripts/sam-full-validation.sh)
とは分離します。その開発用 suite を release-QA の mutation window に入れてはいけません。

現在のツリーに対する実環境実行は未実施です。offline wrapper check は移植可能な
ソース検証にすぎず、ホスト検証、provisioning、有料 qualification の許可ではありません。

レビュー済みの全トポロジー、すなわちルーター 10 台（`pve-rr-a`、`pve-rr-b` と
AWS・Azure・OCI・PVE 各 2 台の leaf）、各環境 2 台ずつ計 8 台のクライアントが必要です。
共有 `SAMNodeSet` はどの PVE router にも WireGuard endpoint を公開しません。
PVE router の生成設定には、他の PVE router guest に向けた明示的なローカル
bootstrap peer を含め、QGA で発見したゲスト管理アドレスだけを使います。
これらは PVE ホストや public のアドレスではなく、cloud 設定には入りません。
cloud peer への handshake は PVE 側から開始し、cloud 側が WireGuard handshake
から endpoint を学習します。同じ artifact を全 10 router に配備し、次の
baseline 1 回だけを検証します。

- control-plane/dataplane の readiness gate。
- 全 56 directed client-to-client hostname flow。
- 全 42 cloud-origin directed cloud-ingress hostname flow。
- `MobilityPool` の provider readiness/no-conflict gate。

legacy protocol、performance、load-balance report、transfer probe、failover/rejoin、
provisioning、destruction は行いません。成功を返す前に matrix の期待行数と全結果を
確認します。不完全な matrix は失敗であり、skip ではありません。

## Baseline 専用の予算

単体 wrapper の上限は従来どおり 20 分です。最終 release contract はこれだけを
選択せず、`representative-redundancy` を選択します。代表プロファイルは PVE RR の
`A -> AB -> B-only -> AB` を既存の cross-site canary 4 経路で確認した後、
AWS・Azure・OCI・PVE の各 edge-A を、独立した試験で 1 台ずつ停止・復帰させます。
各 edge 試験は停止後・復帰後の両方で、全 56 client flow と全 42 cloud-ingress flow、
稼働中の全 leaf の control/dataplane・provider/ownership と RR membership の
必須 gate を確認します。
A の復帰を終えてから次の環境に進みます。B 側の停止・復帰は対象外で未検証です。
PVE は既定の `single-router` のみを対象とし、優先度が非対称な CARP は使いません。
これは guest の routerd/BGP service の切り替えであり、VM や物理ホストの電源断
試験ではありません。従来の qualification 32 分枠を置き換える、承認済みのソース上の
予算契約は次のとおりです。

```json
"qualification": {
  "profile": "representative-redundancy",
  "runScope": "full-representative",
  "provisioningBudgetSeconds": 1080,
  "qualificationBudgetSeconds": 5400,
  "minimumSupervisorReserveSeconds": 300
}
```

guard は、別の最終 profile、18 分超の provision/certification budget、90 分超の
代表 qualification budget、5 分未満の supervisor reserve、合計が mutation TTL の
115 分を超える契約を拒否します。artifact contract は代表 wrapper と依存する
`sam-e2e.sh` harness を固定します。

reserve の最低値は 5 分のままで、`18 + 90 + 5 = 113` 分となり、115 分（6900 秒）の
TTL 内に 2 分の余裕があります。cleanup/inventory は従来どおり `10 + 5` 分を 2 回とし、
有料リソースの cleanup を含む計画上の枠は 145 分（8700 秒）です。追加の試験時間
ではありません。

5400 秒は最初の RR 呼び出し、後続 edge 4 回、証拠検証を含む wrapper 全体の枠です。
後続呼び出しには残り時間だけを渡します。承認済みの費用方針の見積は US$1.55、
実行許可判定の上限は US$1.60 です。現在の provider 料金の見積書や、実際の請求額の
上限ではありません。拡張したシーケンスの実環境での完走時間は未計測です。
offline PASS やソース上の予算変更承認は、実環境の実行許可でも、実環境 PASS・
所要時間・実費の保証でもありません。新規の承認済み contract と source/実行の
全 precheck が引き続き必要で、push や実環境検証の完了を意味しません。
PASS を得るために TTL を延長したり、必須 matrix 行を省略したりしないでください。
静的なテンプレート比較の限界は、
[代表プロファイルの A/B 検証範囲](cloud-sam-representative-redundancy.md#ab-coverage-boundary)
を参照してください。

release-QA mutation driver は最初の予算を cloud/PVE provision/certification に使い、
次の予算で profile を呼び出します。境界ごとに `timeout` を使い、時間切れは fail-closed
とします。永続的な supervisor が mutation process group を終了してから cleanup
に移ります。実行範囲に限定した OpenTofu destroy と網羅的な zero inventory は、
profile ではなく supervisor が実施します。cleanup/inventory の各試行には固定上限が
あり、有料 mutation timer が切れたことを理由に復旧を放棄しません。

有料 cloud run の前に、同じ固定 contract で `"runScope": "pve-certification-only"`
を使用できます。これは全 PVE topology の gate 1 回だけです。PVE certificate が成功
したら mutation driver は終了し、cloud provisioning や routerd 製品検証には進みません。
cleanup、全 7 scope の zero inventory、PVE token revocation は supervisor が続行します。
その成功は、新規の `full-representative` run に進むための証拠であり、release PASS
ではありません。

provision/certification、qualification、cleanup は別々の契約です。数時間の故障注入
suite や qualification 延長を、115 分の有料 mutation deadline が許可するものとは
扱いません。

## 実行

[Cloud SAM 再設計の事前監査（英語）](/docs/operations/cloud-sam-rearchitecture-goal) と、
ローカル Cloud SAM checks がすべて成功し、承認済みの新規 release-QA contract が
read-only precheck を通過してから実行します。provision 済みの topology が必要です。
監査がホスト/cloud 検証を禁止している間は実行しないでください。
有料実行の正規入口は永続的な supervisor です。

```sh
tools/release-qa-labs/drivers/start-supervised-release-qa.sh \
  /var/lib/routerd-release-qa/<run-id>/runtime/contract.json
```

provision 済みの全 topology に対する、baseline profile のコマンドは次の形です。

```sh
tests/e2e/cloudedge/scripts/sam-full-topology-minimal.sh \
  --tofu-output /var/lib/routerd-release-qa/<run-id>/runtime/tofu-output-full.json \
  --artifact /var/lib/routerd-release-qa/<run-id>/runtime/routerd-<version>-linux-amd64.tar.gz \
  --tfvars /var/lib/routerd-release-qa/<run-id>/runtime/terraform.tfvars \
  --ssh-key /var/lib/routerd-release-qa/<run-id>/runtime/secrets/guest_ssh \
  --pve-ssh-key /var/lib/routerd-release-qa/<run-id>/runtime/secrets/pve_ssh \
  --pve-known-hosts /var/lib/routerd-release-qa/<run-id>/runtime/pinned/pve-known_hosts \
  --evidence-root /var/lib/routerd-release-qa/<run-id>/runtime/evidence/qualification/full-topology-minimal \
  --max-runtime-seconds 1200
```

`tofu-output-full.json` は raw `tofu output -json` ではなく、PVE certification driver が
QGA で補完した output です。ローカル WireGuard bootstrap peer の生成前に、全 PVE
router で `management_ip`、`pve_management_source: qga-dhcp`、QGA で検証した
`ssh_host_keys`、`ssh_host_key_source: qga` が必要です。同じ QGA step が mode 0600 の
known-hosts artifact を作り、鍵とゲスト管理アドレスを結び付けます。PVE guest SSH は
共有管理網から鍵を学習しません。

コマンド自体は provision/destruction を行いません。teardown 引数を追加しないでください。
qualification が失敗しても有料リソースを残さないよう、supervisor が無条件の cleanup
を所有します。

## Offline ソース検証

次は fake local file で wrapper だけを検証します。instance の provision、`routerd`
の起動、routerd socket の bind、DHCP/RA/network state の変更はしません。

```sh
make cloudedge-full-topology-minimal-offline-test
shellcheck -x tests/e2e/cloudedge/scripts/sam-full-topology-minimal.sh \
  tests/e2e/cloudedge/scripts/sam-full-topology-minimal-offline-test.sh \
  tools/release-qa-labs/drivers/qualification-driver.sh \
  tools/release-qa-labs/drivers/mutation-driver.sh
```

release-QA Python suite を host-safe preflight として実行しないでください。
一部には `sudo`、service manager、socket、namespace の契約を意図的に検証する case が
あり、それらは別途承認された release-QA フェーズだけで実行します。
