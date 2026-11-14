# DHCPv4Relay

[リソース別の設定](../resources.md#dhcpv4relay) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

dnsmasqを使い、DHCPv4要求を明示した1つのIPv4サーバーへ中継します。

## 指定条件と参照関係

- 1件以上の宣言済みインターフェースと、裸のIPv4上流アドレスが必要です。自動検出・上流一覧・インターフェース別の中継元アドレス・条件フィールドはありません。
- Linux・FreeBSDとも共有dnsmasqプロセスに、各インターフェースのdhcp-relay=0.0.0.0,<upstream>,<OS名>を生成します。上流へ到達できるホスト経路が必要です。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Relay
  metadata:
    name: "<name>"
  spec:
    interfaces:
      - <string>
    upstream: <string>
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.interfaces` | **必須** | array | 空でない論理インターフェース一覧が必須です。同じRouterのインターフェース索引に存在する必要があり、実行時はInterface.ifnameなどの対応する別名を解決します。既定値はありません。 |
| `spec.upstream` | **必須** | string | ポート・CIDR・リソース参照ではない、DHCPサーバーのIPv4アドレスが必須です。既定値はありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`
