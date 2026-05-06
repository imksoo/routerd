---
title: 制御 API v1alpha1
slug: /reference/control-api-v1alpha1
---

# 制御 API v1alpha1

routerd と管理対象 daemon は、ローカル Unix domain socket 上に HTTP+JSON API を公開します。
この API は遠隔管理用ではなく、`routerctl`、routerd 本体、運用スクリプトが同じホスト上で状態を読むためのものです。

## routerd 本体

`routerd serve` は次に listen します：

```text
/run/routerd/routerd.sock
```

読み取り endpoint で状態、event、resource state を返します。代表例：

| Method + Path | 用途 |
| --- | --- |
| `GET /api/control.routerd.net/v1alpha1/status` | routerd 自身の状態 |
| `GET /api/control.routerd.net/v1alpha1/connections` | conntrack または pf state からの実時間コネクション |
| `GET /api/control.routerd.net/v1alpha1/dns-queries` | DNS クエリ履歴 |
| `GET /api/control.routerd.net/v1alpha1/traffic-flows` | トラフィックフロー履歴 |
| `GET /api/control.routerd.net/v1alpha1/firewall-logs` | firewall ログ |

## 管理対象 daemon

状態を持つ daemon は各々独自の socket を持ちます：

```text
/run/routerd/dhcpv6-client/wan-pd.sock
/run/routerd/dhcpv4-client/wan.sock
/run/routerd/pppoe-client/wan-pppoe.sock
/run/routerd/healthcheck/internet.sock
```

FreeBSD では同等のパスは `/var/run/routerd/...` です。

## daemon 共通 endpoint

| Method + Path | 用途 |
| --- | --- |
| `GET /v1/healthz` | liveness check |
| `GET /v1/status` | daemon 状態と関連リソース状態 |
| `GET /v1/events` | event log。`since`、`wait`、`topic` を query で指定 |
| `POST /v1/commands/reload` | 設定再読込 |
| `POST /v1/commands/renew` | daemon 固有の能動操作 (DHCPv6 Renew、DHCPv4 lease refresh、即時 health probe など) |
| `POST /v1/commands/stop` | gracefully 停止 |

`renew` の意味は daemon ごとに異なります：DHCPv6 は Renew 送信、DHCPv4 はリース更新、healthcheck は即時 probe。

## Phase 語彙

`ResourceStatus.phase` はリソース横断で共通の語彙を使います：

| Phase | 意味 |
| --- | --- |
| `Pending` | 必要な入力を待機中 |
| `Bound` | DHCP 等のリースを保持中 |
| `Applied` | ホスト側適用済 |
| `Up` | tunnel または link が up |
| `Installed` | 経路または設定ファイルが入っている |
| `Healthy` | health check が success threshold を満たしている |
| `Unhealthy` | health check が failure threshold を満たしている |
| `Error` | 操作失敗 |

各 phase には `conditions` 配列が付きます。client 側コードでは log 文字列ではなく `phase` と `conditions` で判定してください。

## イベント

イベントは topic と attributes を持ちます：

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
管理対象 daemon は加えて自身の `events.jsonl` にも記録します。
`EventRule` と `DerivedEvent` はこのストリームを入力にして仮想 event を発行します。
