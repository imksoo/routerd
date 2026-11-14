# DHCPv6Information

[リソース別の設定](../resources.md#dhcpv6information) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

同じインターフェースを使うDHCPv6-PD daemonから、AFTR・DNS・SNTP・検索ドメインの情報を取り込みます。独立したInformation-request clientを起動するリソースではありません。

## 指定条件と参照関係

- 同じ論理interfaceのDHCPv6PrefixDelegationがBoundになってから、そのdaemon socketを読みます。対応するPDリソースがなければ取得元はありません。Info-requestはbootstrap/Bound遷移時に送り、独立したリース管理にはしません。
- 情報controllerはwhenで絞った実効構成ではなく宣言済みRouterを使い、dependsOnを評価しません。Readyはdaemon状態を読めたことを示し、新しい情報応答を受信した証明ではありません。ready_whenは拒否し、下記の実装上の制約も確認してください。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 任意; array
      - # object
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        notEmpty: true # 任意; boolean
        optional: true # 任意; boolean
        phase: "<string>" # 任意; string
        resource: "<string>" # 必須; string
    interface: "<string>" # 必須; string
    request: # 任意; array
      - "<string>" # string
    when: # 任意; object
      state: # 任意; object
        "<key>": # 任意; object
          contains: "<string>" # 任意; string
          equals: "<string>" # 任意; string
          exists: true # 任意; boolean
          for: "<string>" # 任意; string
          in: # 任意; array
            - "<string>" # string
          maxAge: "<string>" # 任意; string
          status: "set" # 任意; string; enum=["set", "unset", "unknown"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.interface` | DHCPv6PrefixDelegation.spec.interfaceと完全一致で照合する必須の識別子。既定値はありません。両方の宣言で同じ論理インターフェース名を使います。 |
| `spec.request` | 要求オプションの宣言。省略時は空ですが、現行controllerは使いません。daemonのInfoRequestはAFTR名・DNS・SNTP・検索ドメインを固定のOROで要求します。 |
| `spec.when` | 条件の宣言。省略時は未指定で、共通whenのルールで検証します。ただし現行の情報controllerはこの値で自身の処理を絞らず、情報要求の停止スイッチには使えません。 |
| `spec.dependsOn` | 依存条件の宣言。省略時は空です。参照先・fieldは検証しますが、このcontrollerは情報の読取り・要求の前提としてこれらの条件を評価しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/pdclient/client.go](https://github.com/imksoo/routerd/blob/main/pkg/pdclient/client.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `source`, `aftrName`, `dnsServers`, `sntpServers`, `domainSearch`
