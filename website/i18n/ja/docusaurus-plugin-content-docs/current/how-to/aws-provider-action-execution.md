---
title: AWS SAM プロバイダーアクションをゲート付き executor で実行する
---

# AWS SAM プロバイダーアクションをゲート付き executor で実行する

![読み取り専用 preflight、ゲート付きジャーナル承認、executor IAM 識別情報、可逆 AWS 変更を通じた AWS SAM プロバイダーアクション実行の流れ](/img/diagrams/how-to-aws-provider-action-execution.png)

:::warning 実験的機能 — Phase 5.1
これは CloudEdge プロバイダーアクション実行のための**ゲート付きライブ変更**パスです。
**実験的**であり AWS 限定です。
[ADR 0007: Provider Action Execution](../adr/0007-provider-action-execution.md) および
[Selective Address Mobility](../reference/selective-address-mobility)
データプレーンの上に構築されています。本番環境や共有リソースに対してライブ実行ステップを**実行しないでください**。ライブ実行は、この Runbook と読み取り専用 preflight のエビデンスをレビューした後、**オーナーの明示的な承認を得てからのみ**行います。
:::

SAM データプレーンは AWS×PVE（ENI セカンダリプライベート IP + source/dest check 無効化）ですでに実クラウド検証済みです。これまでその attach/detach は**運用者の手動操作**でした。このガイドでは、同じ変更を手動の代わりに**ゲート付きジャーナル化**実行パス（ADR 0007）で行う `aws-provider-executor` プラグインについて説明します。

## 1. スコープと境界

- **AWS のみ。プロバイダーは 1 つだけ。** この Runbook には Azure も OCI も含みません。
- **トポロジ:** `routerd-cloud` ノード 1 台 + cloud-client 1 台 + on-prem-client 1 台で、on-prem から cloud ENI へ移動する捕捉済み **`/32` は 1 つだけ**です。ラボアドレスとしては（SAM リファレンスに従い）cloud-client が `.7`、on-prem-client が `.9` です。
- **専用ラボ限定。** このテスト用に作成した使い捨ての VPC / サブネット / インスタンスです。**本番リソースや共有リソースは使いません。** 他が依存する EIP、セキュリティグループ、ルートテーブル、インスタンスもありません。
- **ライブ実行はオーナーの明示的な承認後のみ。** 読み取り専用 preflight（Section 4）まではいつでも実行できます。Section 7 の変更はゲートされています。

## 2. Executor の設計

`aws-provider-executor` は `execute.providerAction` ケーパビリティ（`PluginSpec.Capabilities` の Phase 5 列挙値）を通知するプラグインです。**独立したプロセス**で動作し、AWS CLI 経由で **EC2 インスタンス IAM ロール（インスタンスプロファイル）** を使って認証します。**routerd コアは認証情報を一切渡しません** — executor は ADR 0007 のハード不変条件に従い、クラウドネイティブの識別情報のみを使います。

executor は **stdin** から `ExecuteActionRequest` を 1 つ読み取り、stdout に `ExecuteActionResult` を 1 つ出力します。リクエスト仕様には `Action`、`Provider`、`ProviderRef`、`Target`（プロバイダーキー: AWS の場合は `nicRef` = ENI id、`address`、`region`）、`Parameters`、`Mode`（`dry-run` | `execute`）、`IdempotencyKey`、許可リスト済みの `Context` が含まれます。結果には `Status`（`succeeded` | `failed` | `skipped`）、`Message`、`Observed`（ジャーナルが記録する非秘密の事実）、`UndoAvailable`、`Error` が含まれます。

**`dry-run` モードは変更を一切行いません** — describe / 読み取り専用の呼び出しのみです。`execute` モードが変更を行います。

### `assign-secondary-ip`

捕捉した `/32` を cloud ENI にアタッチします。

- **dry-run**（読み取り専用）: ENI を describe して現在のセカンダリ IP を報告し、`would assign <address> to <eni>` と出力します。

  ```sh
  aws ec2 describe-network-interfaces \
    --network-interface-ids "<eni-id>" --region "<region>"
  ```

