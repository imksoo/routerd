---
title: routerctl doctor
sidebar_label: routerctl doctor
---

# routerctl doctor — ランタイム健全性診断

![Diagram showing routerctl doctor read-only diagnostics combining state, status socket, and optional host probes into area checks, stable JSON or YAML output, and fail-only nonzero exit behavior](/img/diagrams/operations-routerctl-doctor.png)

`routerctl doctor` は読み取り専用のチェックを一通り実行し、いまこの routerd が
家庭ゲートウェイとして機能しているかを報告します。ホスト状態は変更しません。
運用者・CI・監視エージェント・下流ツール（Prometheus exporter、Web Console、
LLM 補助診断など）から使われることを想定しています。

## 使い方

```sh
# 全エリア実行（既定）
routerctl doctor

# 単一エリア
routerctl doctor dns

# ホストコマンドを使わない（リソース status のみ）
routerctl doctor --no-host

# 機械可読出力
routerctl doctor -o json
routerctl doctor -o yaml
```

オプションは `diagnose` と共通: `--config`, `--state-file`,
`--no-host` / `--host`, `-o` / `--output`, `--timeout`。

## エリア

| エリア | チェック内容 |
| --- | --- |
| `wan` | `EgressRoutePolicy` と `HealthCheck` のリソース status、IPv4 / IPv6 のデフォルト経路（`ip -4/-6 route show default`）。 |
| `dns` | `DNSResolver` のリソース status、`dig @127.0.0.1` での A レコード応答プローブ。 |
| `dslite` | `DSLiteTunnel` のリソース status、AFTR FQDN の AAAA プローブ、tunnel device の存在（`ip link show`）。 |
| `dhcpv6-pd` | `DHCPv6PrefixDelegation` の status（Bound、委任 prefix）。PD 未取得時は設計上 **WARN**（壊れている IPv6 を LAN に出さない）。 |
| `nat` | `NAT44Rule` のリソース status、`nft list table ip routerd_nat` の存在。 |
| `firewall` | `FirewallZone` / `FirewallPolicy` の status、`nft list table inet routerd_filter` の存在と input チェインの `policy drop`（無いと permissive）、current config から render される ruleset に含まれない routerd-prefixed nft table が Linux host 上に残っていないか。 |
| `rollback` | 1 つ以上の世代が保存されていて `routerctl rollback --to` が使えること。 |
| `disk` | `/var/lib/routerd` と `/run/routerd` の容量。90% 以上 or 256 MiB 未満で WARN、98% 以上 or 64 MiB 未満で FAIL。Linux では一時ディレクトリの不変条件も確認します。`/tmp` と `/var/tmp` は `root:root` の sticky `1777` directory である必要があります。 |
| `mgmt` | 管理用 interface の存在（`ManagementAccess` または `FirewallZone role=mgmt` から推定）。WebConsole の bind 先（`0.0.0.0` / `::` は WARN/FAIL）。 |
| `reconcile` | 読み取り専用ステータスソケットから、コントローラーごとの reconcile 失敗履歴を確認します。`--since <duration>` で対象期間を区切ります。期間内に 1 件以上で WARN、10 件以上で FAIL。detail に最大 5 件のサンプルを表示します。 |
| `runtime` | 読み取り専用ステータスソケットから、routerd 自身の heap / goroutine / fd を確認します: `heapAlloc`、`heapObjects`、`numGoroutine`、`numGC`、`openFds`/`maxFds`。`numGoroutine` が 10000 超、または open fd が `RLIMIT_NOFILE` の 80% 以上で WARN。観測用で FAIL にはなりません。 |
| `hybrid` | `HybridRoute` / `OverlayPeer` の参照、Selective Address Mobility の設定参照、デフォルト経路を触らない安全性、MTU 推定、任意の `HealthCheck` status、読み取り専用の経路表確認（`ip -4 route show <prefix>`）。Linux SAM では `/32` delivery route、provider local-address absence、proxy neighbor capture、`proxy_arp`、`ip_forward`、route lookup、warning-only の `rp_filter`、default-drop `FORWARD` policy heuristic も確認します。 |

各チェックは `pass` / `warn` / `fail` / `skip`（該当リソース/シグナルが無い）のいずれかを返します。

## JSON 出力契約

`routerctl doctor -o json` は**安定した**機械可読インターフェースです。形:

```jsonc
{
  "summary": {
    "overall": "pass",      // "pass" | "warn" | "fail" | "skip"
    "pass": 7,
    "warn": 1,
    "fail": 0,
    "skip": 2
  },
  "checks": [
    {
      "area":   "dns",                          // 上記エリア表のいずれか
      "name":   "DNSResolver/lan-resolver",     // 人間可読の対象名
      "status": "warn",                         // "pass" | "warn" | "fail" | "skip"
      "detail": "phase=Degraded,waiting=...",   // 任意
      "remedy": "wait for or repair dependency wan-pd" // 任意
    }
    // ...
  ]
}
```

保証内容:

- `summary.overall` は `checks[].status` の最悪値（`fail` > `warn` > `unknown`/`skip` > `pass`）。
- `summary.pass/warn/fail/skip` は整数件数で、合計は `len(checks)`。
- `checks[].status` は `pass`, `warn`, `fail`, `skip` のいずれか（他の値は出ない）。
- `checks[].area` はエリア表の識別子のいずれか。集合は安定。
- `checks[].name` は人間可読。厳密な形にパターンマッチしないこと。
- `detail` / `remedy` は任意のフリーフォーム文字列で、運用者向け。

例えば `routerctl doctor runtime -o json` は、読み取り専用ステータス
ソケットから routerd 自身のプロセス footprint を表示します:

```jsonc
{
  "summary": { "overall": "pass", "pass": 1, "warn": 0, "fail": 0, "skip": 0 },
  "checks": [
    {
      "area": "runtime",
      "name": "process",
      "status": "pass",
      "detail": "heapAlloc=11.0MiB heapObjects=84213 numGoroutine=187 numGC=14 openFds=23/1024"
    }
  ]
}
```

## 終了コード

- `0` — `fail` が 1 件も無い（`pass` / `warn` / `skip` は失敗扱いではない）。
- 非0 — `fail` が 1 件以上。`routerctl doctor || alert` のように使えます。

`warn` は exit code を非0 にしません（例: 起動直後で DHCPv6-PD がまだ Bound でない、など情報的なもの）。
厳しめのゲートが欲しい場合はエリアを絞ってください（`routerctl doctor wan` は `wan` の fail でだけ非0）。

## 安定性

JSON の形、エリア識別子、status の enum は v1alpha1 の運用者向け契約です。今後のバージョンで
**エリアやオプショナルフィールドが追加されることはあります**が、既存エリア名や status 値は
v1alpha1 の minor 間で改名・再用途化しません。

## 関連項目

- [調整（reconcile）とロールバック](./reconcile.md)
- [`routerctl ledger` 保守](./reconcile.md#削除)
