---
title: 制御 API v1alpha1
slug: /reference/control-api-v1alpha1
---

# 制御 API v1alpha1

routerd と専用デーモンは、ローカルの Unix ドメインソケットで HTTP+JSON API を公開します。
この API は遠隔管理用ではなく、同じホスト上の `routerctl`、routerd 本体、運用スクリプトが状態を読むためのものです。

## routerd 本体

`routerd serve` は既定で次のソケットを使います。

```text
/run/routerd/routerd.sock
```

主な用途は、現在状態の確認、イベント確認、リソース状態の確認です。

## 専用デーモン

状態を持つ処理は、次のようなソケットを持ちます。

```text
/run/routerd/dhcpv6-client/wan-pd.sock
/run/routerd/dhcpv4-client/wan.sock
/run/routerd/pppoe-client/wan-pppoe.sock
/run/routerd/healthcheck/internet.sock
```

FreeBSD では `/var/run/routerd/...` を使う構成があります。

## 共通エンドポイント

| メソッドとパス | 意味 |
| --- | --- |
| `GET /v1/healthz` | プロセスが応答できるかを返します。 |
| `GET /v1/status` | デーモン状態と関連リソース状態を返します。 |
| `GET /v1/events` | イベントログを返します。`since`、`wait`、`topic` を指定できます。 |
| `POST /v1/commands/reload` | 設定の再読み込みを依頼します。 |
| `POST /v1/commands/renew` | リース更新や即時測定など、デーモンごとの能動処理を依頼します。 |
| `POST /v1/commands/stop` | 安全な停止を依頼します。 |

`renew` の意味はデーモンごとに異なります。
DHCPv6 では Renew、DHCPv4 ではリース更新、ヘルスチェックでは即時測定です。

## 状態の段階

`ResourceStatus.phase` は共通の語彙を使います。
代表例は次の通りです。

| Phase | 意味 |
| --- | --- |
| `Pending` | 必要な入力を待っています。 |
| `Bound` | DHCP などのリースを保持しています。 |
| `Applied` | ホスト側への適用が終わっています。 |
| `Up` | トンネルやリンクが上がっています。 |
| `Installed` | 経路や設定が入っています。 |
| `Healthy` | ヘルスチェックが成功条件を満たしています。 |
| `Unhealthy` | ヘルスチェックが失敗条件を満たしています。 |
| `Error` | 処理に失敗しています。 |

各状態には `conditions` が付きます。
利用者向けの判定は、文字列ログではなく `phase` と `conditions` を見ます。

## イベント

イベントは topic と attributes を持ちます。
例:

```json
{
  "topic": "routerd.dhcpv6.client.prefix.renewed",
  "attributes": {
    "resource.kind": "DHCPv6PrefixDelegation",
    "resource.name": "wan-pd"
  }
}
```

routerd はイベントを SQLite に永続化します。
専用デーモンは `events.jsonl` にも記録します。
EventRule と DerivedEvent は、このイベントを入力にして仮想イベントを発行します。
