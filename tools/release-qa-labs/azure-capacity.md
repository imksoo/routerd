# Azure capacity admission / Azure 容量の事前検査

## English

The closed `full-representative` profile requires three `Standard_B1s` VMs
(three additional vCPUs) in `japaneast`. `precheck-driver.sh` runs
`azure_capacity.py` immediately after contract/source-pin validation and before
the PVE substrate checks, remote-egress checks, baseline inventory, and the
supervisor's `MUTATING` transition. Thus a known Azure quota shortage rejects
the run before creating even the disposable PVE topology. The ordinary
supervised cleanup, complete zero inventory, and token-revocation obligations
still apply to a failed precheck.

The helper uses the existing run-confined `AZURE_CONFIG_DIR`. It verifies the
contract's Azure provider/region and fixed count/flavor limits against the
explicit subscription and location in the pinned tfvars. The subscription is
not a new contract field: it is bound through the existing pinned tfvars input.
`az account show` must identify that same subscription because existing Azure
inventory/cleanup commands use the active account. The helper never runs
`az account set` or changes credentials. It then runs only:

```text
az vm list-usage --subscription <pinned subscription> --location japaneast --output json --only-show-errors
```

Both `cores` (total regional vCPUs) and `standardBSFamily` must be present once,
have unit `Count`, and satisfy `currentValue + 3 <= limit`. Each CLI command has
a 20-second timeout, with no retry or new process session. The CLI's digit-string
values and integer values are accepted; booleans, floats, negative or malformed
values, missing/duplicate quota records, duplicate JSON keys, malformed JSON,
authentication/command failures, or context mismatches fail closed. The usage
response, not a count of running VMs, is authoritative for this admission check:
deallocated allocations can still consume quota. For example, `8 + 3 > 10`
fails even though all eight existing VMs are deallocated.

`pve-certification-only` records `not-applicable` and makes no Azure call in this
helper. This does not remove existing authentication, inventory, or other
prechecks elsewhere in the lifecycle. Unknown scopes fail closed.

Private evidence is written to
`runtime/evidence/preflight/azure-capacity/result.json` (mode `0600`). It binds
the contract/tfvars digests and records the region, checked time, hashed
subscription identity, status and sanitized quota counts. Capacity rejection
retains both valid quota observations. Credentials, account IDs, and CLI stderr
are not echoed. The helper and its precheck caller require reviewed source pins
in `qaImplementation.scriptBlobs`.

This is an observation, **not a reservation or a guarantee of allocation**.
Capacity can change after PRECHECK, and SKU availability/other quotas/provider
errors can still reject an apply. The gate neither raises quotas, changes region
or VM size, modifies unrelated allocations, nor increases lifecycle/cost limits.
A rejection is not permission to destroy old VMs or retry a paid campaign.
Reproduce failures locally, retain evidence, and resolve the exact authorized
capacity prerequisite before another supervised run.

Offline regressions use the real precheck/helper with fake provider I/O, plus
pure malformed-input and source-pin tests. They prove admission ordering and
rejection, not a live Azure allocation or full Cloud SAM PASS:

```sh
cd tools/release-qa-labs/tests
python3 -m unittest -v test_azure_capacity test_qa_guard
```

## 日本語

正規 `full-representative` 入口は、固定構成の Azure `Standard_B1s` 3 台
（追加 3 vCPU）、地域 `japaneast` の容量を PRECHECK で確認する。
contract と source pin の検証直後、PVE substrate・remote egress・baseline
inventory の検査前に実行し、PVE を含む資源作成開始前に既知の不足を拒否する。
拒否後も supervisor の回収・全 scope の zero 確認・token 失効は省略しない。

既存の run 専用 Azure 認証を使用し、pinned tfvars の subscription/location と
contract の地域・3 台/B1s 固定制限を照合する。現在の `az account show` の
subscription も同一でなければ拒否するため、quota と既存 inventory/cleanup
で異なる subscription を使わない。account 切替・quota 変更はしない。
subscription を明示した `az vm list-usage` の地域全体 `cores` と
`standardBSFamily` の両方に `使用中 + 3 <= 上限` を要求する。
各 CLI 呼出しは既存 supervisor 内で最大 20 秒、retry・新 session は設けない。
数字文字列/整数だけを受理し、不足・欠損・重複・不正型・重複 JSON key・
CLI 失敗・認証 context 不一致は拒否する。deallocated VM が消費している quota
も使用量に含め、単なる running 台数で空き枠を判断しない。

`pve-certification-only` はこの helper の Azure 呼出しだけを省略して
`not-applicable` を記録し、既存の他検査は維持する。
数値・照合 digest・判定は run 内の上記 private evidence へ保存する。
不足時も両 quota の観測値を残し、認証値・account ID・CLI stderr は表示しない。
helper と呼出し元の source pin も必須とする。

これは空き枠の予約ではない。PRECHECK 後の容量変化、SKU の在庫、他 quota 等は
引き続き apply 失敗になり得る。地域・VM 型・予算・期限の緩和や、他用途 VM の
削除を自動で行わず、追加の有料 run を黙って開始しない。
上記回帰は fake/pure のローカル検査であり、Azure 実機作成や Cloud SAM 全体の
合格証明とは区別する。