- **execute**（変更）:

  ```sh
  aws ec2 assign-private-ip-addresses \
    --network-interface-id "<eni-id>" \
    --private-ip-addresses "<address>" --region "<region>"
  ```

### `ensure-forwarding-enabled`

捕捉したアドレスのために cloud ノードが転送できるよう、ENI の source/dest check を無効化します。

- **dry-run**（読み取り専用）: 現在の `SourceDestCheck` を describe し、`would set SourceDestCheck=false` と出力します。

- **execute**（変更）: **まず現在の `SourceDestCheck` を describe して変更前の値を `Observed` に記録し、その後**無効化します。

  ```sh
  # 1. 変更前に変更前の状態をキャプチャ（読み取り専用）
  aws ec2 describe-network-interfaces \
    --network-interface-ids "<eni-id>" --region "<region>" \
    --query 'NetworkInterfaces[0].SourceDestCheck'

  # 2. 変更
  aws ec2 modify-network-interface-attribute \
    --network-interface-id "<eni-id>" \
    --no-source-dest-check --region "<region>"
  ```

  結果の `Observed` には `priorSourceDestCheck=<true|false>` を**必ず**含めてください。これにより、このアクション実行前に存在していた状態をジャーナルが記録します。undo ステップはこの値に依存します。

### `unassign-secondary-ip`（`assign-secondary-ip` の undo）

```sh
aws ec2 unassign-private-ip-addresses \
  --network-interface-id "<eni-id>" \
  --private-ip-addresses "<address>" --region "<region>"
```

### `ensure-forwarding-disabled`（`ensure-forwarding-enabled` の undo）

**ジャーナルの `Observed.priorSourceDestCheck` に記録された変更前の状態を復元します。**
これが安全性を支える重要なルールです:

- `priorSourceDestCheck == true` の場合 → 操作前にチェックが有効だった → 復元します:

  ```sh
  aws ec2 modify-network-interface-attribute \
    --network-interface-id "<eni-id>" \
    --source-dest-check --region "<region>"
  ```

- `priorSourceDestCheck == false` の場合 → 操作前に**すでに無効だった**（ENI はすでにフォワーダーだった） → **何もしません**。`Status=skipped` を返します。チェックを強制的に再有効化**しないでください**。

**undo = チェックを有効化、とハードコードしてはいけません。** 盲目的に「undo で source/dest-check を再有効化する」と、独自の理由ですでにフォワーダーとして動作していたアプライアンス/ENI を壊します。undo は観測した値を読み戻し、実際に変更した部分だけを元に戻す必要があります。

## 3. IAM 最小権限

executor の EC2 インスタンスにアタッチされたインスタンスプロファイルには、**以下の 4 つの EC2 アクションのみ**を付与します:

| アクション | 使用箇所 |
|--------|---------|
| `ec2:DescribeNetworkInterfaces` | dry-run + preflight + 変更前状態キャプチャ |
| `ec2:AssignPrivateIpAddresses` | `assign-secondary-ip` の execute |
| `ec2:UnassignPrivateIpAddresses` | `unassign-secondary-ip` の undo |
| `ec2:ModifyNetworkInterfaceAttribute` | forwarding の有効化/無効化 execute |

