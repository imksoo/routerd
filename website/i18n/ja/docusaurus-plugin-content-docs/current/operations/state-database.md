---
title: 状態データベース
slug: /operations/state-database
---

# 状態データベース

![Diagram showing routerd state database paths, daemon lease and event files, routerctl event access, and backup philosophy where YAML remains authoritative and event databases provide forensic history](/img/diagrams/operations-state-database.png)

routerd は状態とイベントを SQLite に永続化します。各デーモンはこれに加えて、自身のリースまたは状態ファイルとイベントログを持ちます。

## 主なパス

| 種類 | パス |
| --- | --- |
| routerd 状態 DB | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD リース | `/var/lib/routerd/dhcpv6-client/<name>/lease.json` |
| DHCPv4 リース | `/var/lib/routerd/dhcpv4-client/<name>/lease.json` |
| PPPoE 状態 | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| HealthCheck 状態 | `/var/lib/routerd/healthcheck/<name>/state.json` |
| デーモン別イベント | `/var/lib/routerd/<daemon>/<name>/events.jsonl` |

## events テーブル

イベントバスはイベントを SQLite に永続化します。`EventRule` や `DerivedEvent` は、このストリームを入力にします。
日常の運用では `sqlite3` を直接叩かず、`routerctl events` を使ってください。

```sh
routerctl events --limit 20
routerctl events --topic routerd.resource.status.changed
routerctl events --resource DNSResolver/lan-resolver -o json
```

### Mobility holder transitions

CloudEdge SAM の failover は、`transitionKind`、`address`、`timestamp`、
`issuedAt`、`fromNode`、`toNode`、`mobilityPathSig`、
`assignmentGeneration` などの機械可読属性を持つ
`routerd.mobility.holder.transition` イベントを出します。

provider-secondary-IP capture では、`seize-complete` は Active な `/32`
`bgpCaptureAssignment` に対する provider capture assign action が action
journal で succeeded になった事実を意味します。`issuedAt` は journal の
`ExecutedAt` なので、`timestamp - issuedAt` は provider が受理してから
イベントを記録するまでの遅延です。`T_seize` は provider の受理時刻です。

`capture-confirmed` は従来どおり discovery による観測に基づきます。
`T_confirm` は provider capture がローカルで効いたと観測した時刻です。
この 2 つで、受理から実効までの区間を測れます。

ノードの再起動や rejoin 後の再確認イベントでは、元の journal 受理時刻が
`issuedAt` として再利用されることがあります。その場合、
`timestamp - issuedAt` にはノードが停止または不在だった時間も含まれます。
この差分は再確認までの経過時間として扱い、収束レイテンシとして解釈しないでください。

static-owned、static-handover、local-home など capture 以外のフローでは、
`seize-complete` は引き続き active-holder と self-identity の BGP 観測から
導出します。lab 実証済みなのは capture フローで、static/handover の
completion event は実環境ではまだ未実証です。

## バックアップの考え方

状態 DB は**観測した**状態を持つもので、設定の代わりにはなりません。
意図の正本は YAML 設定ファイルなので、git で管理してください。
ホストを再構築するときは、SQLite を復元するよりも、設定ファイルを当てて routerd に調整（リコンサイル）させる方が確実です。

事後調査の用途で操作イベントの履歴を残したい場合は、`events.db`、`dns-queries.db`、`traffic-flows.db`、`firewall-logs.db` のスナップショットを定期的に取ってください。これらは追記専用なので、`routerd.db` のような特定時点のバックアップは不要です。

## 関連項目

- [ログストレージ](../concepts/log-storage.md)
- [Reconcile と削除](./reconcile)
