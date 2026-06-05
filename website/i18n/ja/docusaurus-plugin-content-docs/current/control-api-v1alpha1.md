---
title: 制御 API v1alpha1
slug: /reference/control-api-v1alpha1
---

# 制御 API v1alpha1

routerd と管理対象デーモンは、ローカルの Unix domain socket 上に HTTP+JSON API を公開します。
この API はリモート管理用ではなく、`routerctl`、routerd 本体、運用スクリプトが同じホスト上で状態を読むためのものです。

## routerd 本体

`routerd serve` は次を待ち受けます。

```text
/run/routerd/routerd.sock
/run/routerd/routerd-status.sock
```

主 control socket は権限を持つローカル client 向けで、apply や delete などの変更系
endpoint も公開します。読み取り専用 status socket は status 系 endpoint だけを
公開し、一般ユーザーが状態確認に使えます。

主 control socket の読み取り endpoint は、状態、event、resource state を返します。
代表例は次の通りです。

| Method + Path | 用途 |
| --- | --- |
| `GET /api/control.routerd.net/v1alpha1/status` | routerd 自身の状態 |
| `GET /api/control.routerd.net/v1alpha1/connections` | conntrack または pf state から得た現在のコネクション |
| `GET /api/control.routerd.net/v1alpha1/dns-queries` | DNS クエリー履歴 |
| `GET /api/control.routerd.net/v1alpha1/traffic-flows` | 通信フロー履歴 |
| `GET /api/control.routerd.net/v1alpha1/firewall-logs` | ファイアウォールログ |

## Controller の状態

`Status.status.controllers` と `Controllers` endpoint は、controller の設定上の
mode と、実行時の reconcile 状態を返します。runtime field には `interval`、
`lastTrigger`、`lastReconcileTime`、`nextReconcileTime`、`reconcileCount`、
`reconcileErrorCount`、`consecutiveErrorCount`、`currentError`、
`lastDuration`、`maxDuration`、`averageDuration`、`lastError`、
`lastErrorTime`、`lastErrorClearedAt` が含まれます。`reconcileErrorCount` は
累積値なので、現在失敗中かどうかは `currentError` と `consecutiveErrorCount` で
判定してください。これらは観測値なので、controller がまだ一度も実行されて
いない場合は、field が無いものとして扱ってください。

## 管理対象 daemon

状態を持つ daemon は、それぞれ独自の socket を持ちます。

```text
/run/routerd/dhcpv6-client/wan-pd.sock
/run/routerd/dhcpv4-client/wan.sock
/run/routerd/pppoe-client/wan-pppoe.sock
/run/routerd/healthcheck/internet.sock
```

FreeBSD では、対応するパスは `/var/run/routerd/...` です。

## daemon 共通 endpoint

| Method + Path | 用途 |
| --- | --- |
| `GET /v1/healthz` | liveness check |
| `GET /v1/status` | daemon の状態と、関連リソースの状態 |
| `GET /v1/events` | event log。`since`、`wait`、`topic` を query で指定します |
| `POST /v1/commands/reload` | 設定の再読み込み |
| `POST /v1/commands/renew` | daemon ごとの能動操作（DHCPv6 Renew、DHCPv4 のリース更新、即時の health probe など） |
| `POST /v1/commands/stop` | 安全に停止 |

`renew` の意味は daemon ごとに異なります。DHCPv6 は Renew の送信、DHCPv4 はリース更新、healthcheck は即時の probe です。

## Phase 語彙

`ResourceStatus.phase` は、リソース横断で共通の語彙を使います。

| Phase | 意味 |
| --- | --- |
| `Pending` | 必要な入力を待機中です |
| `Bound` | DHCP などのリースを保持中です |
| `Applied` | ホスト側へ適用済みです |
| `Up` | tunnel または link が up しています |
| `Installed` | 経路または設定ファイルが入っています |
| `Healthy` | health check が success threshold を満たしています |
| `Unhealthy` | health check が failure threshold を満たしています |
| `Error` | 操作に失敗しました |

各 phase には `conditions` 配列が付きます。client 側のコードでは、log 文字列ではなく `phase` と `conditions` で判定してください。

## イベント

イベントは topic と attributes を持ちます。

```json
{
  "topic": "routerd.dhcpv6.client.prefix.renewed",
  "attributes": {
    "resource.kind": "DHCPv6PrefixDelegation",
    "resource.name": "wan-pd"
  }
}
```

routerd は event を SQLite に永続化します。
管理対象 daemon は、加えて自身の `events.jsonl` にも記録します。
`EventRule` と `DerivedEvent` は、このストリームを入力にして仮想 event を発行します。