ラボの ENI / VPC にスコープを限定するため、API がサポートする範囲でリソース ARN と条件を設定します（変更系の ENI アクションはラボ ENI ARN にリソーススコープ可能、`Describe*` はリソーススコープ不可のため `ec2:Region` / `ec2:Vpc` などの条件キーで制限）:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "DescribeEnis",
      "Effect": "Allow",
      "Action": "ec2:DescribeNetworkInterfaces",
      "Resource": "*",
      "Condition": { "StringEquals": { "ec2:Region": "<region>" } }
    },
    {
      "Sid": "MutateLabEni",
      "Effect": "Allow",
      "Action": [
        "ec2:AssignPrivateIpAddresses",
        "ec2:UnassignPrivateIpAddresses",
        "ec2:ModifyNetworkInterfaceAttribute"
      ],
      "Resource": "arn:aws:ec2:<region>:<account-id>:network-interface/<eni-id>"
    }
  ]
}
```

**これ以上の EC2 権限は不要です。IAM/STS の書き込み権限も不要です。他の AWS サービスも不要です。** 必要な呼び出しがこのリストにない場合、ロールを拡大するのではなく Runbook を停止します。

## 4. 読み取り専用 preflight

**変更前に**、専用ラボに対して実行し、ターゲットを確認します。**これらはいずれも変更しません。** lab-codex がこれらを実行して出力をキャプチャし、オーナーが go を出す前にレビューするエビデンスとします。

```sh
# ターゲット ENI + 現在のセカンダリプライベート IP + 現在の SourceDestCheck
aws ec2 describe-network-interfaces \
  --network-interface-ids "<eni-id>" --region "<region>" \
  --query 'NetworkInterfaces[0].{Eni:NetworkInterfaceId,SrcDstCheck:SourceDestCheck,PrivateIps:PrivateIpAddresses[*].PrivateIpAddress}'

# ENI がアタッチされているインスタンス
aws ec2 describe-instances \
  --filters "Name=network-interface.network-interface-id,Values=<eni-id>" \
  --region "<region>"

# ENI のサブネット
aws ec2 describe-subnets \
  --subnet-ids "<subnet-id>" --region "<region>"

# そのサブネットのルートテーブル（デフォルトゲートウェイに予期しない変更がないか確認）
aws ec2 describe-route-tables \
  --filters "Name=association.subnet-id,Values=<subnet-id>" \
  --region "<region>"
