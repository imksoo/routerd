# DHCPv4Relay

[リソース別の設定](../resources.md#dhcpv4relay) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

dnsmasqを使い、DHCPv4要求を明示した1つのIPv4サーバーへ中継します。

## 指定条件と参照関係

- 1件以上の宣言済みインターフェースと、裸のIPv4上流アドレスが必要です。自動検出・上流一覧・インターフェース別の中継元アドレス・条件フィールドはありません。
- Linux・FreeBSDとも共有dnsmasqプロセスに、各インターフェースのdhcp-relay=0.0.0.0,<upstream>,<OS名>を生成します。上流へ到達できるホスト経路が必要です。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Relay
  metadata:
    name: "<name>"
  spec:
    interfaces: # 必須; array
      - "<string>" # string
    upstream: "<string>" # 必須; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.interfaces` | 空でない論理インターフェース一覧が必須です。同じRouterのインターフェース索引に存在する必要があり、実行時はInterface.ifnameなどの対応する別名を解決します。既定値はありません。 |
| `spec.upstream` | ポート・CIDR・リソース参照ではない、DHCPサーバーのIPv4アドレスが必須です。既定値はありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`