```

確認事項:

1. **IAM ロールが Section 3 の 4 権限のみであること** — インスタンスプロファイルのアタッチ済みポリシーを確認し、広い EC2 権限、IAM/STS 書き込み権限、他のサービスがないことを検証します。（ポリシードキュメントの読み取り専用検査です。ここでは変更しません。）
2. **アドレスがまだ割り当てられていないこと** — `<address>` が上記の最初の describe で取得した ENI の `PrivateIpAddresses` に**まだ含まれていない**ことを確認します。すでに含まれている場合、assign は no-op であり、ラボが汚れています — 停止して調査してください。
3. **`SourceDestCheck` の現在値が記録されていること** — この値は executor が execute 時に `priorSourceDestCheck` としてキャプチャする値です。

## 5. スモークが依存するアクションジャーナルのフィールド

`action_executions` ジャーナルは、アクションごとに以下を記録します:

- `idempotencyKey` — 重複排除キー。すでに succeeded のキーは再実行されません。
- `provider` — `aws`。
- `action` — 例: `assign-secondary-ip`、`ensure-forwarding-enabled`。
- `target` — `eni`、`address`、`region`。
- `status` — `pending` / `approved` / `succeeded` / `failed` / `skipped` / `rolledBack`。
- `Observed.priorSourceDestCheck` — `true` | `false`。変更前にキャプチャされた値で、`ensure-forwarding-enabled` の undo がこの値を読みます。
- `executedAt` — タイムスタンプ。
- `result` / `error` — `ExecuteActionResult` のメッセージ / `Error`。

ジャーナルは、何が実行されたかと冪等性ガードの単一の信頼できるソースです。認証情報は**一切**ジャーナルに記録されません。

## 6. Undo / teardown 計画

適用されたものを逆順で元に戻します。すべてのステップはライブ実行の**前に**記述可能でなければなりません。

1. **Forwarding の undo** — `ensure-forwarding-disabled`。Section 2 の**変更前状態復元ルール**を適用します: `Observed.priorSourceDestCheck` が `true` だった場合は `--source-dest-check` を実行して再有効化します。`false` だった場合は**何もしません**（skipped）。盲目的にチェックを有効化しないでください。
2. **セカンダリ IP の割り当て解除** — `unassign-secondary-ip`:

   ```sh
   aws ec2 unassign-private-ip-addresses \
     --network-interface-id "<eni-id>" \
     --private-ip-addresses "<address>" --region "<region>"
   ```
3. **ラボインスタンスの停止/終了とコスト発生リソースの解放** — `routerd-cloud`、cloud-client、on-prem-client のラボインスタンスを停止または終了します。割り当て済みの **EIP** を解放し、孤立した **EBS** ボリュームを削除し、このテスト専用に作成した VPC/サブネット/SG を削除します。

**エビデンスをキャプチャした後、すべてのコスト発生リソースを停止または削除してください。** ラボインスタンスをアイドル状態で放置しないでください。

## 7. ライブ変更スモーク計画 + 受け入れ

このスモークはゲート付きパス全体を検証します。Section 9 のゲートが承認された後にのみ実行してください。

シーケンス:

1. `actionPlan` 生成（planner、dry-run、Phase 4.1 と同様）。
2. アクションがジャーナルに `pending` として**インポート**される（`idempotencyKey` でキー付け）。
3. アクションが**承認**される（`routerctl action approve`）。
4. アクションが **`aws-provider-executor` によって実行**される（`routerctl action execute --approved`）。
5. ジャーナルに `succeeded` が表示される。

受け入れ条件（すべて満たす必要があります）:

- [ ] actionPlan 生成 → インポート → 承認 → 実行 → ジャーナル `succeeded`。
- [ ] **セカンダリ IP が ENI 上に存在する**（`describe-network-interfaces` で `<address>` が `PrivateIpAddresses` に表示される）。
- [ ] ENI で **Source/dest check が無効化**されている（`SourceDestCheck=false`）。ジャーナルに `Observed.priorSourceDestCheck` が記録されている。
- [ ] no-local 捕捉では、`routerd-cloud` はアドレスを OS のローカルアドレスとして**保持しない**。これは `configureOSAddress=false` の場合と、プロバイダーのセカンダリ IP を ENI に残したまま BGP delivery でリモートオーナーへ配送する場合の両方を含む。捕捉はプロバイダー ingress とルート/転送状態であり、Linux local `/32` ではない。
- [ ] `RemoteAddressClaim` が **Ready** に到達する。
- [ ] `routerctl doctor` の hybrid チェックが**パス**する。
- [ ] cloud-client **`.7`** と on-prem-client **`.9`** — **ping と ssh が双方向で**成功する。
- [ ] 捕捉パスに **NAT が存在しない**（ルーティング/転送され、変換されない）。
- [ ] すべてのノードで**デフォルトゲートウェイが変更されていない**。
- [ ] Section 6 の **Teardown / undo が成功する**（source/dest-check の変更前状態復元ルールを含む）。
- [ ] エビデンスキャプチャ後に**コスト発生リソースが停止/削除されている**。

## 8. ハードストップ

以下のいずれかに該当する場合は直ちに中止してください（「回避策」は取らない）:

1. 認証情報が **routerd コアを経由する**（してはいけません — executor は自身のインスタンスプロファイルのみを使用）。
2. アクションが**ラボ以外のリソース**に影響を与える。
3. **複数のプロバイダー**が関与している。
4. **ロールバック/クリーンアップが事前に記述できない。**
5. プロバイダー API が**曖昧な/部分的な成功**を返す。
6. **コスト発生リソースがアクティブなテストなしに稼働し続ける。**
7. クラウドリソースが稼働中に人間の判断を **10 分以上待つ** → **停止して deallocate する**（インスタンスを停止してコスト削減）。判断後に再開します。
8. いずれかのコマンドが**本番または共有リソースへの変更を意味する**。

## 9. ライブ実行のゲート

ライブ変更は、オーナーが**この Runbook**と**読み取り専用 preflight のエビデンス**（Section 4）をレビューした後に**明示的に go を出してからのみ**実行されます。go が出るまでは、読み取り専用のステップのみ実行できます。
